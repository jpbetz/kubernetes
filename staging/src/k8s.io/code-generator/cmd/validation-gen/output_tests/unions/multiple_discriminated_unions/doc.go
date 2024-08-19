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
package multiple_discriminated_unions

// Two discriminated unions in the same struct
type DU struct {
	TypeMeta int

	// +unionDiscriminator={"union": "union1"}
	DU1 D `json:"du1"`

	// +unionMember={"union": "union1"}
	U1M1 *M1 `json:"u1m1"`

	// +unionMember={"union": "union1"}
	U1M2 *M2 `json:"u1m2"`

	// +unionDiscriminator={"union": "union2"}
	DU2 D `json:"du2"`

	// +unionMember={"union": "union2"}
	U2M1 *M1 `json:"u2m1"`

	// +unionMember={"union": "union2"}
	U2M2 *M2 `json:"u2m2"`
}

type D string

const (
	DM1 D = "CustomM1"
	DM2 D = "CustomM2"
)

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
