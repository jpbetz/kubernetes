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
	"testing"

	"k8s.io/kube-openapi/pkg/validation/spec"
)

// BenchmarkGetOpenAPIDefinitions measures the cost of building the full
// generated-definitions map. This is the work recomputed on an A2 weak-miss
// when config.Definitions is nil.
func BenchmarkGetOpenAPIDefinitions(b *testing.B) {
	ref := func(path string) spec.Ref { return spec.Ref{} }
	b.ReportAllocs()
	b.ResetTimer()
	var n int
	for i := 0; i < b.N; i++ {
		defs := GetOpenAPIDefinitions(ref)
		n = len(defs)
	}
	b.StopTimer()
	b.ReportMetric(float64(n), "defs")
}
