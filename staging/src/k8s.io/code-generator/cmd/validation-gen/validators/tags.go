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

// TagValidator describes a single validation tag and how to use it.
type TagValidator interface {
	// Init initializes the tag implementation.  This will be called exactly
	// once.
	Init(c *generator.Context)

	// TagName returns the full tag name (without the "marker" prefix) for this
	// tag.
	TagName() string

	// ValidScopes returns the set of scopes where this tag may be used.
	ValidScopes() sets.Set[TagScope]

	// GetValidations returns any validations described by this tag.
	GetValidations(context TagContext, args []string, payload string) (Validations, error)

	// Docs returns user-facing documentation for this tag.
	Docs() TagDoc
}

type TypeValidator interface {
	// Init initializes the implementation.  This will be called exactly once.
	Init(c *generator.Context)

	// Name returns a unique name for this validator.  This is used for sorting
	// and logging.
	Name() string

	// GetValidations returns any validations imposed by this validator for the
	// given types.
	//
	// The way gengo handles type definitions varies between structs and other
	// types.  For struct definitions (e.g. `type Foo struct {}`), the realType
	// is the struct itself (the Kind field will be `types.Struct`) and the
	// parentType will be nil.  For other types (e.g. `type Bar string`), the
	// realType will be the underlying type and the parentType will be the
	// newly defined type (the Kind field will be `types.Alias`).
	GetValidations(realType, parentType *types.Type) (Validations, error)
}

// TagScope describes where a tag is used.
type TagScope string

// Note: All of these values should be strings which can be used in an error
// message such as "may not be used in %s".
const (
	// TagScopeAll indicates that a tag may be use in any context.  This value
	// should never appear in a TagContext struct, since that indicates a
	// specific use.
	TagScopeAll TagScope = "anywhere"

	// TagScopeType indicates a tag used in the comments immediately preceeding
	// a type's definition, which applies to all instances of that type.
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

	// TODO: It's not clear if we need to distinguish (e.g.) list values of
	// fields from list values of typedefs.  We could make {type,field} be
	// orthogonal to {scalar, list, list-value, map, map-key, map-value} (and
	// maybe even pointers?), but that seems like extra work that is not needed
	// for now.
)

// TagContext describes where a tag was used, so that the scope can be checked
// and so validators can handle different cases if they need.
type TagContext struct {
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

	// Member provides details about a field within a struct, when Scope is
	// TagScopeField.  For all other values of Scope, this will be nil.
	Member *types.Member
}

// TagDoc describes a comment-tag and its usage.
type TagDoc struct {
	// Tag is the tag name, without the leading '+'.
	Tag string
	// Description is a short description of this tag's purpose.
	Description string
	// Contexts lists the place or places this tag may be used.  Tags used in
	// the wrong context may or may not cause errors.
	Contexts []TagScope
	// Payloads lists zero or more varieties of value for this tag. If this tag
	// never has a payload, this list should be empty, but if the payload is
	// optional, this list should include an entry for "<none>".
	Payloads []TagPayloadDoc
}

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
