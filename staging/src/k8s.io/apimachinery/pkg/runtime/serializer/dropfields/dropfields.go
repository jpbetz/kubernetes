/*
Copyright 2026 The Kubernetes Authors.

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

// Package dropfields provides a runtime.Serializer decorator that encodes
// objects as its delegate would, but with metadata.managedFields omitted,
// without mutating the input object and without deep copying it.
package dropfields

import (
	"fmt"
	"io"
	"reflect"
	"strconv"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// NewSerializer returns a runtime.Serializer that encodes objects exactly as
// delegate would, except that metadata.managedFields is omitted from the
// output. The input object is never mutated: encoding operates on a
// structural-sharing shallow copy (see CopyWithoutManagedFields). Decoding is
// delegated unchanged.
func NewSerializer(delegate runtime.Serializer) *Serializer {
	return &Serializer{
		delegate: delegate,
		identifier: runtime.Identifier(
			`{"name":"dropfields","drop":"metadata.managedFields","inner":` +
				strconv.Quote(string(delegate.Identifier())) + `}`),
	}
}

// Serializer decorates a delegate serializer, dropping managedFields on encode.
type Serializer struct {
	delegate   runtime.Serializer
	identifier runtime.Identifier
}

var _ runtime.Serializer = &Serializer{}

// Identifier is distinct from the delegate's identifier so that
// CacheableObject implementations (e.g. the watch cache's cachingObject)
// cache full and stripped encodings as separate entries.
func (s *Serializer) Identifier() runtime.Identifier {
	return s.identifier
}

func (s *Serializer) Encode(obj runtime.Object, w io.Writer) error {
	if co, ok := obj.(runtime.CacheableObject); ok {
		return co.CacheEncode(s.Identifier(), s.doEncode, w)
	}
	return s.doEncode(obj, w)
}

func (s *Serializer) doEncode(obj runtime.Object, w io.Writer) error {
	stripped, err := CopyWithoutManagedFields(obj)
	if err != nil {
		return err
	}
	return s.delegate.Encode(stripped, w)
}

func (s *Serializer) Decode(data []byte, defaults *schema.GroupVersionKind, into runtime.Object) (runtime.Object, *schema.GroupVersionKind, error) {
	return s.delegate.Decode(data, defaults, into)
}

// CopyWithoutManagedFields returns an object equivalent to obj with
// metadata.managedFields cleared, without mutating obj and without deep
// copying it. Only the spine of the object is copied: the top-level struct
// (ObjectMeta is embedded by value, so clearing ManagedFields on the copy
// writes a private slice header), and for lists a fresh Items slice with a
// shallow copy of each item. For unstructured content, the top-level and
// metadata maps are copied and the managedFields key deleted. All remaining
// interior memory is shared with obj, so the returned object must only be
// read (encoded), never mutated.
//
// Objects that carry no object metadata (such as metav1.Status) are returned
// unchanged.
func CopyWithoutManagedFields(obj runtime.Object) (runtime.Object, error) {
	switch t := obj.(type) {
	case *unstructured.Unstructured:
		return &unstructured.Unstructured{Object: contentWithoutManagedFields(t.Object)}, nil
	case *unstructured.UnstructuredList:
		items := make([]unstructured.Unstructured, len(t.Items))
		for i := range t.Items {
			items[i] = unstructured.Unstructured{Object: contentWithoutManagedFields(t.Items[i].Object)}
		}
		return &unstructured.UnstructuredList{Object: t.Object, Items: items}, nil
	}
	if meta.IsListType(obj) {
		return listCopyWithoutManagedFields(obj)
	}
	if _, err := meta.Accessor(obj); err != nil {
		return obj, nil
	}
	return itemCopyWithoutManagedFields(obj)
}

// contentWithoutManagedFields path-copies unstructured content: a fresh
// top-level map and a fresh metadata map with the managedFields key deleted.
// It must never use Unstructured.SetManagedFields, which removes the field
// from the shared nested map in place.
func contentWithoutManagedFields(content map[string]interface{}) map[string]interface{} {
	metadata, ok := content["metadata"].(map[string]interface{})
	if !ok {
		return content
	}
	if _, ok := metadata["managedFields"]; !ok {
		return content
	}
	newContent := make(map[string]interface{}, len(content))
	for k, v := range content {
		newContent[k] = v
	}
	newMetadata := make(map[string]interface{}, len(metadata))
	for k, v := range metadata {
		newMetadata[k] = v
	}
	delete(newMetadata, "managedFields")
	newContent["metadata"] = newMetadata
	return newContent
}

func itemCopyWithoutManagedFields(obj runtime.Object) (runtime.Object, error) {
	orig, err := meta.Accessor(obj)
	if err != nil {
		return obj, nil
	}
	if len(orig.GetManagedFields()) == 0 {
		return obj, nil
	}
	v := reflect.ValueOf(obj)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return obj, fmt.Errorf("expected pointer to object, got %T", obj)
	}
	cp := reflect.New(v.Type().Elem())
	cp.Elem().Set(v.Elem())
	out := cp.Interface().(runtime.Object)
	acc, err := meta.Accessor(out)
	if err != nil {
		return nil, err
	}
	// Guard against types whose object metadata is not embedded by value: if
	// the copy's accessor aliases the original's, clearing ManagedFields would
	// write through shared memory, so fall back to a private deep copy.
	if acc == orig {
		out = obj.DeepCopyObject()
		if acc, err = meta.Accessor(out); err != nil {
			return nil, err
		}
	}
	acc.SetManagedFields(nil)
	return out, nil
}

func listCopyWithoutManagedFields(list runtime.Object) (runtime.Object, error) {
	v := reflect.ValueOf(list)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return list, fmt.Errorf("expected pointer to list, got %T", list)
	}
	cp := reflect.New(v.Type().Elem())
	cp.Elem().Set(v.Elem())

	items := cp.Elem().FieldByName("Items")
	if !items.IsValid() || items.Kind() != reflect.Slice {
		return deepCopyWithoutManagedFields(list)
	}
	origItems := v.Elem().FieldByName("Items")

	fresh := reflect.MakeSlice(items.Type(), items.Len(), items.Len())
	reflect.Copy(fresh, items)
	items.Set(fresh)

	for i := 0; i < fresh.Len(); i++ {
		item := fresh.Index(i)
		if item.Kind() == reflect.Pointer {
			// Items of pointer type share their pointees with the original
			// list; replace each entry with a stripped copy.
			itemObj, ok := item.Interface().(runtime.Object)
			if !ok || item.IsNil() {
				continue
			}
			stripped, err := itemCopyWithoutManagedFields(itemObj)
			if err != nil {
				return nil, err
			}
			item.Set(reflect.ValueOf(stripped))
			continue
		}
		if !item.CanAddr() {
			continue
		}
		acc, err := meta.Accessor(item.Addr().Interface())
		if err != nil {
			// Items without object metadata (e.g. RawExtension) pass through.
			continue
		}
		// Same aliasing guard as itemCopyWithoutManagedFields: the copied item
		// must expose its own metadata, or clearing would corrupt the original.
		if origAcc, err := meta.Accessor(origItems.Index(i).Addr().Interface()); err == nil && acc == origAcc {
			return deepCopyWithoutManagedFields(list)
		}
		acc.SetManagedFields(nil)
	}
	return cp.Interface().(runtime.Object), nil
}

// deepCopyWithoutManagedFields is the fallback for shapes the spine copy does
// not understand. It pays the full deep copy cost, but is always correct.
func deepCopyWithoutManagedFields(obj runtime.Object) (runtime.Object, error) {
	out := obj.DeepCopyObject()
	if meta.IsListType(out) {
		err := meta.EachListItem(out, func(o runtime.Object) error {
			if acc, err := meta.Accessor(o); err == nil {
				acc.SetManagedFields(nil)
			}
			return nil
		})
		return out, err
	}
	if acc, err := meta.Accessor(out); err == nil {
		acc.SetManagedFields(nil)
	}
	return out, nil
}
