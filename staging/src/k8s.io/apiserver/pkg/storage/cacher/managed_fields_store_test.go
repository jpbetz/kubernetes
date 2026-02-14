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
	"bytes"
	"fmt"
	"io"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	examplev1 "k8s.io/apiserver/pkg/apis/example/v1"
	"k8s.io/apiserver/pkg/features"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/cacher/store"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/tools/cache"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
)

func makeManagedFields(manager string, raw []byte) []metav1.ManagedFieldsEntry {
	return []metav1.ManagedFieldsEntry{
		{
			Manager:    manager,
			Operation:  metav1.ManagedFieldsOperationApply,
			APIVersion: "v1",
			FieldsType: "FieldsV1",
			FieldsV1:   &metav1.FieldsV1{Raw: raw},
		},
	}
}

func TestManagedFieldsStoreAddGet(t *testing.T) {
	store := newManagedFieldsStore()
	mf := makeManagedFields("kubectl", []byte(`{"f:metadata":{}}`))

	store.Add("key1", 100, mf, 1)

	got := store.Get("key1", 100)
	if got == nil {
		t.Fatal("expected to get managedFields, got nil")
	}
	if len(got) != 1 || got[0].Manager != "kubectl" {
		t.Fatalf("unexpected managedFields: %v", got)
	}
}

func TestManagedFieldsStoreGetNotFound(t *testing.T) {
	store := newManagedFieldsStore()

	got := store.Get("key1", 100)
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestManagedFieldsStoreRelease(t *testing.T) {
	store := newManagedFieldsStore()
	mf := makeManagedFields("kubectl", []byte(`{"f:metadata":{}}`))

	store.Add("key1", 100, mf, 2)

	// First release should not remove it.
	store.Release("key1", 100)
	got := store.Get("key1", 100)
	if got == nil {
		t.Fatal("expected managedFields to still exist after first release")
	}

	// Second release should remove it.
	store.Release("key1", 100)
	got = store.Get("key1", 100)
	if got != nil {
		t.Fatalf("expected nil after all refs released, got %v", got)
	}
}

func TestManagedFieldsStoreContentDedup(t *testing.T) {
	store := newManagedFieldsStore()
	raw := []byte(`{"f:metadata":{}}`)

	mf1 := makeManagedFields("kubectl", raw)
	mf2 := makeManagedFields("kubectl", raw)

	store.Add("key1", 100, mf1, 1)
	store.Add("key2", 200, mf2, 1)

	// Both should be present.
	got1 := store.Get("key1", 100)
	got2 := store.Get("key2", 200)
	if got1 == nil || got2 == nil {
		t.Fatal("expected both entries to exist")
	}

	// They should share the same underlying record (content dedup).
	store.mu.RLock()
	rec1 := store.index[managedFieldsKey{objectKey: "key1", resourceVersion: 100}]
	rec2 := store.index[managedFieldsKey{objectKey: "key2", resourceVersion: 200}]
	store.mu.RUnlock()
	if rec1 != rec2 {
		t.Fatal("expected content-addressed dedup to share the same record")
	}

	// Releasing one should not affect the other.
	store.Release("key1", 100)
	got2 = store.Get("key2", 200)
	if got2 == nil {
		t.Fatal("expected key2 to still exist after releasing key1")
	}
}

func TestManagedFieldsStoreDifferentContent(t *testing.T) {
	store := newManagedFieldsStore()

	mf1 := makeManagedFields("kubectl", []byte(`{"f:metadata":{}}`))
	mf2 := makeManagedFields("kube-controller-manager", []byte(`{"f:status":{}}`))

	store.Add("key1", 100, mf1, 1)
	store.Add("key2", 200, mf2, 1)

	// They should NOT share the same record.
	store.mu.RLock()
	rec1 := store.index[managedFieldsKey{objectKey: "key1", resourceVersion: 100}]
	rec2 := store.index[managedFieldsKey{objectKey: "key2", resourceVersion: 200}]
	store.mu.RUnlock()
	if rec1 == rec2 {
		t.Fatal("expected different content to have different records")
	}
}

func TestManagedFieldsStoreAddSameKeyRVIncrementsRefs(t *testing.T) {
	store := newManagedFieldsStore()
	mf := makeManagedFields("kubectl", []byte(`{"f:metadata":{}}`))

	store.Add("key1", 100, mf, 1)
	store.Add("key1", 100, mf, 1) // second add for same key/rv

	// Should need 2 releases.
	store.Release("key1", 100)
	got := store.Get("key1", 100)
	if got == nil {
		t.Fatal("expected entry to still exist after one release")
	}

	store.Release("key1", 100)
	got = store.Get("key1", 100)
	if got != nil {
		t.Fatalf("expected entry to be removed after two releases, got %v", got)
	}
}

func TestManagedFieldsStoreClear(t *testing.T) {
	store := newManagedFieldsStore()
	mf := makeManagedFields("kubectl", []byte(`{"f:metadata":{}}`))

	store.Add("key1", 100, mf, 1)
	store.Add("key2", 200, mf, 1)

	store.Clear()

	if got := store.Get("key1", 100); got != nil {
		t.Fatalf("expected nil after clear, got %v", got)
	}
	if got := store.Get("key2", 200); got != nil {
		t.Fatalf("expected nil after clear, got %v", got)
	}
}

func TestManagedFieldsStoreNilManagedFields(t *testing.T) {
	store := newManagedFieldsStore()

	// Adding nil managedFields should be a no-op.
	store.Add("key1", 100, nil, 1)
	if got := store.Get("key1", 100); got != nil {
		t.Fatalf("expected nil for nil managedFields, got %v", got)
	}

	// Releasing a non-existent entry should not panic.
	store.Release("key1", 100)
}

func TestExtractAndClearManagedFields(t *testing.T) {
	pod := &examplev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			ManagedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:    "kubectl",
					Operation:  metav1.ManagedFieldsOperationApply,
					APIVersion: "v1",
				},
			},
		},
	}

	mf := extractAndClearManagedFields(pod)
	if len(mf) != 1 || mf[0].Manager != "kubectl" {
		t.Fatalf("unexpected extracted managedFields: %v", mf)
	}

	// Object should have nil managedFields.
	if pod.ManagedFields != nil {
		t.Fatal("expected object's managedFields to be nil after extraction")
	}
}

