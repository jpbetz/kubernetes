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

package validate

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

// Union verifies that exactly one member of a union is specified.
//
// UnionMembership must define all the members of the union.
//
// For example:
//
//	var abcUnionMembership := schema.NewUnionMembership("a", "b", "c")
//	func ValidateABC(fldPath, *field.Path, in *ABC) (errs fields.ErrorList) {
//		errs = append(errs, Union(fldPath, in, abcUnionMembership, in.A, in.B, in.C)...)
//		return errs
//	}
func Union(fldPath *field.Path, in any, union *UnionMembership, fieldValues ...any) field.ErrorList {
	if len(union.members) != len(fieldValues) {
		return field.ErrorList{field.InternalError(fldPath, fmt.Errorf("unexpected difference in length between fields defined in UnionMembership as fieldValues"))}
	}
	var specifiedMember *string
	for i, fieldValue := range fieldValues {
		rv := reflect.ValueOf(fieldValue)
		if rv.IsValid() && !rv.IsZero() {
			m := union.members[i]
			if specifiedMember != nil && *specifiedMember != m.memberName {
				return field.ErrorList{field.Invalid(fldPath, "", fmt.Sprintf("must not specify %s, must specify one of: %s", union.specifiedFields(fieldValues), union.allFields()))}
			}
			name := m.memberName
			specifiedMember = &name
		}
	}
	if specifiedMember == nil {
		return field.ErrorList{field.Invalid(fldPath, "", fmt.Sprintf("must specify one of: %s", union.allFields()))}
	}
	return nil
}

// DiscriminatedUnion verifies specified union member matches the discriminator.
//
// UnionMembership must define all the members of the union and the discriminator.
//
// For example:
//
//	var abcUnionMembership := schema.NewDiscriminatedUnionMembership("type", "a", "b", "c")
//	func ValidateABC(fldPath, *field.Path, in *ABC) (errs fields.ErrorList) {
//		errs = append(errs, DiscriminatedUnion(fldPath, in, abcUnionMembership, in.Type, in.A, in.B, in.C)...)
//		return errs
//	}
func DiscriminatedUnion[T ~string](fldPath *field.Path, in any, union *UnionMembership, discriminatorValue T, fieldValues ...any) (errs field.ErrorList) {
	discriminatorStrValue := string(discriminatorValue)
	if len(union.members) != len(fieldValues) {
		return field.ErrorList{field.InternalError(fldPath, fmt.Errorf("unexpected difference in length between fields defined in UnionMembership and fieldValues"))}
	}
	for i, fieldValue := range fieldValues {
		member := union.members[i]
		isDiscriminatedMember := discriminatorStrValue == member.memberName
		rv := reflect.ValueOf(fieldValue)
		isSpecified := rv.IsValid() && !rv.IsZero()
		if isSpecified && !isDiscriminatedMember {
			errs = append(errs, field.Invalid(fldPath.Child(member.fieldName), "", fmt.Sprintf("must not be specified when %s is %s", strconv.Quote(fmt.Sprint(union.discriminatorName)), strconv.Quote(fmt.Sprint(discriminatorValue)))))
		} else if !isSpecified && isDiscriminatedMember {
			errs = append(errs, field.Invalid(fldPath.Child(member.fieldName), "", fmt.Sprintf("must be specified when %s is %s", strconv.Quote(fmt.Sprint(union.discriminatorName)), strconv.Quote(fmt.Sprint(discriminatorValue)))))
		}
	}
	return errs
}

type member struct {
	fieldName, memberName string
}

// UnionMembership represents an ordered list of field union memberships.
type UnionMembership struct {
	discriminatorName string
	members           []member
}

// NewUnionMembership returns a new UnionMembership for the given list of members.
//
// Each member entry may one of:
//   - string: To provide the fieldName of a field union member where the union member name is CamelCase of the fieldName.
//   - [2]string: To provide a fieldName and memberName pair, where [0] identifies the field fieldName and [1] identifies the
//     union member Name.
//
// Field names must be unique.
func NewUnionMembership(member ...any) *UnionMembership {
	return NewDiscriminatedUnionMembership("", member...)
}

// NewDiscriminatedUnionMembership returns a new UnionMembership for the given discriminator field and list of members.
// members are provided in the same way as for NewUnionMembership.
func NewDiscriminatedUnionMembership(discriminatorFieldName string, members ...any) *UnionMembership {
	u := &UnionMembership{}
	u.discriminatorName = discriminatorFieldName
	for _, fieldName := range members {
		switch f := fieldName.(type) {
		case string:
			u.members = append(u.members, member{fieldName: f, memberName: strings.ToUpper(f[:1]) + f[1:]})
		case [2]string:
			u.members = append(u.members, member{fieldName: f[0], memberName: f[1]})
		}
	}
	return u
}

// specifiedFields returns a string listing all the field names of the specified fieldValues for use in error reporting.
func (u UnionMembership) specifiedFields(fieldValues []any) string {
	var membersSpecified []string
	for i, fieldValue := range fieldValues {
		rv := reflect.ValueOf(fieldValue)
		if rv.IsValid() && !rv.IsZero() {
			f := u.members[i]
			membersSpecified = append(membersSpecified, strconv.Quote(fmt.Sprint(f.fieldName)))
		}
	}
	return joinWithSeparators(membersSpecified, ", ", "and")
}

// specifiedFields returns a string listing all the field names of the member of a union for use in error reporting.
func (u UnionMembership) allFields() string {
	memberNames := make([]string, 0, len(u.members))
	for _, f := range u.members {
		memberNames = append(memberNames, strconv.Quote(fmt.Sprint(f.fieldName)))
	}
	return joinWithSeparators(memberNames, ", ", "or")
}

func joinWithSeparators(values []string, separator string, lastSeparator string) string {
	switch len(values) {
	case 0:
		return ""
	case 1:
		return values[0]
	default:
		sort.Strings(values)
		return strings.Join(values[0:len(values)-1], separator) + lastSeparator + values[len(values)-1]
	}
}
