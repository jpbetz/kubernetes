/*
Copyright 2021 The Kubernetes Authors.

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
	"encoding/json"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/parser/tags"
	"k8s.io/gengo/v2/types"
)

var discriminatedUnionValidator = types.Name{Package: libValidationPkg, Name: "DiscriminatedUnion"}
var unionValidator = types.Name{Package: libValidationPkg, Name: "Union"}

var newDiscriminatedUnionMembership = types.Name{Package: libValidationPkg, Name: "NewDiscriminatedUnionMembership"}
var newUnionMembership = types.Name{Package: libValidationPkg, Name: "NewUnionMembership"}

func init() {
	// Unions are comprised of multiple tags, which need to share information
	// between them.  The tags are on struct fields, but the validation
	// actually pertains to the struct itself.
	shared := map[*types.Type]unions{}
	RegisterTypeValidator(unionTypeValidator{shared})
	RegisterTagValidator(unionDiscriminatorTag{shared})
	RegisterTagValidator(unionMemberTag{shared})
}

type unionTypeValidator struct {
	shared map[*types.Type]unions
}

func (unionTypeValidator) Init(_ *generator.Context) {}

func (unionTypeValidator) Name() string {
	return "unionTypeValidator"
}

func (utv unionTypeValidator) GetValidations(realType, _ *types.Type) (Validations, error) {
	result := Validations{}

	if realType.Kind != types.Struct {
		return result, nil
	}

	unions := utv.shared[realType]
	if len(unions) == 0 {
		return result, nil
	}

	// Sort the keys for stable output.
	keys := make([]string, 0, len(unions))
	for k := range unions {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, unionName := range keys {
		u := unions[unionName]
		if len(u.fieldMembers) > 0 || u.discriminator != nil {
			// TODO: Avoid the "local" here. This was added to to avoid errors caused when the package is an empty string.
			//       The correct package would be the output package but is not known here. This does not show up in generated code.
			// TODO: Append a consistent hash suffix to avoid generated name conflicts?
			supportVarName := PrivateVar{Name: "UnionMembershipFor" + realType.Name.Name + unionName, Package: "local"}
			if u.discriminator != nil {
				supportVar := Variable(supportVarName,
					Function(unionMemberTagName, DefaultFlags, newDiscriminatedUnionMembership,
						append([]any{*u.discriminator}, u.fields...)...))
				result.Variables = append(result.Variables, supportVar)
				fn := Function(unionMemberTagName, DefaultFlags, discriminatedUnionValidator,
					append([]any{supportVarName, u.discriminatorMember}, u.fieldMembers...)...)
				result.Functions = append(result.Functions, fn)
			} else {
				supportVar := Variable(supportVarName, Function(unionMemberTagName, DefaultFlags, newUnionMembership, u.fields...))
				result.Variables = append(result.Variables, supportVar)
				fn := Function(unionMemberTagName, DefaultFlags, unionValidator, append([]any{supportVarName}, u.fieldMembers...)...)
				result.Functions = append(result.Functions, fn)
			}
		}
	}

	return result, nil
}

const (
	unionDiscriminatorTagName = "k8s:unionDiscriminator"
	unionMemberTagName        = "k8s:unionMember"
)

type unionDiscriminatorTag struct {
	shared map[*types.Type]unions
}

func (unionDiscriminatorTag) Init(_ *generator.Context) {}

func (unionDiscriminatorTag) TagName() string {
	return unionDiscriminatorTagName
}

// Shared between unionDiscriminatorTag and unionMemberTag.
var unionTagScopes = sets.New(TagScopeField)

func (unionDiscriminatorTag) ValidScopes() sets.Set[TagScope] {
	return unionTagScopes
}

func (udt unionDiscriminatorTag) GetValidations(context Context, _ []string, payload string) (Validations, error) {
	p := &discriminatorParams{}
	if len(payload) > 0 {
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return Validations{}, fmt.Errorf("error parsing JSON value: %v (%q)", err, payload)
		}
	}
	if udt.shared[context.Parent] == nil {
		udt.shared[context.Parent] = unions{}
	}
	u := udt.shared[context.Parent].getOrCreate(p.Union)

	var discriminatorFieldName string
	if jsonAnnotation, ok := tags.LookupJSON(*context.Member); ok {
		discriminatorFieldName = jsonAnnotation.Name
		u.discriminator = &discriminatorFieldName
		u.discriminatorMember = *context.Member
	}

	// This tag does not actually emit any validations, it just accumulates
	// information. The validation is done by the unionTypeValidator.
	return Validations{}, nil
}

func (udt unionDiscriminatorTag) Docs() TagDoc {
	return TagDoc{
		Tag:         udt.TagName(),
		Contexts:    udt.ValidScopes().UnsortedList(),
		Description: "Indicates that this field is the discriminator for a union.",
		Payloads: []TagPayloadDoc{{
			Description: "<json-object>",
			Docs:        "",
			Schema: []TagPayloadSchema{{
				Key:   "union",
				Value: "<string>",
				Docs:  "the name of the union, if more than one exists",
			}},
		}},
	}
}

type unionMemberTag struct {
	shared map[*types.Type]unions
}

func (unionMemberTag) Init(_ *generator.Context) {}

func (unionMemberTag) TagName() string {
	return unionMemberTagName
}

func (unionMemberTag) ValidScopes() sets.Set[TagScope] {
	return unionTagScopes
}

func (umt unionMemberTag) GetValidations(context Context, _ []string, payload string) (Validations, error) {
	var fieldName string
	jsonTag, ok := tags.LookupJSON(*context.Member)
	if !ok {
		return Validations{}, fmt.Errorf("field %q is a union member but has no JSON struct field tag", context.Member)
	}
	fieldName = jsonTag.Name
	if len(fieldName) == 0 {
		return Validations{}, fmt.Errorf("field %q is a union member but has no JSON name", context.Member)
	}

	p := &memberParams{MemberName: context.Member.Name}
	if len(payload) > 0 {
		// Name may optionally be overridden by tag's memberName field.
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return Validations{}, fmt.Errorf("error parsing JSON value: %v (%q)", err, payload)
		}
	}
	if umt.shared[context.Parent] == nil {
		umt.shared[context.Parent] = unions{}
	}
	u := umt.shared[context.Parent].getOrCreate(p.Union)
	u.fields = append(u.fields, [2]string{fieldName, p.MemberName})
	u.fieldMembers = append(u.fieldMembers, *context.Member)

	// This tag does not actually emit any validations, it just accumulates
	// information. The validation is done by the unionTypeValidator.
	return Validations{}, nil
}

func (umt unionMemberTag) Docs() TagDoc {
	return TagDoc{
		Tag:         umt.TagName(),
		Contexts:    umt.ValidScopes().UnsortedList(),
		Description: "Indicates that this field is a member of a union.",
		Payloads: []TagPayloadDoc{{
			Description: "<json-object>",
			Docs:        "",
			Schema: []TagPayloadSchema{{
				Key:   "union",
				Value: "<string>",
				Docs:  "the name of the union, if more than one exists",
			}, {
				Key:     "memberName",
				Value:   "<string>",
				Docs:    "the discriminator value for this member",
				Default: "the field's name",
			}},
		}},
	}
}

// discriminatorParams defines JSON the parameter value for the
// +k8s:unionDiscriminator tag.
type discriminatorParams struct {
	// Union sets the name of the union this discriminator belongs to.
	// This is only needed for go structs that contain more than one union.
	// Optional.
	Union string `json:"union,omitempty"`
}

// memberParams defines the JSON parameter value for the +k8s:unionMember tag.
type memberParams struct {
	// Union sets the name of the union this member belongs to.
	// This is only needed for go structs that contain more than one union.
	// Optional.
	Union string `json:"union,omitempty"`
	// MemberName provides a name for a union member. If the union has a
	// discriminator, the member name must match the value the discriminator
	// is set to when this member is specified.
	// Optional.
	// Defaults to the go field name.
	MemberName string `json:"memberName,omitempty"`
}

// union defines how a union validation will be generated, based
// on +k8s:unionMember and +k8s:unionDiscriminator tags found in a go struct.
type union struct {
	// fields provides field information about all the members of the union.
	// Each slice element is a [2]string to provide a fieldName and memberName pair, where
	// [0] identifies the field name and [1] identifies the union member Name.
	// fields is index aligned with fieldMembers.
	// If member name is not set, it defaults to the go struct field name.
	fields []any
	// fieldMembers is a list of types.Member for all the members of the union.
	fieldMembers []any

	// discriminator is the name of the discriminator field
	discriminator *string
	// discriminatorMember is the types.Member of the discriminator field.
	discriminatorMember any
}

// unions represents all the unions for a go struct.
type unions map[string]*union

// getOrCreate gets a union by name, or initializes a new union by the given name.
func (us unions) getOrCreate(name string) *union {
	var u *union
	var ok bool
	if u, ok = us[name]; !ok {
		u = &union{}
		us[name] = u
	}
	return u
}
