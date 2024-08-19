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

// This is a test package.
package type_args

import "k8s.io/code-generator/cmd/validation-gen/output_tests/primitives"

// Empty discriminated union
type T1 struct {
	TypeMeta int

	// +validateTrue={"typeArg": "k8s.io/code-generator/cmd/validation-gen/output_tests/primitives.T1", "msg": "T1.S1"}
	S1 primitives.T1 `json:"s1"`

	// +validateTrue={"typeArg": "k8s.io/code-generator/cmd/validation-gen/output_tests/type_args.E1", "msg": "T1.E1"}
	E1 `json:"e1"`

	// +validateTrue={"typeArg": "int", "msg": "T1.I1"}
	I1 int `json:"i1"`
}

// +validateTrue={"msg": "type E1"}
type E1 string
