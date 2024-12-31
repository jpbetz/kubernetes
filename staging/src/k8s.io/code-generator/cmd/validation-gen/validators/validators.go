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
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/types"
)

// DeclarativeValidator is able to extract validation function generators from
// types.go files.
// legacy
type DeclarativeValidator interface {
	// ExtractValidations returns a Validations for the validation this DeclarativeValidator
	// supports for the given go type, and it's corresponding comment strings.
	ExtractValidations(t *types.Type, comments []string) (Validations, error)

	// Docs returns user-friendly documentation for all of the tags that this
	// validator supports.
	Docs() []TagDoc
}

// TagDescriptor describes a single validation tag and how to use it.
type TagDescriptor interface {
	// Init initializes the tag implementation.  This will be called exactly
	// once.
	Init(c *generator.Context)

	// TagName returns the full tag name (without the "marker" prefix) for this
	// tag.
	TagName() string

	// ValidScopes returns the set of scopes where this tag may be used.
	ValidScopes() sets.Set[TagScope]

	// GetValidations returns any validations described by this tag.
	GetValidations(context TagContext2, args []string, payload string) (Validations, error)

	// Docs returns user-facing documentation for this tag.
	// FIXME: get rid of contexts and call ValidScopes()
	Docs() []TagDoc
}

// TagScope describes where a tag is used.
type TagScope string

// Note: All of these values should be strings which can be used in an error
// message such as "may not be used in %s".
const (
	// TagScopeAll indicates that a tag may be use in any context.  This value
	// should never appear in a TagContext2 struct, since that indicates a
	// specific use.
	TagScopeAll TagScope = "anywhere"

	// TagScopeType indicates a tag used in the comments immediately preceeding
	// a type's definition, which applies to all instances of that type.
	//FIXME: if this were in a tags pkg, maybe "FieldScope"
	TagScopeType TagScope = "type definitions"

	// TagScopeField indicates a tag used in the comments immediately
	// preceeding a struct field's definition, which applies only to that
	// field.
	TagScopeField TagScope = "struct fields"

	// TagScopeListVal indicates a tag which applies to all elements of a list
	// field or type.
	TagScopeListVal TagScope = "list values"

	// TagScopeMapKey indicates a tag which applies to all keys of a map field
	// or type.
	TagScopeMapKey TagScope = "map keys"

	// TagScopeMapVal indicates a tag which applies to all values of a map
	// field or type.
	TagScopeMapVal TagScope = "map values"

	// FIXME: It's not clear if we need to distinguish (e.g.) list values of
	// fields from list values of typedefs.  We could make {type,field} be
	// orthogonal to {scalar, list, list-value, map, map-key, map-value} (and
	// maybe even pointers?), but that seems like extra work that is not needed
	// for now.
)

// TagContext2 describes where a tag was used, so that the scope can be checked
// and so validators can handle different cases if they need.
type TagContext2 struct {
	// Scope is where the tag was used.
	Scope TagScope

	// Type provides details about the type being validated.  When Scope is
	// TagScopeType, this is the underlying type.  When Scope is TagScopeField,
	// this is the field's type (including any pointerness).  When Scope
	// indicates a list-value, map-key, or map-value, this is the type of that
	// key or value.
	Type *types.Type

	// Parent provides details about the logical parent type of the type being
	// validated, when applicable.  When Scope is TagScopeType, this is the
	// newly-defined type (when it exists - gengo handles struct-type definitions
	// differently that other "alias" type definitions).  When Scope is
	// TagScopeField, this is the field's parent struct's type.  When Scope
	// indicates a list-value, map-key, or map-value, this is the type of the
	// whole list or map.
	//
	// Because of how gengo handles struct-type definitions, this field may be
	// nil in those cases.
	Parent *types.Type
}

// Validations defines the function calls and variables to generate to perform validation.
type Validations struct {
	Functions []FunctionGen
	Variables []VariableGen
	Comments  []string
}

func (v *Validations) Empty() bool {
	return v.Len() == 0
}

func (v *Validations) Len() int {
	return len(v.Functions) + len(v.Variables) + len(v.Comments)
}

