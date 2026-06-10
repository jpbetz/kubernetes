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

package responsewriters

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	testapigroupv1 "k8s.io/apimachinery/pkg/apis/testapigroup/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/endpoints/handlers/negotiation"
	"k8s.io/apiserver/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
)

var testManagedFields = []metav1.ManagedFieldsEntry{{
	Manager:    "test",
	Operation:  metav1.ManagedFieldsOperationApply,
	APIVersion: "v1",
}}

func testCarp(name string, managedFields []metav1.ManagedFieldsEntry) *testapigroupv1.Carp {
	return &testapigroupv1.Carp{
		ObjectMeta: metav1.ObjectMeta{
			Name:          name,
			Namespace:     "default",
			Labels:        map[string]string{"app": name},
			ManagedFields: managedFields,
		},
		Spec: testapigroupv1.CarpSpec{NodeSelector: map[string]string{"node": name}},
	}
}

func testUnstructured(name string, managedFields bool) *unstructured.Unstructured {
	metadata := map[string]interface{}{"name": name}
	if managedFields {
		metadata["managedFields"] = []interface{}{map[string]interface{}{"manager": "test"}}
	}
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "testapigroup.apimachinery.k8s.io/v1",
		"kind":       "Carp",
		"metadata":   metadata,
		"spec":       map[string]interface{}{"nodeSelector": map[string]interface{}{"node": name}},
	}}
}

