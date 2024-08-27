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

// +k8s:validation-gen=*

// This is a test package.
package primitives_ptr_ok

type T1 struct {
	// +validateTrue={"flags":["UpdateOnly", "PtrOK"], "msg":"T1.US, UpdateOnly, PtrOK"}
	US string `json:"us"`
	// +validateTrue={"flags":["UpdateOnly", "PtrOK"], "msg":"T1.UI, UpdateOnly, PtrOK"}
	UI int `json:"ui"`
	// +validateTrue={"flags":["UpdateOnly", "PtrOK"], "msg":"T1.UB, UpdateOnly, PtrOK"}
	UB bool `json:"ub"`
	// +validateTrue={"flags":["UpdateOnly", "PtrOK"], "msg":"T1.UF, UpdateOnly, PtrOK"}
	UF float64 `json:"uf"`

	// +validateTrue={"flags":[], "msg":"T1.S"}
	S string `json:"s"`
	// +validateTrue={"flags":[], "msg":"T1.I"}
	I int `json:"i"`
	// +validateTrue={"flags":[], "msg":"T1.B"}
	B bool `json:"b"`
	// +validateTrue={"flags":[], "msg":"T1.F"}
	F float64 `json:"f"`
}
