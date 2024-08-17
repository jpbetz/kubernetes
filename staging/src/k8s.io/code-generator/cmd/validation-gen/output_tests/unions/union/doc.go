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
package union

// Non-discriminated union
type U struct {
	TypeMeta int

	// +unionMember
	M1 *M1 `json:"m1"`

	// +unionMember
	M2 *M2 `json:"m2"`

	T1 *T1 `json:"t1"` // not part of the union
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

// +validateTrue="type T1"
type T1 struct {
	TypeMeta int

	// +validateTrue="field T1.LS"
	// +eachVal=+validateTrue="field T1.LS[*]"
	// +eachVal=+required
	LS []string `json:"ls"`
}
