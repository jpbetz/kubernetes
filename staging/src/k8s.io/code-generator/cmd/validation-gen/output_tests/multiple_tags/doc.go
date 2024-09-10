/*
Copyright 2024 The Kubernetes Authors.

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

// +k8s:validation-gen=TypeMeta
// +k8s:validation-gen-scheme-registry=k8s.io/code-generator/cmd/validation-gen/testscheme.Scheme
// +k8s:validation-gen-test-fixture=validateFalse

// This is a test package.
package multipletags

import "k8s.io/code-generator/cmd/validation-gen/testscheme"

var localSchemeBuilder = testscheme.New()

// +validateFalse="type T1 #1"
// +validateFalse="type T1 #2"
// +validateFalse="type T1 #3"
type T1 struct {
	TypeMeta int
	// +validateFalse="field T1.S #1"
	// +validateFalse="field T1.S #2"
	// +validateFalse="field T1.S #3"
	S string `json:"s"`
	// +validateFalse="field T1.T2 #1"
	// +validateFalse="field T1.T2 #2"
	// +validateFalse="field T1.T2 #3"
	T2 T2 `json:"t2"`
}

// +validateFalse="type T2 #1"
// +validateFalse="type T2 #2"
type T2 struct {
	// +validateFalse="field T2.S #1"
	// +validateFalse="field T2.S #2"
	S string `json:"s"`
}
