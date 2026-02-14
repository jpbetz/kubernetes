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

package handlers

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	"net/http"
	"net/url"

	"k8s.io/apiserver/pkg/endpoints/request"
)

func podWithManagedFields() *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "pod1",
			Namespace:       "default",
			ResourceVersion: "100",
			ManagedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:    "kubectl",
					Operation:  metav1.ManagedFieldsOperationApply,
					APIVersion: "v1",
					FieldsType: "FieldsV1",
					FieldsV1:   &metav1.FieldsV1{Raw: []byte(`{"f:metadata":{}}`)},
				},
			},
		},
	}
}

func TestClearManagedFieldsHelper(t *testing.T) {
	pod := podWithManagedFields()
	if len(pod.GetManagedFields()) == 0 {
		t.Fatal("expected managedFields to be set")
	}
	clearManagedFields(pod)
	if len(pod.GetManagedFields()) != 0 {
		t.Fatal("expected managedFields to be nil after clear")
	}
}

func TestClearManagedFieldsFromListHelper(t *testing.T) {
	podList := &v1.PodList{
		Items: []v1.Pod{
			*podWithManagedFields(),
			*podWithManagedFields(),
		},
	}
	clearManagedFieldsFromList(podList)
	for i := range podList.Items {
		if len(podList.Items[i].GetManagedFields()) != 0 {
			t.Fatalf("expected managedFields to be nil on pod %d", i)
		}
	}
}

func TestExcludeManagedFieldsQueryParam(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(featureStateName(enabled), func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.ExcludeManagedFields, enabled)

			req := &http.Request{
				URL: &url.URL{
					RawQuery: "excludeManagedFields=true",
				},
			}
			req = req.WithContext(request.NewContext())

			// Simulate the parsing logic from getResourceHandler
			ctx := req.Context()
			if utilfeature.DefaultFeatureGate.Enabled(features.ExcludeManagedFields) {
				if values := req.URL.Query(); values.Get("excludeManagedFields") == "true" {
					ctx = request.WithExcludeManagedFields(ctx)
					req = req.WithContext(ctx)
				}
			}

			if enabled {
				if !request.ExcludeManagedFieldsFrom(req.Context()) {
					t.Fatal("expected excludeManagedFields to be set on context when feature gate is enabled")
				}
			} else {
				if request.ExcludeManagedFieldsFrom(req.Context()) {
					t.Fatal("expected excludeManagedFields to NOT be set on context when feature gate is disabled")
				}
			}
		})
	}
}

func TestExcludeManagedFieldsFeatureGateDisabled(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.ExcludeManagedFields, false)

	req := &http.Request{
		URL: &url.URL{
			RawQuery: "excludeManagedFields=true",
		},
	}
	req = req.WithContext(request.NewContext())

	ctx := req.Context()
	if utilfeature.DefaultFeatureGate.Enabled(features.ExcludeManagedFields) {
		if values := req.URL.Query(); values.Get("excludeManagedFields") == "true" {
			ctx = request.WithExcludeManagedFields(ctx)
			req = req.WithContext(ctx)
		}
	}

	if request.ExcludeManagedFieldsFrom(req.Context()) {
		t.Fatal("expected excludeManagedFields to be ignored when feature gate is disabled")
	}
}

func featureStateName(enabled bool) string {
	if enabled {
		return "featureEnabled"
	}
	return "featureDisabled"
}