func TestHydratingObjectCacheEncode(t *testing.T) {
	pod := &examplev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	co, err := newCachingObject(pod)
	if err != nil {
		t.Fatal(err)
	}

	mf := []metav1.ManagedFieldsEntry{
		{
			Manager:    "kubectl",
			Operation:  metav1.ManagedFieldsOperationApply,
			APIVersion: "v1",
		},
	}

	ho := &hydratingObject{
		cachingObject: co,
		managedFields: mf,
	}

	// CacheEncode should inject managedFields.
	id := runtime.Identifier("test")
	var encoded bytes.Buffer
	encodeFunc := func(obj runtime.Object, w io.Writer) error {
		accessor, ok := obj.(metav1.ObjectMetaAccessor)
		if !ok {
			t.Fatal("expected object to implement ObjectMetaAccessor")
		}
		gotMF := accessor.GetObjectMeta().GetManagedFields()
		if len(gotMF) != 1 || gotMF[0].Manager != "kubectl" {
			t.Fatalf("expected managedFields to be injected, got %v", gotMF)
		}
		_, err := w.Write([]byte("encoded"))
		return err
	}
	err = ho.CacheEncode(id, encodeFunc, &encoded)
	if err != nil {
		t.Fatal(err)
	}

	// The underlying object should still have nil managedFields.
	co.lock.RLock()
	objMF := co.object.GetManagedFields()
	co.lock.RUnlock()
	if objMF != nil {
		t.Fatal("expected underlying object to still have nil managedFields")
	}
}

