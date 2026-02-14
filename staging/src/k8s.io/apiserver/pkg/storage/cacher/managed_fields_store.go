/*
Copyright 2025 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cacher

import (
	"hash/fnv"
	"io"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// managedFieldsRecord holds a deduplicated copy of managedFields along with
// its content hash and reference count.
type managedFieldsRecord struct {
	fields []metav1.ManagedFieldsEntry
	hash   uint64
	refs   int32
}

// managedFieldsKey identifies a specific object version in the side store.
type managedFieldsKey struct {
	objectKey       string
	resourceVersion uint64
}

// managedFieldsStore is a content-addressed side store for managedFields.
// It deduplicates managedFields across both object versions (temporal) and
// different objects (cross-object) using FNV-1a hashing with equality verification.
type managedFieldsStore struct {
	mu     sync.RWMutex
	index  map[managedFieldsKey]*managedFieldsRecord // (key, rv) -> record
	byHash map[uint64]*managedFieldsRecord           // hash -> record (dedup)
}

func newManagedFieldsStore() *managedFieldsStore {
	return &managedFieldsStore{
		index:  make(map[managedFieldsKey]*managedFieldsRecord),
		byHash: make(map[uint64]*managedFieldsRecord),
	}
}

// Add adds managedFields for the given object key and resource version.
// refCount specifies the initial reference count (e.g. 2 for ring buffer + store).
// If identical managedFields already exist (by content hash), the existing
// record is shared.
func (s *managedFieldsStore) Add(objectKey string, rv uint64, mf []metav1.ManagedFieldsEntry, refCount int32) {
	if len(mf) == 0 {
		return
	}
	h := hashManagedFields(mf)

	s.mu.Lock()
	defer s.mu.Unlock()

	key := managedFieldsKey{objectKey: objectKey, resourceVersion: rv}

	// Check if this exact (objectKey, rv) is already stored.
	if existing, ok := s.index[key]; ok {
		existing.refs += refCount
		return
	}

	// Check for content-addressed dedup.
	if existing, ok := s.byHash[h]; ok && managedFieldsEqual(existing.fields, mf) {
		existing.refs += refCount
		s.index[key] = existing
		return
	}

	// New unique managedFields content.
	record := &managedFieldsRecord{
		fields: mf,
		hash:   h,
		refs:   refCount,
	}
	s.index[key] = record
	s.byHash[h] = record
}

// Get returns the managedFields for the given object key and resource version.
// Returns nil if not found.
func (s *managedFieldsStore) Get(objectKey string, rv uint64) []metav1.ManagedFieldsEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := managedFieldsKey{objectKey: objectKey, resourceVersion: rv}
	if record, ok := s.index[key]; ok {
		return record.fields
	}
	return nil
}

// Release decrements the reference count for the given object key and resource version.
// When the reference count reaches zero, the record is removed.
func (s *managedFieldsStore) Release(objectKey string, rv uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := managedFieldsKey{objectKey: objectKey, resourceVersion: rv}
	record, ok := s.index[key]
	if !ok {
		return
	}

	record.refs--
	if record.refs <= 0 {
		delete(s.index, key)
		// Only delete from byHash if the record in byHash is the same pointer.
		// Another record with the same hash might have replaced it.
		if existing, ok := s.byHash[record.hash]; ok && existing == record {
			delete(s.byHash, record.hash)
		}
	}
}

// Clear resets the store, removing all entries.
func (s *managedFieldsStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.index = make(map[managedFieldsKey]*managedFieldsRecord)
	s.byHash = make(map[uint64]*managedFieldsRecord)
}

// hashManagedFields computes an FNV-1a hash over managedFields content.
func hashManagedFields(mf []metav1.ManagedFieldsEntry) uint64 {
	h := fnv.New64a()
	for i := range mf {
		io.WriteString(h, mf[i].Manager)
		io.WriteString(h, string(mf[i].Operation))
		io.WriteString(h, mf[i].APIVersion)
		io.WriteString(h, mf[i].FieldsType)
		io.WriteString(h, mf[i].Subresource)
		if mf[i].Time != nil {
			b, _ := mf[i].Time.Marshal()
			h.Write(b)
		}
		if mf[i].FieldsV1 != nil {
			h.Write(mf[i].FieldsV1.Raw)
		}
	}
	return h.Sum64()
}

// extractAndClearManagedFields extracts managedFields from an object and sets
// them to nil on the object. Returns the extracted managedFields.
func extractAndClearManagedFields(obj runtime.Object) []metav1.ManagedFieldsEntry {
	accessor, ok := obj.(metav1.ObjectMetaAccessor)
	if !ok {
		return nil
	}
	meta := accessor.GetObjectMeta()
	if meta == nil {
		return nil
	}
	mf := meta.GetManagedFields()
	if len(mf) == 0 {
		return nil
	}
	meta.SetManagedFields(nil)
	return mf
}

// hydratingObject wraps a cachingObject and injects managedFields during
// CacheEncode. This allows objects in the watch cache to have nil managedFields
// while still producing correct serialized output.
type hydratingObject struct {
	*cachingObject
	managedFields []metav1.ManagedFieldsEntry
}

var _ runtime.CacheableObject = &hydratingObject{}

// CacheEncode implements runtime.CacheableObject interface.
// It wraps the encode function to inject managedFields into the deep-copied
// object before serialization.
func (h *hydratingObject) CacheEncode(id runtime.Identifier, encode func(runtime.Object, io.Writer) error, w io.Writer) error {
	wrappedEncode := func(obj runtime.Object, w io.Writer) error {
		// obj is already deep-copied by GetObject() — safe to modify
		if len(h.managedFields) > 0 {
			if accessor, ok := obj.(metav1.ObjectMetaAccessor); ok {
				if meta := accessor.GetObjectMeta(); meta != nil {
					meta.SetManagedFields(h.managedFields)
				}
			}
		}
		return encode(obj, w)
	}
	return h.cachingObject.CacheEncode(id, wrappedEncode, w)
}

// GetObject implements runtime.CacheableObject interface.
// It returns a deep-copy of the wrapped object with managedFields injected.
func (h *hydratingObject) GetObject() runtime.Object {
	obj := h.cachingObject.GetObject()
	if len(h.managedFields) > 0 {
		if accessor, ok := obj.(metav1.ObjectMetaAccessor); ok {
			if meta := accessor.GetObjectMeta(); meta != nil {
				meta.SetManagedFields(h.managedFields)
			}
		}
	}
	return obj
}

// DeepCopyObject implements runtime.Object interface.
func (h *hydratingObject) DeepCopyObject() runtime.Object {
	return &hydratingObject{
		cachingObject: h.cachingObject.DeepCopyObject().(*cachingObject),
		managedFields: h.managedFields,
	}
}
