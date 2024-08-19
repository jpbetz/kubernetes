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

import (
	"k8s.io/gengo/v2/types"
)

// DeclarativeValidator is able to extract validation function generators from
// types.go files.
type DeclarativeValidator interface {
	// ExtractValidations returns a Validations for the validation this DeclarativeValidator
	// supports for the given go type, and it's corresponding comment strings.
	ExtractValidations(t *types.Type, comments []string) (Validations, error)
}

// Validations defines the function calls and variables to generate to perform validation.
type Validations struct {
	Functions []FunctionGen
	Variables []VariableGen
}

func (v *Validations) Empty() bool {
	return len(v.Functions) == 0 && len(v.Variables) == 0
}

func (v *Validations) Len() int {
	return len(v.Functions) + len(v.Variables)
}

func (v *Validations) AddFunction(f FunctionGen) {
	v.Functions = append(v.Functions, f)
}

func (v *Validations) AddVariable(variable VariableGen) {
	v.Variables = append(v.Variables, variable)
}

func (v *Validations) Add(o Validations) {
	v.Functions = append(v.Functions, o.Functions...)
	v.Variables = append(v.Variables, o.Variables...)
}

// FunctionFlags define optional properties of a validator.  Most validators
// can just use DefaultFlags.
type FunctionFlags uint32

// IsSet returns true if all of the wanted flags are set.
func (ff FunctionFlags) IsSet(wanted FunctionFlags) bool {
	return (ff & wanted) == wanted
}

const (
	// DefaultFlags is defined for clarity.
	DefaultFlags FunctionFlags = 0

	// IsFatal indicates that further validations should be skipped if this
	// validator fails. Most validators are not fatal.
	IsFatal FunctionFlags = 1 << iota

	// PtrOK indicates that when validating a pointer field, this validator
	// wants the pointer value, rather than the dereferenced value.  Most
	// validators want the value, not the pointer.
	PtrOK
)

// FunctionGen provides validation-gen with the information needed to generate a
// validation function invocation.
type FunctionGen interface {
	// TagName returns the tag which triggers this validator.
	TagName() string

	// SignatureAndArgs returns the function name and all extraArg value literals that are passed when the function
	// invocation is generated.
	//
	// The function signature must be of the form:
	//   func(field.Path, <valueType>, extraArgs[0] <extraArgs[0]Type>, ..., extraArgs[N] <extraArgs[N]Type>)
	//
	// extraArgs may contain:
	// - data literals comprised of maps, slices, strings, ints, floats and bools
	// - references, represented by types.Type (to reference any type in the universe), and types.Member (to reference members of the current value)
	//
	// If validation function to be called does not have a signature of this form, please introduce
	// a function that does and use that function to call the validation function.
	SignatureAndArgs() (function types.Name, extraArgs []any)

	// TypeArgs assigns types to the type parameters of the function, for invocation.
	TypeArgs() []types.Name

	// Flags returns the options for this validator function.
	Flags() FunctionFlags
}

// PrivateVar is a variable name that the generator will output as a private identifier.
type PrivateVar types.Name

// VariableGen provides validation-gen with the information needed to generate variable.
// Variables typically support generated functions by providing static information such
// as the list of supported symbols for an enum.
type VariableGen interface {
	// TagName returns the tag which triggers this validator.
	TagName() string

	// Var returns the variable identifier.
	Var() PrivateVar

	// Init generates the function call that the variable is assigned to.
	Init() FunctionGen
}

// Function creates a FunctionGen for a given function name and extraArgs.
func Function(tagName string, flags FunctionFlags, function types.Name, extraArgs ...any) FunctionGen {
	return GenericFunction(tagName, flags, function, nil, extraArgs...)
}

func GenericFunction(tagName string, flags FunctionFlags, function types.Name, typeArgs []types.Name, extraArgs ...any) FunctionGen {
	// Callers of Signature don't care if the args are all of a known type, it just
	// makes it easier to declare validators.
	var anyArgs []any
	if len(extraArgs) > 0 {
		anyArgs = make([]any, len(extraArgs))
		for i, arg := range extraArgs {
			anyArgs[i] = arg
		}
	}
	return &functionGen{tagName: tagName, flags: flags, function: function, extraArgs: anyArgs, typeArgs: typeArgs}
}

type functionGen struct {
	tagName   string
	function  types.Name
	extraArgs []any
	typeArgs  []types.Name
	flags     FunctionFlags
}

func (v *functionGen) TagName() string {
	return v.tagName
}

func (v *functionGen) SignatureAndArgs() (function types.Name, args []any) {
	return v.function, v.extraArgs
}

func (v *functionGen) TypeArgs() []types.Name { return v.typeArgs }

func (v *functionGen) Flags() FunctionFlags {
	return v.flags
}

// Variable creates a VariableGen for a given function name and extraArgs.
func Variable(variable PrivateVar, init FunctionGen) VariableGen {
	return &variableGen{
		variable: variable,
		init:     init,
	}
}

type variableGen struct {
	variable PrivateVar
	init     FunctionGen
}

func (v variableGen) TagName() string {
	return v.init.TagName()
}

func (v variableGen) Var() PrivateVar {
	return v.variable
}

func (v variableGen) Init() FunctionGen {
	return v.init
}
