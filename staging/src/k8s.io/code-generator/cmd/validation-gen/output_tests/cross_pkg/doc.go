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
package crosspkg

import (
	"k8s.io/code-generator/cmd/validation-gen/output_tests/_codegenignore/other"
	"k8s.io/code-generator/cmd/validation-gen/output_tests/primitives"
	"k8s.io/code-generator/cmd/validation-gen/output_tests/typedefs"
)

type T1 struct {
	TypeMeta int

	// +validateTrue="field T1.PrimitivesT1"
	PrimitivesT1 primitives.T1 `json:"primitivest1"`
	// +validateTrue="field T1.PrimitivesT2"
	PrimitivesT2 primitives.T2 `json:"primitivest2"`
	// +validateTrue="field T1.PrimitivesT3"
	PrimitivesT3 primitives.T3 `json:"primitivest3"`
	// +validateTrue="field T1.PrimitivesT4"
	PrimitivesT4 primitives.T4 `json:"primitivest4"`

	// +validateTrue="field T1.TypedefsE1"
	TypedefsE1 typedefs.E1 `json:"typedefse1"`
	// +validateTrue="field T1.TypedefsE2"
	TypedefsE2 typedefs.E2 `json:"typedefse2"`
	// +validateTrue="field T1.TypedefsE3"
	TypedefsE3 typedefs.E3 `json:"typedefse3"`
	// +validateTrue="field T1.TypedefsE4"
	TypedefsE4 typedefs.E4 `json:"typedefse4"`

	// +validateTrue="field T1.OtherString"
	OtherString other.StringType `json:"otherString"`
	// +validateTrue="field T1.OtherInt"
	OtherInt other.IntType `json:"otherInt"`
	// +validateTrue="field T1.OtherStruct"
	OtherStruct other.StructType `json:"otherStruct"`
}