func TestWithoutManagedFields(t *testing.T) {
	testCases := []struct {
		name     string
		obj      runtime.Object
		want     runtime.Object
		wantSame bool
	}{
		{
			name: "typed object",
			obj:  testCarp("a", testManagedFields),
			want: testCarp("a", nil),
		},
		{
			name:     "typed object without managedFields",
			obj:      testCarp("a", nil),
			wantSame: true,
		},
		{
			name: "typed list",
			obj: &testapigroupv1.CarpList{
				ListMeta: metav1.ListMeta{ResourceVersion: "10"},
				Items:    []testapigroupv1.Carp{*testCarp("a", testManagedFields), *testCarp("b", nil)},
			},
			want: &testapigroupv1.CarpList{
				ListMeta: metav1.ListMeta{ResourceVersion: "10"},
				Items:    []testapigroupv1.Carp{*testCarp("a", nil), *testCarp("b", nil)},
			},
		},
		{
			name:     "status",
			obj:      &metav1.Status{Message: "ok"},
			wantSame: true,
		},
		{
			name: "unstructured",
			obj:  testUnstructured("a", true),
			want: testUnstructured("a", false),
		},
		{
			name:     "unstructured without managedFields",
			obj:      testUnstructured("a", false),
			wantSame: true,
		},
		{
			name: "unstructured list",
			obj: &unstructured.UnstructuredList{
				Object: map[string]interface{}{"apiVersion": "testapigroup.apimachinery.k8s.io/v1", "kind": "CarpList"},
				Items:  []unstructured.Unstructured{*testUnstructured("a", true), *testUnstructured("b", false)},
			},
			want: &unstructured.UnstructuredList{
				Object: map[string]interface{}{"apiVersion": "testapigroup.apimachinery.k8s.io/v1", "kind": "CarpList"},
				Items:  []unstructured.Unstructured{*testUnstructured("a", false), *testUnstructured("b", false)},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			original := tc.obj.DeepCopyObject()
			got := withoutManagedFields(tc.obj)
			if tc.wantSame {
				if got != tc.obj {
					t.Errorf("expected the original object to be returned unchanged")
				}
			} else {
				if got == tc.obj {
					t.Errorf("expected a copy, got the original object")
				}
				if diff := cmp.Diff(tc.want, got); diff != "" {
					t.Errorf("unexpected result (-want +got):\n%s", diff)
				}
			}
			if diff := cmp.Diff(original, tc.obj); diff != "" {
				t.Errorf("original object was mutated (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWithoutManagedFieldsSharesMemory(t *testing.T) {
	obj := testCarp("a", testManagedFields)
	got, ok := withoutManagedFields(obj).(*testapigroupv1.Carp)
	if !ok {
		t.Fatalf("unexpected type %T", got)
	}
	if reflect.ValueOf(got.Labels).Pointer() != reflect.ValueOf(obj.Labels).Pointer() {
		t.Error("expected labels map to be shared with the original")
	}
	if reflect.ValueOf(got.Spec.NodeSelector).Pointer() != reflect.ValueOf(obj.Spec.NodeSelector).Pointer() {
		t.Error("expected spec to be shared with the original")
	}
}

func TestStripManagedFieldsEncoderIdentifier(t *testing.T) {
	delegate := &fakeEncoder{}
	encoder := &stripManagedFieldsEncoder{delegate: delegate}
	if encoder.Identifier() == delegate.Identifier() {
		t.Error("expected identifier to differ from the delegate's")
	}
}

func TestWriteObjectNegotiatedDropManagedFields(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := testapigroupv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	codecs := serializer.NewCodecFactory(scheme)

	testCases := []struct {
		name              string
		accept            string
		featureEnabled    bool
		object            runtime.Object
		wantManagedFields bool
	}{
		{
			name:              "default keeps managedFields",
			accept:            "application/json",
			featureEnabled:    true,
			object:            testCarp("a", testManagedFields),
			wantManagedFields: true,
		},
		{
			name:           "drop strips managedFields",
			accept:         "application/json;drop=metadata.managedFields",
			featureEnabled: true,
			object:         testCarp("a", testManagedFields),
		},
		{
			name:              "feature disabled keeps managedFields",
			accept:            "application/json;drop=metadata.managedFields",
			object:            testCarp("a", testManagedFields),
			wantManagedFields: true,
		},
		{
			name:           "drop strips managedFields from list items",
			accept:         "application/json;drop=metadata.managedFields",
			featureEnabled: true,
			object: &testapigroupv1.CarpList{
				Items: []testapigroupv1.Carp{*testCarp("a", testManagedFields), *testCarp("b", testManagedFields)},
			},
		},
		{
			name:           "default keeps managedFields in list items",
			accept:         "application/json",
			featureEnabled: true,
			object: &testapigroupv1.CarpList{
				Items: []testapigroupv1.Carp{*testCarp("a", testManagedFields), *testCarp("b", testManagedFields)},
			},
			wantManagedFields: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.ManagedFieldsOptOut, tc.featureEnabled)
			original := tc.object.DeepCopyObject()
			req := httptest.NewRequest(http.MethodGet, "/path", nil)
			req.Header.Set("Accept", tc.accept)
			recorder := httptest.NewRecorder()
			WriteObjectNegotiated(codecs, negotiation.DefaultEndpointRestrictions, testapigroupv1.SchemeGroupVersion, recorder, req, http.StatusOK, tc.object, false)
			result := recorder.Result()
			if result.StatusCode != http.StatusOK {
				t.Fatalf("unexpected status code: %v", result.StatusCode)
			}
			var body map[string]interface{}
			if err := json.NewDecoder(result.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			for i, content := range bodyObjects(t, body) {
				metadata, ok := content["metadata"].(map[string]interface{})
				if !ok {
					t.Fatalf("object %d: missing metadata: %v", i, content)
				}
				if _, got := metadata["managedFields"]; got != tc.wantManagedFields {
					t.Errorf("object %d: managedFields present: got %v, want %v", i, got, tc.wantManagedFields)
				}
			}
			if diff := cmp.Diff(original, tc.object); diff != "" {
				t.Errorf("original object was mutated (-want +got):\n%s", diff)
			}
		})
	}
}

// bodyObjects returns the objects to inspect in a response body: the list
// items if the body is a list, otherwise the body itself.
func bodyObjects(t *testing.T, body map[string]interface{}) []map[string]interface{} {
	items, ok := body["items"]
	if !ok {
		return []map[string]interface{}{body}
	}
	itemList, ok := items.([]interface{})
	if !ok || len(itemList) == 0 {
		t.Fatalf("expected non-empty items in list response: %v", body)
	}
	var out []map[string]interface{}
	for _, item := range itemList {
		content, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected item type %T", item)
		}
		out = append(out, content)
	}
	return out
}
