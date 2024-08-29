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

// package validate holds API validation functions which are designed for use
// with the k8s.io/code-generator/cmd/validation-gen tool.  Each validation
// function has a similar fingerprint:
//
//	func <Name>(opCtx operation.Context,
//	            fldPath *field.Path,
//	            value, oldValue <type>,
//	            <other args...>) field.ErrorList
//
// For a CREATE operation, the oldValue will always be nil.  For an UPDATE
// operation, either value or oldValue may be nil, e.g. when adding or removing
// a value in a list-map.  Validators which care about UPDATE operations should
// look at the opCtx argument to know which operation is being executed.
package validate