func TestHydratingObjectGetObject(t *testing.T) {
	pod := &examplev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	co, err := newCachingObject(pod)
	if err != nil {
		t.Fatal(err)
	}

	mf := []metav1.ManagedFieldsEntry{
		{Manager: "kubectl"},
	}

	ho := &hydratingObject{
		cachingObject: co,
		managedFields: mf,
	}

	obj := ho.GetObject()
	accessor := obj.(metav1.ObjectMetaAccessor)
	gotMF := accessor.GetObjectMeta().GetManagedFields()
	if len(gotMF) != 1 || gotMF[0].Manager != "kubectl" {
		t.Fatalf("expected GetObject to return object with managedFields, got %v", gotMF)
	}
}

func TestHydratingObjectGetObjectKind(t *testing.T) {
	pod := &examplev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}
	pod.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"})

	co, err := newCachingObject(pod)
	if err != nil {
		t.Fatal(err)
	}

	ho := &hydratingObject{
		cachingObject: co,
		managedFields: nil,
	}

	gvk := ho.GetObjectKind().GroupVersionKind()
	if gvk.Kind != "Pod" {
		t.Fatalf("expected Kind=Pod, got %v", gvk)
	}
}

func TestHashManagedFields(t *testing.T) {
	mf1 := makeManagedFields("kubectl", []byte(`{"f:metadata":{}}`))
	mf2 := makeManagedFields("kubectl", []byte(`{"f:metadata":{}}`))
	mf3 := makeManagedFields("kube-controller-manager", []byte(`{"f:status":{}}`))

	h1 := hashManagedFields(mf1)
	h2 := hashManagedFields(mf2)
	h3 := hashManagedFields(mf3)

	if h1 != h2 {
		t.Fatal("expected identical managedFields to have the same hash")
	}
	if h1 == h3 {
		t.Fatal("expected different managedFields to have different hashes")
	}
}

func podWithManagedFields(name string, rv string, manager string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       "default",
			ResourceVersion: rv,
			ManagedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:    manager,
					Operation:  metav1.ManagedFieldsOperationApply,
					APIVersion: "v1",
					FieldsType: "FieldsV1",
					FieldsV1:   &metav1.FieldsV1{Raw: []byte(`{"f:metadata":{}}`)},
				},
			},
		},
		Spec: v1.PodSpec{NodeName: "node1"},
	}
}

// TestWatchCacheStripHydrateRoundtrip tests that processEvent strips managedFields
// and HydrateManagedFields restores them, with both gate states.
func TestWatchCacheStripHydrateRoundtrip(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("sideStore=%v", enabled), func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.WatchCacheManagedFieldsSideStore, enabled)
			s := newTestWatchCache(10, DefaultEventFreshDuration, &cache.Indexers{})
			defer s.Stop()

			pod := podWithManagedFields("pod1", "100", "kubectl")

			err := s.Add(pod)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Retrieve from store.
			obj, exists, err := s.store.GetByKey("/prefix/default/pod1")
			if err != nil || !exists {
				t.Fatalf("object not found in store: %v", err)
			}
			elem := obj.(*store.Element)

			if enabled {
				// Object in store should have nil managedFields.
				accessor := elem.Object.(metav1.ObjectMetaAccessor)
				if accessor.GetObjectMeta().GetManagedFields() != nil {
					t.Fatal("expected managedFields to be nil in store when side store is enabled")
				}

				// HydrateManagedFields should restore them.
				restored := elem.Object.DeepCopyObject()
				s.HydrateManagedFields("/prefix/default/pod1", restored)
				restoredAccessor := restored.(metav1.ObjectMetaAccessor)
				mf := restoredAccessor.GetObjectMeta().GetManagedFields()
				if len(mf) != 1 || mf[0].Manager != "kubectl" {
					t.Fatalf("expected managedFields to be restored, got %v", mf)
				}
			} else {
				// Without side store, managedFields should be present on object.
				accessor := elem.Object.(metav1.ObjectMetaAccessor)
				mf := accessor.GetObjectMeta().GetManagedFields()
				if len(mf) != 1 || mf[0].Manager != "kubectl" {
					t.Fatalf("expected managedFields on object, got %v", mf)
				}
			}
		})
	}
}

