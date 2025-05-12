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

package openapi

import (
	"testing"

	"github.com/go-openapi/jsonreference"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestParseRef(t *testing.T) {
	testCases := []struct {
		ref          jsonreference.Ref
		groupVersion schema.GroupVersion
		typeName     string
		wantErr      error
	}{
		{
			ref: jsonreference.MustCreateRef("/openapi/v3/api/v1#/components/schemas/io.k8s.api.core.v1.PodSpec"),
			groupVersion: schema.GroupVersion{
				Group:   "",
				Version: "v1",
			},
			typeName: "PodSpec",
		},
		{
			ref: jsonreference.MustCreateRef("/openapi/v3/apis/apps/v1#/components/schemas/io.k8s.api.apps.v1.DaemonSet"),
			groupVersion: schema.GroupVersion{
				Group:   "apps",
				Version: "v1",
			},
			typeName: "DaemonSet",
		},
		{
			ref: jsonreference.MustCreateRef("/openapi/v3/apis/certificates.k8s.io/v1#/components/schemas/io.k8s.api.certificates.v1.CertificateSigningRequest"),
			groupVersion: schema.GroupVersion{
				Group:   "certificates.k8s.io",
				Version: "v1",
			},
			typeName: "CertificateSigningRequest",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.ref.String(), func(t *testing.T) {
			groupVersion, typeName, err := ParseRef(tc.ref)

			if tc.wantErr != nil {
				if err == nil {
					t.Errorf("expected error %v, but got nil", tc.wantErr)
				} else if err.Error() != tc.wantErr.Error() {
					t.Errorf("expected error %v, but got %v", tc.wantErr, err)
				}
				return
			}
			if groupVersion != tc.groupVersion {
				t.Errorf("expected groupVersion %v, but got %v", tc.groupVersion, groupVersion)
			}
			if typeName != tc.typeName {
				t.Errorf("expected typeName %v, but got %v", tc.typeName, typeName)
			}
		})
	}
}
