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
package ratcheting

import "k8s.io/code-generator/cmd/validation-gen/testscheme"

var localSchemeBuilder = testscheme.New()

// +k8s:ratcheting=1
// +k8s:validateFalse="type Struct1"
type Struct1 struct {
	TypeMeta int

	// +k8s:listType=map
	// +k8s:listMapKey=key1Field
	// +k8s:eachVal=+k8s:immutable
	// +k8s:ratcheting=1
	ListField []OtherStruct1 `json:"listField"`

	// +k8s:minimum=1
	// +k8s:ratcheting=1
	MinField int `json:"minField"`
}

// +k8s:validateFalse="type OtherStruct"
// +k8s:ratcheting=1
type OtherStruct1 struct {
	// +k8s:ratcheting=1
	Key1Field string `json:"key1Field"`
	// +k8s:ratcheting=1
	DataField string `json:"dataField"`
}

// +k8s:ratcheting=2
// +k8s:validateFalse="type Struct2"
type Struct2 struct {
	TypeMeta int

	// +k8s:listType=map
	// +k8s:listMapKey=key1Field
	// +k8s:eachVal=+k8s:immutable
	// +k8s:ratcheting=2
	ListField []OtherStruct1 `json:"listField"`

	// +k8s:minimum=1
	// +k8s:ratcheting=2
	MinField int `json:"minField"`
}

// +k8s:validateFalse="type OtherStruct"
// +k8s:ratcheting=2
type OtherStruct2 struct {
	// +k8s:ratcheting=2
	Key1Field string `json:"key1Field"`
	// +k8s:ratcheting=2
	DataField string `json:"dataField"`
}

type Element1 struct {
	// +k8s:ratcheting=1
	// +k8s:validateFalse="field TypeMeta"
	TypeMeta int

	// +k8s:ratcheting=1
	// +k8s:optional
	// +k8s:validateFalse="field Element1.F1"
	F1 *Element1 `json:"f1"`

	// +k8s:ratcheting=1
	// +k8s:optional
	// +k8s:validateFalse="field Element1.F2"
	F2 *Element1 `json:"f2"`

	// +k8s:ratcheting=1
	// +k8s:optional
	// +k8s:validateFalse="field Element1.F3"
	F3 *Element1 `json:"f3"`

	// +k8s:ratcheting=1
	// +k8s:optional
	// +k8s:validateFalse="field Element1.F4"
	F4 *Element1 `json:"f4"`

	// +k8s:ratcheting=1
	// +k8s:optional
	// +k8s:validateFalse="field Element1.F5"
	F5 *Element1 `json:"f5"`
}

type Element2 struct {
	// +k8s:ratcheting=2
	// +k8s:validateFalse="field TypeMeta"
	TypeMeta int

	// +k8s:ratcheting=2
	// +k8s:optional
	// +k8s:validateFalse="field Element2.F1"
	F1 *Element2 `json:"f1"`

	// +k8s:ratcheting=2
	// +k8s:optional
	// +k8s:validateFalse="field Element2.F2"
	F2 *Element2 `json:"f2"`

	// +k8s:ratcheting=2
	// +k8s:optional
	// +k8s:validateFalse="field Element2.F3"
	F3 *Element2 `json:"f3"`

	// +k8s:ratcheting=2
	// +k8s:optional
	// +k8s:validateFalse="field Element2.F4"
	F4 *Element2 `json:"f4"`

	// +k8s:ratcheting=2
	// +k8s:optional
	// +k8s:validateFalse="field Element2.F5"
	F5 *Element2 `json:"f5"`
}