// TestWatchCacheEvictionRelease tests that evicting events from the ring buffer
// properly releases side store references.
func TestWatchCacheEvictionRelease(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("sideStore=%v", enabled), func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.WatchCacheManagedFieldsSideStore, enabled)
			// Small capacity to force eviction.
			s := newTestWatchCache(3, DefaultEventFreshDuration, &cache.Indexers{})
			defer s.Stop()

			// Add 5 objects to force eviction of the first 2.
			for i := 0; i < 5; i++ {
				pod := podWithManagedFields(fmt.Sprintf("pod%d", i), fmt.Sprintf("%d", 100+i), "kubectl")
				if err := s.Add(pod); err != nil {
					t.Fatalf("unexpected error adding pod%d: %v", i, err)
				}
			}

			if !enabled || s.managedFieldsStore == nil {
				return
			}

			// The first 2 events should have been evicted from the ring buffer.
			// Their ring buffer refs should be released.
			// But their store refs should still exist since the objects are still in the store.
			for i := 0; i < 5; i++ {
				key := fmt.Sprintf("/prefix/default/pod%d", i)
				rv := uint64(100 + i)
				mf := s.managedFieldsStore.Get(key, rv)
				// Store refs should still exist for all objects.
				if mf == nil {
					t.Fatalf("expected managedFields for pod%d (store ref), got nil", i)
				}
			}
		})
	}
}

// TestWatchCacheReplaceRebuildsSideStore tests that Replace clears and rebuilds
// the side store.
func TestWatchCacheReplaceRebuildsSideStore(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("sideStore=%v", enabled), func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.WatchCacheManagedFieldsSideStore, enabled)
			s := newTestWatchCache(10, DefaultEventFreshDuration, &cache.Indexers{})
			defer s.Stop()

			// Add an object first.
			pod1 := podWithManagedFields("pod1", "100", "kubectl")
			if err := s.Add(pod1); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Replace with new objects.
			pod2 := podWithManagedFields("pod2", "200", "kube-controller-manager")
			pod3 := podWithManagedFields("pod3", "201", "kubectl")
			err := s.Replace([]interface{}{pod2, pod3}, "201")
			if err != nil {
				t.Fatalf("unexpected error on replace: %v", err)
			}

			if !enabled || s.managedFieldsStore == nil {
				return
			}

			// Old entry should be gone.
			if mf := s.managedFieldsStore.Get("/prefix/default/pod1", 100); mf != nil {
				t.Fatal("expected old entry to be cleared after Replace")
			}

			// New entries should be present.
			mf2 := s.managedFieldsStore.Get("/prefix/default/pod2", 200)
			if len(mf2) != 1 || mf2[0].Manager != "kube-controller-manager" {
				t.Fatalf("unexpected managedFields for pod2: %v", mf2)
			}
			mf3 := s.managedFieldsStore.Get("/prefix/default/pod3", 201)
			if len(mf3) != 1 || mf3[0].Manager != "kubectl" {
				t.Fatalf("unexpected managedFields for pod3: %v", mf3)
			}

			// Objects in store should have nil managedFields.
			obj, exists, err := s.store.GetByKey("/prefix/default/pod2")
			if err != nil || !exists {
				t.Fatalf("pod2 not found in store: %v", err)
			}
			accessor := obj.(*store.Element).Object.(metav1.ObjectMetaAccessor)
			if accessor.GetObjectMeta().GetManagedFields() != nil {
				t.Fatal("expected managedFields to be nil in store after Replace")
			}
		})
	}
}

