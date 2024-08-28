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
package structs

import "k8s.io/code-generator/cmd/validation-gen/testscheme"

var localSchemeBuilder = testscheme.New()

type Tother struct {
	// +validateTrue={"flags":[], "msg":"Tother, no flags"}
	OS string `json:"os"`
}

// Treat these as 4 bits, and ensure all combinations
//   bit 0: no flags
//   bit 1: IsFatal

// Note: No validations.
type T00 struct {
	TypeMeta int
	S        string  `json:"s"`
	PS       *string `json:"ps"`
	T        Tother  `json:"t"`
	PT       *Tother `json:"pt"`
}

// +validateTrue={"flags":[], "msg":"T01, no flags"}
type T01 struct {
	TypeMeta int
	// +validateTrue={"flags":[], "msg":"T01.S, no flags"}
	S string `json:"s"`
	// +validateTrue={"flags":[], "msg":"T01.PS, no flags"}
	PS *string `json:"ps"`
	// +validateTrue={"flags":[], "msg":"T01.T, no flags"}
	T Tother `json:"t"`
	// +validateTrue={"flags":[], "msg":"T01.PT, no flags"}
	PT *Tother `json:"pt"`
}

// +validateTrue={"flags":["IsFatal"], "msg":"T02, IsFatal"}
type T02 struct {
	TypeMeta int
	// +validateTrue={"flags":["IsFatal"], "msg":"T02.S, IsFatal"}
	S string `json:"s"`
	// +validateTrue={"flags":["IsFatal"], "msg":"T02.PS, IsFatal"}
	PS *string `json:"ps"`
	// +validateTrue={"flags":["IsFatal"], "msg":"T02.T, IsFatal"}
	T Tother `json:"t"`
	// +validateTrue={"flags":["IsFatal"], "msg":"T02.PT, IsFatal"}
	PT *Tother `json:"pt"`
}

// +validateTrue={"flags":[], "msg":"T03, no flags"}
// +validateTrue={"flags":["IsFatal"], "msg":"T03, IsFatal"}
type T03 struct {
	TypeMeta int
	// +validateTrue={"flags":[], "msg":"T03.S, no flags"}
	// +validateTrue={"flags":["IsFatal"], "msg":"T03.S, IsFatal"}
	S string `json:"s"`
	// +validateTrue={"flags":[], "msg":"T03.PS, no flags"}
	// +validateTrue={"flags":["IsFatal"], "msg":"T03.PS, IsFatal"}
	PS *string `json:"ps"`
	// +validateTrue={"flags":[], "msg":"T03.T, no flags"}
	// +validateTrue={"flags":["IsFatal"], "msg":"T03.T, IsFatal"}
	T Tother `json:"t"`
	// +validateTrue={"flags":[], "msg":"T03.PT, no flags"}
	// +validateTrue={"flags":["IsFatal"], "msg":"T03.PT, IsFatal"}
	PT *Tother `json:"pt"`
}

// Note: these are intentionally in the wrong final order.
// +validateTrue={"flags":[], "msg":"TMultiple, no flags 1"}
// +validateTrue={"flags":["IsFatal"], "msg":"TMultiple, IsFatal 1"}
// +validateTrue="T0, string payload"
// +validateTrue={"flags":[], "msg":"TMultiple, no flags 2"}
// +validateTrue={"flags":["IsFatal"], "msg":"TMultiple, IsFatal 2"}
type TMultiple struct {
	TypeMeta int
	// +validateTrue={"flags":[], "msg":"TMultiple.S, no flags 1"}
	// +validateTrue={"flags":["IsFatal"], "msg":"TMultiple.S, IsFatal 1"}
	// +validateTrue="T0, string payload"
	// +validateTrue={"flags":[], "msg":"TMultiple.S, no flags 2"}
	// +validateTrue={"flags":["IsFatal"], "msg":"TMultiple.S, IsFatal 2"}
	S string `json:"s"`
	// +validateTrue={"flags":[], "msg":"TMultiple.PS, no flags 1"}
	// +validateTrue={"flags":["IsFatal"], "msg":"TMultiple.PS, IsFatal 1"}
	// +validateTrue="T0, string payload"
	// +validateTrue={"flags":[], "msg":"TMultiple.PS, no flags 2"}
	// +validateTrue={"flags":["IsFatal"], "msg":"TMultiple.PS, IsFatal 2"}
	PS *string `json:"ps"`
	// +validateTrue={"flags":[], "msg":"TMultiple.T, no flags 1"}
	// +validateTrue={"flags":["IsFatal"], "msg":"TMultiple.T, IsFatal 1"}
	// +validateTrue="T0, string payload"
	// +validateTrue={"flags":[], "msg":"TMultiple.T, no flags 2"}
	// +validateTrue={"flags":["IsFatal"], "msg":"TMultiple.T, IsFatal 2"}
	T Tother `json:"t"`
	// +validateTrue={"flags":[], "msg":"TMultiple.PT, no flags 1"}
	// +validateTrue={"flags":["IsFatal"], "msg":"TMultiple.PT, IsFatal 1"}
	// +validateTrue="T0, string payload"
	// +validateTrue={"flags":[], "msg":"TMultiple.PT, no flags 2"}
	// +validateTrue={"flags":["IsFatal"], "msg":"TMultiple.PT, IsFatal 2"}
	PT *Tother `json:"pt"`
}