func (v *Validations) AddFunction(f FunctionGen) {
	v.Functions = append(v.Functions, f)
}

func (v *Validations) AddVariable(variable VariableGen) {
	v.Variables = append(v.Variables, variable)
}

func (v *Validations) AddComment(comment string) {
	v.Comments = append(v.Comments, comment)
}

func (v *Validations) Add(o Validations) {
	v.Functions = append(v.Functions, o.Functions...)
	v.Variables = append(v.Variables, o.Variables...)
	v.Comments = append(v.Comments, o.Comments...)
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

	// ShortCircuit indicates that further validations should be skipped if
	// this validator fails. Most validators are not fatal.
	ShortCircuit FunctionFlags = 1 << iota

	// NonError indicates that a failure of this validator should not be
	// accumulated as an error, but should trigger other aspects of the failure
	// path (e.g. early return when combined with ShortCircuit).
	NonError
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
	//   func(opCtx operation.Context,
	//        fldPath field.Path,
	//        value, oldValue <ValueType>,     // always nilable
	//        extraArgs[0] <extraArgs[0]Type>, // optional
	//        ...,
	//        extraArgs[N] <extraArgs[N]Type>)
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

	// Conditions returns the conditions that must true for a resource to be
	// validated by this function.
	Conditions() Conditions
}

// Conditions defines what conditions must be true for a resource to be validated.
// If any of the conditions are not true, the resource is not validated.
type Conditions struct {
	// OptionEnabled specifies an option name that must be set to true for the condition to be true.
	OptionEnabled string

	// OptionDisabled specifies an option name that must be set to false for the condition to be true.
	OptionDisabled string
}

func (c Conditions) Empty() bool {
	return len(c.OptionEnabled) == 0 && len(c.OptionDisabled) == 0
}

// Identifier is a name that the generator will output as an identifier.
// Identifiers are generated using the RawNamer strategy.
type Identifier types.Name

// PrivateVar is a variable name that the generator will output as a private identifier.
// PrivateVars are generated using the PrivateNamer strategy.
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

func WithCondition(fn FunctionGen, conditions Conditions) FunctionGen {
	name, args := fn.SignatureAndArgs()
	return &functionGen{
		tagName: fn.TagName(), flags: fn.Flags(), function: name, extraArgs: args, typeArgs: fn.TypeArgs(),
		conditions: conditions,
	}
}

type functionGen struct {
	tagName    string
	function   types.Name
	extraArgs  []any
	typeArgs   []types.Name
	flags      FunctionFlags
	conditions Conditions
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

func (v *functionGen) Conditions() Conditions { return v.conditions }

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

// TagDoc describes a comment-tag and its usage.
type TagDoc struct {
	// Tag is the tag name, without the leading '+'.
	Tag string
	// Description is a short description of this tag's purpose.
	Description string
	// Contexts lists the place or places this tag may be used.  Tags used in
	// the wrong context may or may not cause errors.
	Contexts []TagContext
	// Payloads lists zero or more varieties of value for this tag. If this tag
	// never has a payload, this list should be empty, but if the payload is
	// optional, this list should include an entry for "<none>".
	Payloads []TagPayloadDoc
}

// TagContext describes where a tag may be attached.
type TagContext string

const (
	// TagContextType indicates that a tag may be attached to a type
	// definition.
	TagContextType TagContext = "Type definition"
	// TagContextField indicates that a tag may be attached to a struct
	// field, the keys of a map, or the values of a map or slice.
	TagContextField TagContext = "Field definition, map key, map/slice value"
)

// TagPayloadDoc describes a value for a tag (e.g `+tagName=tagValue`).  Some
// tags upport multiple payloads, including <none> (e.g. `+tagName`).
type TagPayloadDoc struct {
	Description string
	Docs        string             `json:",omitempty"`
	Schema      []TagPayloadSchema `json:",omitempty"`
}

// TagPayloadSchema describes a JSON tag payload.
type TagPayloadSchema struct {
	Key     string // required
	Value   string // required
	Docs    string `json:",omitempty"`
	Default string `json:",omitempty"`
}