// TestWatchCacheDeleteReleasesStoreRef tests that deleting an object releases
// the store reference in the side store.
func TestWatchCacheDeleteReleasesStoreRef(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("sideStore=%v", enabled), func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.WatchCacheManagedFieldsSideStore, enabled)
			s := newTestWatchCache(10, DefaultEventFreshDuration, &cache.Indexers{})
			defer s.Stop()

			pod := podWithManagedFields("pod1", "100", "kubectl")
			if err := s.Add(pod); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Delete the object.
			pod.ResourceVersion = "101"
			if err := s.Delete(pod); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !enabled || s.managedFieldsStore == nil {
				return
			}

			// Store ref for the old RV should be released.
			// Only the ring buffer ref for the add event (rv=100) and
			// the ring buffer ref for the delete event (rv=101) should remain.
			if _, ok := s.storeRVs["/prefix/default/pod1"]; ok {
				t.Fatal("expected storeRVs entry to be deleted after Delete")
			}
		})
	}
}

// TestSetCachingObjectsWithSideStore tests that setCachingObjects properly
// wraps objects in hydratingObject when the side store is enabled.
func TestSetCachingObjectsWithSideStore(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("sideStore=%v", enabled), func(t *testing.T) {
			var mfStore *managedFieldsStore
			if enabled {
				mfStore = newManagedFieldsStore()
				mfStore.Add("key1", 100, makeManagedFields("kubectl", []byte(`{"f:metadata":{}}`)), 1)
			}

			versioner := storage.APIObjectVersioner{}
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "pod1",
					Namespace:       "default",
					ResourceVersion: "100",
				},
			}

			event := &watchCacheEvent{
				Type:            watch.Added,
				Object:          pod,
				Key:             "key1",
				ResourceVersion: 100,
			}

			setCachingObjects(event, versioner, mfStore)

			if enabled {
				ho, ok := event.Object.(*hydratingObject)
				if !ok {
					t.Fatalf("expected hydratingObject, got %T", event.Object)
				}
				if len(ho.managedFields) != 1 || ho.managedFields[0].Manager != "kubectl" {
					t.Fatalf("unexpected managedFields on hydratingObject: %v", ho.managedFields)
				}
			} else {
				_, ok := event.Object.(*cachingObject)
				if !ok {
					t.Fatalf("expected cachingObject, got %T", event.Object)
				}
			}
		})
	}
}

// TestSetCachingObjectsDeleteWithSideStore tests the delete path of
// setCachingObjects with the side store.
func TestSetCachingObjectsDeleteWithSideStore(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("sideStore=%v", enabled), func(t *testing.T) {
			var mfStore *managedFieldsStore
			if enabled {
				mfStore = newManagedFieldsStore()
				// Store MF under the event's RV (how processEvent stores delete MFs).
				mfStore.Add("key1", 101, makeManagedFields("kubectl", []byte(`{"f:metadata":{}}`)), 1)
			}

			versioner := storage.APIObjectVersioner{}
			prevPod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "pod1",
					Namespace:       "default",
					ResourceVersion: "100",
				},
			}

			event := &watchCacheEvent{
				Type:            watch.Deleted,
				Object:          prevPod,
				PrevObject:      prevPod,
				Key:             "key1",
				ResourceVersion: 101,
			}

			setCachingObjects(event, versioner, mfStore)

			if enabled {
				ho, ok := event.PrevObject.(*hydratingObject)
				if !ok {
					t.Fatalf("expected hydratingObject for PrevObject, got %T", event.PrevObject)
				}
				if len(ho.managedFields) != 1 || ho.managedFields[0].Manager != "kubectl" {
					t.Fatalf("unexpected managedFields on hydratingObject: %v", ho.managedFields)
				}
			} else {
				_, ok := event.PrevObject.(*cachingObject)
				if !ok {
					t.Fatalf("expected cachingObject for PrevObject, got %T", event.PrevObject)
				}
			}
		})
	}
}

