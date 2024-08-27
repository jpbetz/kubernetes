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

// This is a test package.
package union

import "k8s.io/code-generator/cmd/validation-gen/testscheme"

var localSchemeBuilder = testscheme.New()

// Non-discriminated union
type U struct {
	TypeMeta int

	// +unionMember
	M1 *M1 `json:"m1"`

	// +unionMember
	M2 *M2 `json:"m2"`
}

// +validateTrue="type M1"
type M1 struct {
	// +validateTrue="field M1.S"
	S string `json:"s"`
}

// +validateTrue="type M2"
type M2 struct {
	// +validateTrue="field M2.S"
	S string `json:"s"`
}
