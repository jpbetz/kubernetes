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

package openapi

import (
	"bytes"
	"encoding/json"
	"testing"

	"k8s.io/kube-openapi/pkg/builder3"
	"k8s.io/kube-openapi/pkg/common"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

// TestPartialV3SpecMarshalDeterminism builds a partial v3 spec over a handful
// of core types and asserts that MarshalJSON produces byte-identical output on
// repeated calls. This confirms spec.Schema.MarshalJSON's internal.
// DeterministicMarshal path (stable map ordering), which A2 relies on for a
// stable ETag/hash across weak-miss rebuilds.
func TestPartialV3SpecMarshalDeterminism(t *testing.T) {
	names := []string{
		"io.k8s.api.core.v1.Pod",
		"io.k8s.api.core.v1.Service",
		"io.k8s.api.core.v1.Node",
		"io.k8s.api.core.v1.ConfigMap",
		"io.k8s.api.core.v1.Namespace",
	}

	config := &common.OpenAPIV3Config{
		Info:           &spec.Info{InfoProps: spec.InfoProps{Title: "test", Version: "v0"}},
		GetDefinitions: GetOpenAPIDefinitions,
	}

	schemas, err := builder3.BuildOpenAPIDefinitionsForResources(config, names...)
	if err != nil {
		t.Fatalf("BuildOpenAPIDefinitionsForResources: %v", err)
	}
	if len(schemas) == 0 {
		t.Fatalf("expected non-empty schema map")
	}
	t.Logf("built partial v3 spec with %d schemas (transitive closure of %d roots)", len(schemas), len(names))

	first, err := json.Marshal(schemas)
	if err != nil {
		t.Fatalf("marshal #1: %v", err)
	}
	for i := 0; i < 100; i++ {
		next, err := json.Marshal(schemas)
		if err != nil {
			t.Fatalf("marshal #%d: %v", i+2, err)
		}
		if !bytes.Equal(first, next) {
			t.Fatalf("non-deterministic marshal on iteration %d (%d vs %d bytes)", i+2, len(first), len(next))
		}
	}
	t.Logf("marshal is byte-identical across 101 iterations (%d bytes)", len(first))
}