// TestCacheWatcherHydrateManagedFields tests the cacheWatcher hydration callback.
func TestCacheWatcherHydrateManagedFields(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("sideStore=%v", enabled), func(t *testing.T) {
			cw := &cacheWatcher{
				versioner: storage.APIObjectVersioner{},
			}

			if enabled {
				mfStore := newManagedFieldsStore()
				mfStore.Add("key1", 100, makeManagedFields("kubectl", []byte(`{"f:metadata":{}}`)), 1)
				cw.getManagedFields = mfStore.Get
			}

			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "pod1",
					Namespace:       "default",
					ResourceVersion: "100",
				},
			}

			cw.hydrateManagedFields(pod, "key1", 100)

			mf := pod.GetManagedFields()
			if enabled {
				if len(mf) != 1 || mf[0].Manager != "kubectl" {
					t.Fatalf("expected managedFields to be hydrated, got %v", mf)
				}
			} else {
				if len(mf) != 0 {
					t.Fatalf("expected no managedFields without side store, got %v", mf)
				}
			}
		})
	}
}

// TestCacheWatcherSkipsHydrationForCachingObject verifies that hydrateManagedFields
// skips cachingObject and hydratingObject (those handle hydration via CacheEncode).
func TestCacheWatcherSkipsHydrationForCachingObject(t *testing.T) {
	mfStore := newManagedFieldsStore()
	mfStore.Add("key1", 100, makeManagedFields("kubectl", []byte(`{"f:metadata":{}}`)), 1)

	cw := &cacheWatcher{
		versioner:       storage.APIObjectVersioner{},
		getManagedFields: mfStore.Get,
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "pod1",
			ResourceVersion: "100",
		},
	}
	co, _ := newCachingObject(pod)

	// Should not modify cachingObject.
	cw.hydrateManagedFields(co, "key1", 100)

	// Should not modify hydratingObject.
	ho := &hydratingObject{cachingObject: co}
	cw.hydrateManagedFields(ho, "key1", 100)
}

// TestWatchCacheUpdateReleasesOldStoreRef tests that updating an object
// releases the old store ref and creates a new one.
func TestWatchCacheUpdateReleasesOldStoreRef(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("sideStore=%v", enabled), func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.WatchCacheManagedFieldsSideStore, enabled)
			s := newTestWatchCache(10, DefaultEventFreshDuration, &cache.Indexers{})
			defer s.Stop()

			// Add an object.
			pod := podWithManagedFields("pod1", "100", "kubectl")
			if err := s.Add(pod); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Update the object.
			pod2 := podWithManagedFields("pod1", "101", "kubectl")
			if err := s.Update(pod2); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !enabled || s.managedFieldsStore == nil {
				return
			}

			// storeRVs should track the new RV.
			if rv, ok := s.storeRVs["/prefix/default/pod1"]; !ok || rv != 101 {
				t.Fatalf("expected storeRVs to track rv=101, got %v", rv)
			}

			// New entry should be retrievable.
			mf := s.managedFieldsStore.Get("/prefix/default/pod1", 101)
			if len(mf) != 1 || mf[0].Manager != "kubectl" {
				t.Fatalf("unexpected managedFields for updated object: %v", mf)
			}

			// Old entry should still exist (ring buffer ref), but store ref released.
			// It was added with refCount=2, store ref released = 1 remaining (ring buffer).
			mfOld := s.managedFieldsStore.Get("/prefix/default/pod1", 100)
			if mfOld == nil {
				t.Fatal("expected old entry to still exist (ring buffer ref)")
			}
		})
	}
}

// TestClearManagedFields tests the clearManagedFields helper.
func TestClearManagedFields(t *testing.T) {
	pod := podWithManagedFields("pod1", "100", "kubectl")
	if len(pod.GetManagedFields()) == 0 {
		t.Fatal("expected managedFields to be set")
	}
	clearManagedFields(pod)
	if len(pod.GetManagedFields()) != 0 {
		t.Fatal("expected managedFields to be nil after clear")
	}
}

