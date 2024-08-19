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
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

type testUnion struct{}
type testMember struct{}

func TestUnion(t *testing.T) {
	testCases := []struct {
		name        string
		fields      []any
		fieldValues []any
		expected    field.ErrorList
	}{
		{
			name:        "valid pointers one of",
			fields:      []any{"a", "b", "c", "d"},
			fieldValues: []any{nil, nil, nil, &testMember{}},
			expected:    nil,
		},
		{
			name:        "invalid pointers one of",
			fields:      []any{"a", "b", "c", "d"},
			fieldValues: []any{nil, &testMember{}, nil, &testMember{}},
			expected:    field.ErrorList{field.Invalid(nil, "", "must not specify \"b\" and \"d\", must specify one of: \"a\", \"b\", \"c\" or \"d\"")},
		},
		{
			name:        "valid string one of",
			fields:      []any{"a", "b", "c", "d"},
			fieldValues: []any{"", "", "", "x"},
			expected:    nil,
		},
		{
			name:        "invalid string one of",
			fields:      []any{"a", "b", "c", "d"},
			fieldValues: []any{"", "x", "", "x"},
			expected:    field.ErrorList{field.Invalid(nil, "", "must not specify \"b\" and \"d\", must specify one of: \"a\", \"b\", \"c\" or \"d\"")},
		},
		{
			name:        "valid mixed type one of",
			fields:      []any{"a", "b", "c", "d"},
			fieldValues: []any{0, "", nil, "x"},
			expected:    nil,
		},
		{
			name:        "invalid mixed type one of",
			fields:      []any{"a", "b", "c", "d"},
			fieldValues: []any{0, "x", nil, &testMember{}},
			expected:    field.ErrorList{field.Invalid(nil, "", "must not specify \"b\" and \"d\", must specify one of: \"a\", \"b\", \"c\" or \"d\"")},
		},
		{
			name:        "invalid no member set",
			fields:      []any{"a", "b", "c", "d"},
			fieldValues: []any{nil, nil, nil, nil},
			expected:    field.ErrorList{field.Invalid(nil, "", "must specify one of: \"a\", \"b\", \"c\" or \"d\"")},
		},
		{
			name:        "valid multiple field for union member",
			fields:      []any{[2]string{"a", "x"}, "b", "c", [2]string{"d", "x"}},
			fieldValues: []any{1, nil, nil, 1},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := Union(nil, &testUnion{}, NewUnionMembership(tc.fields...), tc.fieldValues...)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got %v want %v", got, tc.expected)
			}
		})
	}
}

func TestDiscriminatedUnion(t *testing.T) {
	testCases := []struct {
		name               string
		discriminatorField string
		fields             []any
		discriminatorValue string
		fieldValues        []any
		expected           field.ErrorList
	}{
		{
			name:               "valid discriminated union",
			discriminatorField: "d",
			fields:             []any{[2]string{"a", "A"}, "b", "c", [2]string{"d", "D"}},
			discriminatorValue: "A",
			fieldValues:        []any{1, nil, nil, nil},
		},
		{
			name:               "valid discriminated union, implicit union name",
			discriminatorField: "type",
			fields:             []any{[2]string{"a", "A"}, "b", "c", [2]string{"d", "D"}},
			discriminatorValue: "B",
			fieldValues:        []any{nil, 1, nil, nil},
		},
		{
			name:               "invalid, discriminator not set to member that is specified",
			discriminatorField: "type",
			fields:             []any{[2]string{"a", "A"}, "b", "c", [2]string{"d", "D"}},
			discriminatorValue: "C",
			fieldValues:        []any{nil, 1, nil, nil},
			expected: field.ErrorList{
				field.Invalid(field.NewPath("b"), "", "must not be specified when \"type\" is \"C\""),
				field.Invalid(field.NewPath("c"), "", "must be specified when \"type\" is \"C\""),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := DiscriminatedUnion(nil, &testUnion{}, NewDiscriminatedUnionMembership(tc.discriminatorField, tc.fields...), tc.discriminatorValue, tc.fieldValues...)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got %v want %v", got.ToAggregate(), tc.expected.ToAggregate())
			}
		})
	}
}
