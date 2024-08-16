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
package unions

// Empty discriminated union
type DU1 struct {
	TypeMeta int

	// +unionDiscriminator
	D1 D1 `json:"d1"`
}

type D1 string

// Discriminated union
type DU2 struct {
	TypeMeta int

	// +unionDiscriminator
	D2 D2 `json:"d2"`

	// +unionMember
	M1 *M1 `json:"m1"`

	// +unionMember
	M2 *M2 `json:"m2"`

	T1 *T1 `json:"t1"` // not part of the union
}

type D2 string

const (
	D2M1 D2 = "m1"
	D2M2 D2 = "m2"
)

// Discriminated union with custom member names
type DU3 struct {
	TypeMeta int

	// +unionDiscriminator
	D3 D3 `json:"d3"`

	// +unionMember={"memberName": "CustomM1"}
	M1 *M1 `json:"m1"`

	// +unionMember={"memberName": "CustomM2"}
	M2 *M2 `json:"m2"`

	T1 *T1 `json:"t1"` // not part of the union
}

type D3 string

const (
	D3M1 D3 = "CustomM1"
	D3M2 D3 = "CustomM2"
)

// Two discriminated unions in the same struct
type DU4 struct {
	TypeMeta int

	// +unionDiscriminator={"union": "union1"}
	D3U1 D3 `json:"d3u1"`

	// +unionMember={"union": "union1"}
	U1M1 *M1 `json:"u1m1"`

	// +unionMember={"union": "union1"}
	U1M2 *M2 `json:"u1m2"`

	// +unionDiscriminator={"union": "union2"}
	D3U2 D3 `json:"d3u2"`

	// +unionMember={"union": "union2"}
	U2M1 *M1 `json:"u2m1"`

	// +unionMember={"union": "union2"}
	U2M2 *M2 `json:"u2m2"`

	T1 *T1 `json:"t1"` // not part of the union
}

// Non-discriminated union
type U1 struct {
	TypeMeta int

	// +unionMember
	M1 *M1 `json:"m1"`

	// +unionMember
	M2 *M2 `json:"m2"`

	T1 *T1 `json:"t1"` // not part of the union
}

// Non-discriminated union with custom member names
type U2 struct {
	TypeMeta int

	// +unionMember={"memberName": "CustomM1"}
	M1 *M1 `json:"m1"`

	// +unionMember={"memberName": "CustomM2"}
	M2 *M2 `json:"m2"`

	T1 *T1 `json:"t1"` // not part of the union
}

// Two non-discriminated unions in the same struct
type U3 struct {
	TypeMeta int

	// +unionMember={"union": "union1"}
	U1M1 *M1 `json:"u1m1"`

	// +unionMember={"union": "union1"}
	U1M2 *M2 `json:"u1m2"`

	// +unionMember={"union": "union2"}
	U2M1 *M1 `json:"u2m1"`

	// +unionMember={"union": "union2"}
	U2M2 *M2 `json:"u2m2"`

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