// TestClearManagedFieldsFromList tests the clearManagedFieldsFromList helper.
func TestClearManagedFieldsFromList(t *testing.T) {
	podList := &v1.PodList{
		Items: []v1.Pod{
			*podWithManagedFields("pod1", "100", "kubectl"),
			*podWithManagedFields("pod2", "101", "controller"),
		},
	}
	for i := range podList.Items {
		if len(podList.Items[i].GetManagedFields()) == 0 {
			t.Fatalf("expected managedFields on pod %d", i)
		}
	}
	clearManagedFieldsFromList(podList)
	for i := range podList.Items {
		if len(podList.Items[i].GetManagedFields()) != 0 {
			t.Fatalf("expected managedFields to be nil on pod %d after clear", i)
		}
	}
}

// TestConvertToWatchEventExcludeManagedFields tests that watch events with
// excludeManagedFields set return plain objects without managedFields.
func TestConvertToWatchEventExcludeManagedFields(t *testing.T) {
	for _, sideStoreEnabled := range []bool{true, false} {
		for _, excludeMF := range []bool{true, false} {
			t.Run(fmt.Sprintf("sideStore=%v/excludeMF=%v", sideStoreEnabled, excludeMF), func(t *testing.T) {
				var mfStore *managedFieldsStore
				if sideStoreEnabled {
					mfStore = newManagedFieldsStore()
					mfStore.Add("key1", 100, makeManagedFields("kubectl", []byte(`{"f:metadata":{}}`)), 1)
				}

				cw := &cacheWatcher{
					versioner:            storage.APIObjectVersioner{},
					filter:               func(_ string, _ labels.Set, _ fields.Set) bool { return true },
					excludeManagedFields: excludeMF,
				}
				if mfStore != nil {
					cw.getManagedFields = mfStore.Get
				}

				pod := podWithManagedFields("pod1", "100", "kubectl")
				// Simulate what processEvent does: strip MF when side store is active.
				if sideStoreEnabled {
					extractAndClearManagedFields(pod)
				}
				event := &watchCacheEvent{
					Type:            watch.Added,
					Object:          pod,
					Key:             "key1",
					ResourceVersion: 100,
					ObjLabels:       labels.Set{},
					ObjFields:       fields.Set{},
				}
				setCachingObjects(event, storage.APIObjectVersioner{}, mfStore)

				watchEvent := cw.convertToWatchEvent(event)
				if watchEvent == nil {
					t.Fatal("expected watch event, got nil")
				}

				if excludeMF {
					// With excludeMF, the result should be a plain object (not CacheableObject)
					if _, ok := watchEvent.Object.(runtime.CacheableObject); ok {
						t.Fatal("expected plain object, got CacheableObject")
					}
					accessor := watchEvent.Object.(metav1.ObjectMetaAccessor)
					mf := accessor.GetObjectMeta().GetManagedFields()
					if len(mf) != 0 {
						t.Fatalf("expected no managedFields with excludeMF, got %v", mf)
					}
				} else {
					if sideStoreEnabled {
						// With side store, result is hydratingObject (CacheableObject)
						if _, ok := watchEvent.Object.(runtime.CacheableObject); !ok {
							t.Fatal("expected CacheableObject with side store")
						}
					}
				}
			})
		}
	}
}

