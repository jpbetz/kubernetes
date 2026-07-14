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

package dropfields_test

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	testapigroupv1 "k8s.io/apimachinery/pkg/apis/testapigroup/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/cbor"
	"k8s.io/apimachinery/pkg/runtime/serializer/dropfields"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/runtime/serializer/protobuf"
)

const fieldsV1 = `{"f:metadata":{"f:labels":{"f:app":{},"f:tier":{},"f:track":{}},"f:annotations":{"f:checksum/config":{}}},"f:spec":{"f:restartPolicy":{},"f:serviceAccountName":{},"f:nodeSelector":{"f:disktype":{}},"f:terminationGracePeriodSeconds":{}},"f:status":{"f:conditions":{"k:{\"type\":\"Ready\"}":{".":{},"f:status":{},"f:reason":{},"f:message":{}}}}}`

func newCarp(name string) *testapigroupv1.Carp {
	grace := int64(30)
	return &testapigroupv1.Carp{
		TypeMeta: metav1.TypeMeta{APIVersion: "testapigroup.apimachinery.k8s.io/v1", Kind: "Carp"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			UID:         "12345678-1234-1234-1234-123456789012",
			Labels:      map[string]string{"app": name, "tier": "backend", "track": "stable"},
			Annotations: map[string]string{"checksum/config": "abc123def456"},
			ManagedFields: []metav1.ManagedFieldsEntry{
				{Manager: "kube-controller-manager", Operation: metav1.ManagedFieldsOperationUpdate, APIVersion: "v1", FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(fieldsV1)}},
				{Manager: "kube-scheduler", Operation: metav1.ManagedFieldsOperationUpdate, APIVersion: "v1", FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:status":{"f:conditions":{"k:{\"type\":\"CarpScheduled\"}":{".":{},"f:status":{}}}}}`)}},
				{Manager: "kubelet", Operation: metav1.ManagedFieldsOperationUpdate, APIVersion: "v1", Subresource: "status", FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(fieldsV1)}},
			},
		},
		Spec: testapigroupv1.CarpSpec{
			RestartPolicy:                 testapigroupv1.RestartPolicy("Always"),
			ServiceAccountName:            "default",
			NodeSelector:                  map[string]string{"disktype": "ssd"},
			TerminationGracePeriodSeconds: &grace,
			NodeName:                      "node-1",
			Hostname:                      name,
			Subdomain:                     "cluster.local",
		},
		Status: testapigroupv1.CarpStatus{
			Phase:   testapigroupv1.CarpPhase("Running"),
			HostIP:  "10.0.0.1",
			CarpIP:  "192.168.1.1",
			Message: "running happily",
			Conditions: []testapigroupv1.CarpCondition{
				{Type: testapigroupv1.CarpConditionType("Ready"), Status: testapigroupv1.ConditionStatus("True"), Reason: "Started", Message: "carp is ready"},
			},
		},
	}
}

func newCarpList(n int) *testapigroupv1.CarpList {
	list := &testapigroupv1.CarpList{
		TypeMeta: metav1.TypeMeta{APIVersion: "testapigroup.apimachinery.k8s.io/v1", Kind: "CarpList"},
		ListMeta: metav1.ListMeta{ResourceVersion: "12345"},
	}
	for i := 0; i < n; i++ {
		list.Items = append(list.Items, *newCarp(fmt.Sprintf("carp-%d", i)))
	}
	return list
}

func TestCopyWithoutManagedFieldsTyped(t *testing.T) {
	orig := newCarp("a")
	out, err := dropfields.CopyWithoutManagedFields(orig)
	if err != nil {
		t.Fatal(err)
	}
	stripped := out.(*testapigroupv1.Carp)

	if len(stripped.ManagedFields) != 0 {
		t.Errorf("copy still has %d managedFields entries", len(stripped.ManagedFields))
	}
	if len(orig.ManagedFields) != 3 {
		t.Errorf("original mutated: managedFields = %d entries, want 3", len(orig.ManagedFields))
	}
	// Interior memory is shared, not copied.
	if &orig.Status.Conditions[0] != &stripped.Status.Conditions[0] {
		t.Error("conditions backing array was copied, want shared")
	}
	if reflect.ValueOf(orig.Labels).Pointer() != reflect.ValueOf(stripped.Labels).Pointer() {
		t.Error("labels map was copied, want shared")
	}
	if orig.Spec.TerminationGracePeriodSeconds != stripped.Spec.TerminationGracePeriodSeconds {
		t.Error("spec pointer field was copied, want shared")
	}
}

func TestCopyWithoutManagedFieldsTypedList(t *testing.T) {
	orig := newCarpList(3)
	out, err := dropfields.CopyWithoutManagedFields(orig)
	if err != nil {
		t.Fatal(err)
	}
	stripped := out.(*testapigroupv1.CarpList)

	for i := range stripped.Items {
		if len(stripped.Items[i].ManagedFields) != 0 {
			t.Errorf("item %d of copy still has managedFields", i)
		}
		if len(orig.Items[i].ManagedFields) != 3 {
			t.Errorf("item %d of original mutated", i)
		}
		if &orig.Items[i].Status.Conditions[0] != &stripped.Items[i].Status.Conditions[0] {
			t.Errorf("item %d interior was copied, want shared", i)
		}
	}
	if &orig.Items[0] == &stripped.Items[0] {
		t.Error("items backing array is shared, want fresh")
	}
}

func TestCopyWithoutManagedFieldsUnstructured(t *testing.T) {
	orig := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "example.com/v1",
		"kind":       "Widget",
		"metadata": map[string]interface{}{
			"name":          "w",
			"managedFields": []interface{}{map[string]interface{}{"manager": "m"}},
		},
		"spec": map[string]interface{}{"replicas": int64(3)},
	}}
	out, err := dropfields.CopyWithoutManagedFields(orig)
	if err != nil {
		t.Fatal(err)
	}
	stripped := out.(*unstructured.Unstructured)

	if _, found := stripped.Object["metadata"].(map[string]interface{})["managedFields"]; found {
		t.Error("copy still has managedFields")
	}
	if _, found := orig.Object["metadata"].(map[string]interface{})["managedFields"]; !found {
		t.Error("original mutated: managedFields removed from shared map")
	}
	origSpec := reflect.ValueOf(orig.Object["spec"]).Pointer()
	strippedSpec := reflect.ValueOf(stripped.Object["spec"]).Pointer()
	if origSpec != strippedSpec {
		t.Error("spec map was copied, want shared")
	}
}

func TestCopyWithoutManagedFieldsPassThrough(t *testing.T) {
	status := &metav1.Status{Status: "Failure", Message: "not found", Code: 404}
	out, err := dropfields.CopyWithoutManagedFields(status)
	if err != nil {
		t.Fatal(err)
	}
	if out != runtime.Object(status) {
		t.Error("object without object metadata should pass through unchanged")
	}

	clean := newCarp("no-mf")
	clean.ManagedFields = nil
	out, err = dropfields.CopyWithoutManagedFields(clean)
	if err != nil {
		t.Fatal(err)
	}
	if out != runtime.Object(clean) {
		t.Error("object without managedFields should pass through without copying")
	}
}

func serializers() map[string]runtime.Serializer {
	return map[string]runtime.Serializer{
		"json":     json.NewSerializerWithOptions(json.DefaultMetaFactory, nil, nil, json.SerializerOptions{}),
		"protobuf": protobuf.NewSerializer(nil, nil),
		"cbor":     cbor.NewSerializer(nil, nil),
	}
}

// TestEncodeByteEquality verifies for every format that the dropfields
// serializer produces byte-identical output to deep-copy-then-strip-then-encode
// with the stock serializer, without mutating the input.
func TestEncodeByteEquality(t *testing.T) {
	for name, delegate := range serializers() {
		t.Run(name, func(t *testing.T) {
			for _, obj := range []runtime.Object{newCarp("a"), newCarpList(10)} {
				var full, dropped, want bytes.Buffer

				if err := delegate.Encode(obj, &full); err != nil {
					t.Fatal(err)
				}
				if err := dropfields.NewSerializer(delegate).Encode(obj, &dropped); err != nil {
					t.Fatal(err)
				}

				// The oracle: deep copy, strip in place, stock encode.
				oracle := obj.DeepCopyObject()
				if list, ok := oracle.(*testapigroupv1.CarpList); ok {
					for i := range list.Items {
						list.Items[i].ManagedFields = nil
					}
				} else {
					oracle.(*testapigroupv1.Carp).ManagedFields = nil
				}
				if err := delegate.Encode(oracle, &want); err != nil {
					t.Fatal(err)
				}

				if !bytes.Equal(dropped.Bytes(), want.Bytes()) {
					t.Errorf("%T: dropfields encoding differs from deepcopy-strip encoding\ngot:  %q\nwant: %q", obj, dropped.String(), want.String())
				}
				if bytes.Equal(dropped.Bytes(), full.Bytes()) {
					t.Errorf("%T: dropfields encoding identical to full encoding; nothing was stripped", obj)
				}
				if bytes.Contains(dropped.Bytes(), []byte("kube-scheduler")) {
					t.Errorf("%T: dropfields encoding still contains a managedFields manager", obj)
				}

				// Input must be untouched.
				var again bytes.Buffer
				if err := delegate.Encode(obj, &again); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(full.Bytes(), again.Bytes()) {
					t.Errorf("%T: input object was mutated by dropfields encoding", obj)
				}
			}
		})
	}
}

type fakeCacheableObject struct {
	obj    runtime.Object
	lastID runtime.Identifier
}

func (f *fakeCacheableObject) CacheEncode(id runtime.Identifier, encode func(runtime.Object, io.Writer) error, w io.Writer) error {
	f.lastID = id
	return encode(f.obj, w)
}
func (f *fakeCacheableObject) GetObject() runtime.Object        { return f.obj.DeepCopyObject() }
func (f *fakeCacheableObject) GetObjectKind() schema.ObjectKind { return f.obj.GetObjectKind() }
func (f *fakeCacheableObject) DeepCopyObject() runtime.Object   { panic("not needed") }

// TestEncodeCacheableObject verifies the decorator routes CacheableObject
// through CacheEncode under its own identifier, so caches like the watch
// cache's cachingObject keep full and stripped bytes as separate entries.
func TestEncodeCacheableObject(t *testing.T) {
	delegate := json.NewSerializerWithOptions(json.DefaultMetaFactory, nil, nil, json.SerializerOptions{})
	dropping := dropfields.NewSerializer(delegate)

	co := &fakeCacheableObject{obj: newCarp("a")}
	var buf bytes.Buffer
	if err := dropping.Encode(co, &buf); err != nil {
		t.Fatal(err)
	}
	if co.lastID != dropping.Identifier() {
		t.Errorf("CacheEncode called with identifier %q, want %q", co.lastID, dropping.Identifier())
	}
	if co.lastID == delegate.Identifier() {
		t.Error("dropfields identifier collides with delegate identifier")
	}
	if bytes.Contains(buf.Bytes(), []byte("managedFields")) {
		t.Error("cached encoding still contains managedFields")
	}
}

// TestConcurrentEncodeSharedObject encodes one shared list from many
// goroutines, mixing full and dropping encoders across formats. Run with
// -race: the spine copy must introduce no writes to shared memory.
func TestConcurrentEncodeSharedObject(t *testing.T) {
	shared := newCarpList(50)
	var wg sync.WaitGroup
	for name, delegate := range serializers() {
		for i := 0; i < 8; i++ {
			wg.Add(2)
			go func(s runtime.Serializer) {
				defer wg.Done()
				if err := s.Encode(shared, io.Discard); err != nil {
					t.Errorf("full encode: %v", err)
				}
			}(delegate)
			go func(s runtime.Serializer) {
				defer wg.Done()
				if err := dropfields.NewSerializer(s).Encode(shared, io.Discard); err != nil {
					t.Errorf("drop encode: %v", err)
				}
			}(delegate)
		}
		_ = name
	}
	wg.Wait()
	for i := range shared.Items {
		if len(shared.Items[i].ManagedFields) != 3 {
			t.Fatalf("shared list mutated at item %d", i)
		}
	}
}

// BenchmarkEncode compares, per format: full encode, dropfields spine-copy
// encode, and the deep-copy-then-strip strawman.
func BenchmarkEncode(b *testing.B) {
	list := newCarpList(1000)
	for _, tc := range []struct {
		name     string
		delegate runtime.Serializer
	}{
		{"json", json.NewSerializerWithOptions(json.DefaultMetaFactory, nil, nil, json.SerializerOptions{StreamingCollectionsEncoding: true})},
		{"protobuf", protobuf.NewSerializerWithOptions(nil, nil, protobuf.SerializerOptions{StreamingCollectionsEncoding: true})},
		{"cbor", cbor.NewSerializer(nil, nil)},
	} {
		dropping := dropfields.NewSerializer(tc.delegate)
		b.Run(tc.name+"/full", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := tc.delegate.Encode(list, io.Discard); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(tc.name+"/drop-spine-copy", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := dropping.Encode(list, io.Discard); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(tc.name+"/drop-deep-copy", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				stripped := list.DeepCopy()
				for j := range stripped.Items {
					stripped.Items[j].ManagedFields = nil
				}
				if err := tc.delegate.Encode(stripped, io.Discard); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
