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
	"context"
	"regexp"
	"testing"

	"k8s.io/apimachinery/pkg/api/operation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	fldTesting "k8s.io/apimachinery/pkg/util/validation/field/testing"
)

func TestMaxLength(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		max      int
		wantErrs field.ErrorList // regex
	}{{
		name:     "empty string",
		value:    "",
		max:      0,
		wantErrs: nil,
	}, {
		name:  "zero length",
		value: "0",
		max:   0,
		wantErrs: field.ErrorList{
			field.Invalid(field.NewPath("fldpath"), nil, "must be no more than").WithOrigin("maxLength"),
		},
	}, {
		name:     "one character",
		value:    "0",
		max:      1,
		wantErrs: nil,
	}, {
		name:  "two characters",
		value: "01",
		max:   1,
		wantErrs: field.ErrorList{
			field.Invalid(field.NewPath("fldpath"), nil, "must be no more than").WithOrigin("maxLength"),
		},
	}, {
		value: "",
		max:   -1,
		wantErrs: field.ErrorList{
			field.Invalid(field.NewPath("fldpath"), nil, "must be no more than").WithOrigin("maxLength"),
		},
	}}

	matcher := fldTesting.Match().ByOrigin().ByDetailSubstring().ByField().ByType()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := tc.value
			gotErrs := MaxLength(context.Background(), operation.Operation{}, field.NewPath("fldpath"), &v, nil, tc.max)
			fldTesting.MatchErrors(t, tc.wantErrs, gotErrs, matcher)
		})
	}
}

func TestMaxItems(t *testing.T) {
	cases := []struct {
		fn  func(op operation.Operation, fp *field.Path) field.ErrorList
		err string // regex
	}{{
		fn: func(op operation.Operation, fp *field.Path) field.ErrorList {
			value := make([]string, 0)
			max := 0
			return MaxItems(context.Background(), op, fp, value, nil, max)
		},
	}, {
		fn: func(op operation.Operation, fp *field.Path) field.ErrorList {
			value := make([]string, 1)
			max := 0
			return MaxItems(context.Background(), op, fp, value, nil, max)
		},
		err: "fldpath: Too many.*must have at most",
	}, {
		fn: func(op operation.Operation, fp *field.Path) field.ErrorList {
			value := make([]int, 1)
			max := 1
			return MaxItems(context.Background(), op, fp, value, nil, max)
		},
	}, {
		fn: func(op operation.Operation, fp *field.Path) field.ErrorList {
			value := make([]int, 2)
			max := 1
			return MaxItems(context.Background(), op, fp, value, nil, max)
		},
		err: "fldpath: Too many.*must have at most",
	}, {
		fn: func(op operation.Operation, fp *field.Path) field.ErrorList {
			value := make([]bool, 0)
			max := -1
			return MaxItems(context.Background(), op, fp, value, nil, max)
		},
		err: "fldpath: Too many.*too many items",
	}}

	for i, tc := range cases {
		result := tc.fn(operation.Operation{}, field.NewPath("fldpath"))
		if len(result) > 0 && tc.err == "" {
			t.Errorf("case %d: unexpected failure: %v", i, fmtErrs(result))
			continue
		}
		if len(result) == 0 && tc.err != "" {
			t.Errorf("case %d: unexpected success: expected %q", i, tc.err)
			continue
		}
		if len(result) > 0 {
			if len(result) > 1 {
				t.Errorf("case %d: unexepected multi-error: %v", i, fmtErrs(result))
				continue
			}
			if re := regexp.MustCompile(tc.err); !re.MatchString(result[0].Error()) {
				t.Errorf("case %d: wrong error\nexpected: %q\n     got: %v", i, tc.err, fmtErrs(result))
			}
		}
	}
}
