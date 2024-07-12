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

package validators

import "k8s.io/gengo/v2/types"

// DeclarativeValidator provides validation-gen with the information needed to generate a
// validation function invocation.
type DeclarativeValidator interface {
	// ValidatorSignature returns the function name and value literals, in string form, to be passed as extraArg.
	//
	// The function signature must be of the form:
	//   func(field.Path, <valueType>, extraArgs[0] <extraArgs[0]Type>, ..., extraArgs[N] <extraArgs[N]Type>)
	//
	// extraArgs may contain strings, ints, floats and bools.
	//
	// If validation function to be called does not have a signature of this form, please introduce
	// a function that does and use that function to call the validation function.
	ValidatorSignature() (function types.Name, extraArgs []any)
}

// NewValidator creates a validator for a given function name and extraArgs.
func NewValidator(function types.Name, extraArgs ...any) DeclarativeValidator {
	return &basicValidator{function: function, extraArgs: extraArgs}
}

type basicValidator struct {
	function  types.Name
	extraArgs []any
}

func (v *basicValidator) ValidatorSignature() (function types.Name, args []any) {
	return v.function, v.extraArgs
}
