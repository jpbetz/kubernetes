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

// Package testconsumer is a test package that accesses fields on test API types.
// It is used by the analysis tests to verify field usage detection.
package testconsumer

import (
	v1 "k8s.io/code-generator/cmd/transform-gen/analysis/testapi/v1"
)

// DirectAccess accesses fields via a direct chain.
func DirectAccess(r *v1.MyResource) *v1.LabelSelector {
	return r.Spec.Selector
}

// IntermediateVariable accesses fields through an intermediate variable.
func IntermediateVariable(r *v1.MyResource) *int32 {
	spec := r.Spec
	return spec.Replicas
}

// RangeVariable accesses fields on a range variable.
func RangeVariable(r *v1.MyResource) []string {
	var result []string
	for _, item := range r.Spec.Items {
		result = append(result, item.Value)
	}
	return result
}

// MultiHop accesses fields through multiple intermediate variables.
func MultiHop(r *v1.MyResource) map[string]string {
	spec := r.Spec
	sel := spec.Selector
	return sel.MatchLabels
}

// ObjectMetaAccess accesses fields through embedded ObjectMeta.
// These should be skipped since ObjectMeta is auto-injected.
func ObjectMetaAccess(r *v1.MyResource) string {
	return r.Name
}