// TestConvertToWatchEventExcludeManagedFieldsDelete tests that delete events with
// excludeManagedFields preserve the correct resourceVersion.
func TestConvertToWatchEventExcludeManagedFieldsDelete(t *testing.T) {
	for _, sideStoreEnabled := range []bool{true, false} {
		for _, excludeMF := range []bool{true, false} {
			t.Run(fmt.Sprintf("sideStore=%v/excludeMF=%v", sideStoreEnabled, excludeMF), func(t *testing.T) {
				var mfStore *managedFieldsStore
				if sideStoreEnabled {
					mfStore = newManagedFieldsStore()
					mfStore.Add("key1", 101, makeManagedFields("kubectl", []byte(`{"f:metadata":{}}`)), 1)
				}

				versioner := storage.APIObjectVersioner{}
				cw := &cacheWatcher{
					versioner:            versioner,
					filter:               func(_ string, _ labels.Set, _ fields.Set) bool { return true },
					excludeManagedFields: excludeMF,
				}
				if mfStore != nil {
					cw.getManagedFields = mfStore.Get
				}

				prevPod := podWithManagedFields("pod1", "100", "kubectl")
				// Simulate what processEvent does: strip MF when side store is active.
				if sideStoreEnabled {
					extractAndClearManagedFields(prevPod)
				}
				event := &watchCacheEvent{
					Type:            watch.Deleted,
					Object:          prevPod,
					PrevObject:      prevPod,
					Key:             "key1",
					ResourceVersion: 101,
					PrevObjLabels:   labels.Set{},
					PrevObjFields:   fields.Set{},
				}
				setCachingObjects(event, versioner, mfStore)

				watchEvent := cw.convertToWatchEvent(event)
				if watchEvent == nil {
					t.Fatal("expected watch event, got nil")
				}
				if watchEvent.Type != watch.Deleted {
					t.Fatalf("expected Deleted event, got %v", watchEvent.Type)
				}

				// Verify resourceVersion is updated to the event's RV.
				rv, err := versioner.ObjectResourceVersion(watchEvent.Object)
				if err != nil {
					t.Fatalf("failed to get resourceVersion: %v", err)
				}
				if rv != 101 {
					t.Fatalf("expected resourceVersion 101, got %d", rv)
				}

				if excludeMF {
					if _, ok := watchEvent.Object.(runtime.CacheableObject); ok {
						t.Fatal("expected plain object with excludeMF, got CacheableObject")
					}
					accessor := watchEvent.Object.(metav1.ObjectMetaAccessor)
					mf := accessor.GetObjectMeta().GetManagedFields()
					if len(mf) != 0 {
						t.Fatalf("expected no managedFields with excludeMF, got %v", mf)
					}
				}
			})
		}
	}
}

// TestCacheWatcherExcludeManagedFieldsInitialEvents tests that initial events
// from the ring buffer also omit managedFields when excludeManagedFields is set.
func TestCacheWatcherExcludeManagedFieldsInitialEvents(t *testing.T) {
	for _, sideStoreEnabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("sideStore=%v", sideStoreEnabled), func(t *testing.T) {
			var mfStore *managedFieldsStore
			if sideStoreEnabled {
				mfStore = newManagedFieldsStore()
				mfStore.Add("key1", 100, makeManagedFields("kubectl", []byte(`{"f:metadata":{}}`)), 1)
			}

			cw := &cacheWatcher{
				versioner:            storage.APIObjectVersioner{},
				filter:               func(_ string, _ labels.Set, _ fields.Set) bool { return true },
				excludeManagedFields: true,
			}
			if mfStore != nil {
				cw.getManagedFields = mfStore.Get
			}

			// Simulate an initial event with a plain object (not wrapped in cachingObject).
			pod := podWithManagedFields("pod1", "100", "kubectl")
			event := &watchCacheEvent{
				Type:            watch.Added,
				Object:          pod,
				Key:             "key1",
				ResourceVersion: 100,
				ObjLabels:       labels.Set{},
				ObjFields:       fields.Set{},
			}
			// Don't call setCachingObjects to simulate initial events from ring buffer
			// which may have plain objects.

			watchEvent := cw.convertToWatchEvent(event)
			if watchEvent == nil {
				t.Fatal("expected watch event, got nil")
			}

			accessor := watchEvent.Object.(metav1.ObjectMetaAccessor)
			mf := accessor.GetObjectMeta().GetManagedFields()
			if len(mf) != 0 {
				t.Fatalf("expected no managedFields for initial event with excludeMF, got %v", mf)
			}
		})
	}
}

