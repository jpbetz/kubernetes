/*
Copyright 2023 The Kubernetes Authors.

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

package resolver

import (
	"testing"

	"github.com/go-openapi/jsonreference"

	"k8s.io/kube-openapi/pkg/common"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

func testDefs(_ common.ReferenceCallback) map[string]common.OpenAPIDefinition {
	return map[string]common.OpenAPIDefinition{
		"k8s.io/api/core/v1.PodSpec": {
			Schema: spec.Schema{
				SchemaProps: spec.SchemaProps{
					Description: "PodSpec",
					Type:        []string{"object"},
				},
			},
		},
		"k8s.io/api/core/v1.PodTemplate": {
			Schema: spec.Schema{
				SchemaProps: spec.SchemaProps{
					Description: "PodTemplate",
					Type:        []string{"object"},
					Properties: map[string]spec.Schema{
						"spec": {
							SchemaProps: spec.SchemaProps{
								Ref: spec.Ref{Ref: jsonreference.MustCreateRef("k8s.io/api/core/v1.PodSpec")},
							},
						},
					},
				},
			},
		},
	}
}

func TestResolveRef(t *testing.T) {
	testCases := []struct {
		name   string
		ref    jsonreference.Ref
		expect func(out *spec.Schema) bool
	}{
		{
			name: "resolve type",
			ref:  jsonreference.MustCreateRef("/openapi/v3/api/v1#/components/schemas/io.k8s.api.core.v1.PodSpec"),
			expect: func(out *spec.Schema) bool {
				return out.Description == "PodSpec"
			},
		},
		{
			name: "resolve type and nested type",
			ref:  jsonreference.MustCreateRef("/openapi/v3/api/v1#/components/schemas/io.k8s.api.core.v1.PodTemplate"),
			expect: func(out *spec.Schema) bool {
				return out.Description == "PodTemplate" && out.Properties["spec"].Description == "PodSpec"
			},
		},
	}

	resolver := NewDefinitionsSchemaResolver(testDefs)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := resolver.resolveRef(tc.ref)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.expect(actual) {
				t.Fatalf("unexpected result: %v", actual)
			}
		})
	}
}

func TestResolveRefs(t *testing.T) {
	testCases := []struct {
		name   string
		schema *spec.Schema
		expect func(out *spec.Schema) bool
	}{
		{
			name: "root",
			schema: &spec.Schema{
				SchemaProps: spec.SchemaProps{
					Ref: spec.Ref{Ref: jsonreference.MustCreateRef("/openapi/v3/api/v1#/components/schemas/io.k8s.api.core.v1.PodTemplate")},
				},
			},
			expect: func(out *spec.Schema) bool {
				return out.Description == "PodTemplate" && out.Properties["spec"].Description == "PodSpec"
			},
		},
		{
			name: "property",
			schema: &spec.Schema{
				SchemaProps: spec.SchemaProps{
					Description: "Property",
					Type:        []string{"object"},
					Properties: map[string]spec.Schema{
						"template": {
							SchemaProps: spec.SchemaProps{
								Ref: spec.Ref{Ref: jsonreference.MustCreateRef("/openapi/v3/api/v1#/components/schemas/io.k8s.api.core.v1.PodTemplate")},
							},
						},
					},
				},
			},
			expect: func(out *spec.Schema) bool {
				return out.Description == "Property" &&
					out.Properties["template"].Description == "PodTemplate" &&
					out.Properties["template"].Properties["spec"].Description == "PodSpec"
			},
		},
	}

	resolver := NewDefinitionsSchemaResolver(testDefs)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := resolver.ResolveRefs(tc.schema)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.expect(actual) {
				t.Fatalf("unexpected result: %v", actual)
			}
		})
	}
}
