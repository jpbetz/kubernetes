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

// UseSelector accesses the Spec.Selector field of MyResource.
func UseSelector(r *v1.MyResource) map[string]string {
	if r.Spec.Selector != nil {
		return r.Spec.Selector.MatchLabels
	}
	return nil
}

// UseName accesses the Name field of MyResource (through ObjectMeta embedding).
// This should be skipped by the analyzer since ObjectMeta is auto-injected.
func UseName(r *v1.MyResource) string {
	return r.Name
}
