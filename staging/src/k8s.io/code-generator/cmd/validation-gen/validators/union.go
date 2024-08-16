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
	"strings"

	"k8s.io/gengo/v2"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/parser/tags"
	"k8s.io/gengo/v2/types"
)

var discriminatedUnionValidator = types.Name{Package: libValidationPkg, Name: "DiscriminatedUnion"}
var unionValidator = types.Name{Package: libValidationPkg, Name: "Union"}

var newDiscriminatedUnionMembership = types.Name{Package: libValidationPkg, Name: "NewDiscriminatedUnionMembership"}
var newUnionMembership = types.Name{Package: libValidationPkg, Name: "NewUnionMembership"}

func init() {
	AddToRegistry(InitUnionDeclarativeValidator)
}

func InitUnionDeclarativeValidator(c *generator.Context) DeclarativeValidator {
	return &unionDeclarativeValidator{
		universe: c.Universe,
	}
}

type unionDeclarativeValidator struct {
	universe types.Universe
}

const (
	// +union and +unionDiscriminator tag are used by openapi-gen to publish x-kubernetes-union and x-kubernetes-discriminator
	// extensions into Kubernetes published OpenAPI.
	discriminatorTagName = "unionDiscriminator"
	memberTagName        = "unionMember"
)

type discriminatorParams struct {
	Union string `json:"union,omitempty"`
}

type memberParams struct {
	Union      string `json:"union,omitempty"`
	MemberName string `json:"memberName,omitempty"`
}

type union struct {
	fields       []any
	fieldMembers []any

	discriminator       *string
	discriminatorMember any
}

type unions map[string]*union

func (us unions) getOrCreate(name string) *union {
	var u *union
	var ok bool
	if u, ok = us[name]; !ok {
		u = &union{}
		us[name] = u
	}
	return u
}

func (c *unionDeclarativeValidator) ExtractValidations(t *types.Type, comments []string) (ValidatorGen, error) {
	result := ValidatorGen{}

	// TODO: handle multiple unions
	// This really only works if we name each union
	// so the most obvious approach is to track data by union
	unions := unions{}

	for _, member := range t.Members {
		commentTags := gengo.ExtractCommentTags("+", member.CommentLines)
		if commentTags, ok := commentTags[memberTagName]; ok {
			if len(commentTags) != 1 {
				return result, fmt.Errorf("must have one %q tag", memberTagName)
			}
			tag := commentTags[0]
			var fieldName string
			if jsonAnnotation, ok := tags.LookupJSON(member); ok {
				fieldName = jsonAnnotation.Name
				if len(fieldName) > 0 {
					p := &memberParams{MemberName: member.Name}
					if len(tag) > 0 {
						if err := json.Unmarshal([]byte(tag), &p); err != nil {
							return result, fmt.Errorf("error parsing JSON value: %v (%q)", err, tag)
						}
					}
					u := unions.getOrCreate(p.Union)

					if fieldName == strings.ToLower(p.MemberName[:1])+p.MemberName[1:] {
						u.fields = append(u.fields, fieldName) // member name follows conventions, only track field name
					} else {
						u.fields = append(u.fields, [2]string{fieldName, p.MemberName}) // member name is custom, track it
					}
					u.fieldMembers = append(u.fieldMembers, member)
				}
			}
		}
		if commentTags, ok := commentTags[discriminatorTagName]; ok {
			if len(commentTags) != 1 {
				return result, fmt.Errorf("must have one %q tag", memberTagName)
			}
			tag := commentTags[0]

			p := &discriminatorParams{}
			if len(tag) > 0 {
				if err := json.Unmarshal([]byte(tag), &p); err != nil {
					return result, fmt.Errorf("error parsing JSON value: %v (%q)", err, tag)
				}
			}
			u := unions.getOrCreate(p.Union)

			var discriminatorFieldName string
			if jsonAnnotation, ok := tags.LookupJSON(member); ok {
				discriminatorFieldName = jsonAnnotation.Name
				u.discriminator = &discriminatorFieldName
				u.discriminatorMember = member
			}
		}
	}
	for unionName, u := range unions {
		if len(u.fieldMembers) > 0 || u.discriminator != nil {
			// TODO: Avoid the "local" here. This was added to to avoid errors caused when the package is an empty string.
			//       The correct package would be the output package but is not known here. This does not show up in generated code.
			// TODO: Append a consistent hash suffix to avoid generated name conflicts?
			supportVarName := PrivateVar{Name: "UnionMembershipFor" + t.Name.Name + unionName, Package: "local"}
			if u.discriminator != nil {
				supportVar := Variable(supportVarName, Function(memberTagName, DefaultFlags, newDiscriminatedUnionMembership, append([]any{*u.discriminator}, u.fields...)...))
				result.Variables = append(result.Variables, supportVar)
				fn := Function(memberTagName, DefaultFlags, discriminatedUnionValidator, append([]any{supportVarName, u.discriminatorMember}, u.fieldMembers...)...)
				result.Functions = append(result.Functions, fn)
			} else {
				supportVar := Variable(supportVarName, Function(memberTagName, DefaultFlags, newUnionMembership, u.fields...))
				result.Variables = append(result.Variables, supportVar)
				fn := Function(memberTagName, DefaultFlags, unionValidator, append([]any{supportVarName}, u.fieldMembers...)...)
				result.Functions = append(result.Functions, fn)
			}
		}
	}

	return result, nil
}
