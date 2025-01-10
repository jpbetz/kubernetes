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

// Package subfield contains test types for testing subfield field validation tags.
package validatefalse

import "k8s.io/code-generator/cmd/validation-gen/testscheme"

var localSchemeBuilder = testscheme.New()

type StructField struct {
	StringField string `json:"stringField"`
}

// T2 has string, slice, pointer and map fields to test subfield field validations across types
type T2 struct {
	MapField     map[string]string `json:"mapField"`
	PointerField *string           `json:"pointerField"`
	SliceField   []string          `json:"sliceField"`
	SliceField2  []StructField     `json:"sliceField2"`
	StringField  string            `json:"stringField"`
	// +k8s:validateFalse="field T2.StringFieldWithValidation"
	StringFieldWithValidation string      `json:"stringFieldWithValidation"`
	StructField               StructField `json:"structField"`
}

// T1 demonstrates validations for subfield fields of structs.
// +k8s:validateFalse="type T1"
type T1 struct {
	TypeMeta int `json:"typeMeta"`

	// T2 struct with subfield field validations
	// +k8s:subfield(mapField)=+k8s:validateFalse="subfield T1.T2.MapField"
	// +k8s:subfield(pointerField)=+k8s:validateFalse="subfield T1.T2.PointerField"
	// +k8s:subfield(sliceField)=+k8s:validateFalse="subfield T1.T2.SliceField"
	// +k8s:subfield(stringField)=+k8s:validateFalse="subfield T1.T2.StringField"
	// +k8s:subfield(stringFieldWithValidation)=+k8s:validateFalse="subfield T1.T2.StringFieldWithValidation"
	// +k8s:subfield(structField)=+k8s:validateFalse="subfield T1.T2.StructField"
	T2 T2 `json:"t2"`

	// +k8s:subfield(mapField)=+k8s:validateFalse="subfield T1.PT2.MapField"
	// +k8s:subfield(pointerField)=+k8s:validateFalse="subfield T1.PT2.PointerField"
	// +k8s:subfield(sliceField)=+k8s:validateFalse="subfield T1.PT2.SliceField"
	// +k8s:subfield(stringField)=+k8s:validateFalse="subfield T1.PT2.StringField"
	// +k8s:subfield(stringFieldWithValidation)=+k8s:validateFalse="subfield T1.PT2.StringFieldWithValidation"
	// +k8s:subfield(structField)=+k8s:validateFalse="subfield T1.PT2.StructField"
	PT2 *T2 `json:"pt2"`
}

type T3 struct {
	TypeMeta int `json:"typeMeta"`

	// +k8s:subfield2(stringField)=+k8s:validateFalse="subfield T3.T2.StringField"
	// +k8s:subfield2(structField)=+k8s:subfield2(stringField)=+k8s:validateFalse="subfield T3.T2.StructField.StringField"
	// +k8s:subfield2(sliceField2)=+k8s:eachVal2=+k8s:subfield2(stringField)=+k8s:validateFalse="subfield T3.T2.SliceField[*].StringField"
	T2 T2 `json:"t2"`
}

// +k8s:subfield2(mapField)=+k8s:validateFalse="subfield T1.T2.MapField"
// +k8s:subfield(pointerField)=+k8s:validateFalse="subfield T1.T2.PointerField"
// +k8s:subfield(stringFieldWithValidation)=+k8s:validateFalse="subfield T1.T2.StringFieldWithValidation"
