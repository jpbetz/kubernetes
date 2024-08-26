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
package typedefs

// Treat these as 4 bits, and ensure all combinations
//   bit 0: no flags
//   bit 1: Fatal

// Note: No validations.
type E00 string

// +validateTrue={"flags":[], "msg":"E01, no flags"}
type E01 string

// +validateTrue={"flags":["Fatal"], "msg":"E02, Fatal"}
type E02 string

// +validateTrue={"flags":[], "msg":"E03, no flags"}
// +validateTrue={"flags":["Fatal"], "msg":"E03, Fatal"}
type E03 string

// Note: these are intentionally in the wrong final order.
// +validateTrue={"flags":[], "msg":"EMultiple, no flags 1"}
// +validateTrue={"flags":["Fatal"], "msg":"EMultiple, Fatal 1"}
// +validateTrue="E0, string payload"
// +validateTrue={"flags":[], "msg":"EMultiple, no flags 2"}
// +validateTrue={"flags":["Fatal"], "msg":"EMultiple, Fatal 2"}
type EMultiple string
