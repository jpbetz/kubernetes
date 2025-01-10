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

package validatefalse

import (
	"sort"
	"testing"

	operation "k8s.io/apimachinery/pkg/api/operation"
)

func TestSubfieldValidationWithValidateFalse(t *testing.T) {
	st := localSchemeBuilder.Test(t)

	st.Value(&T1{
		T2: T2{
			StringField:               "",
			StringFieldWithValidation: "",
			SliceField:                []string{},
			PointerField:              nil,
			MapField:                  map[string]string{},
		},
		PT2: &T2{
			StringField:               "",
			StringFieldWithValidation: "",
			SliceField:                []string{},
			PointerField:              nil,
			MapField:                  map[string]string{},
		},
	}).
		// All ifOptionDisabled validations should fail
		ExpectValidateFalse(
			"field T2.StringFieldWithValidation",
			"subfield T1.PT2.MapField",
			"subfield T1.PT2.PointerField",
			"subfield T1.PT2.SliceField",
			"subfield T1.PT2.StringField",
			"subfield T1.PT2.StringFieldWithValidation",
			"subfield T1.PT2.StructField",
			"subfield T1.T2.MapField",
			"subfield T1.T2.PointerField",
			"subfield T1.T2.SliceField",
			"subfield T1.T2.StringField",
			"subfield T1.T2.StringFieldWithValidation",
			"subfield T1.T2.StructField",
			"type T1",
		)
}

func TestSubfieldValidationWithValidateFalseCountDupeErrors(t *testing.T) {
	cases := []struct {
		name          string
		obj           *T1
		expectedPaths []string
		expectErrors  bool
	}{
		{
			name: "t1 subfield validation",
			obj: &T1{
				T2: T2{
					StringField:               "",
					StringFieldWithValidation: "",
					SliceField:                []string{},
					PointerField:              nil,
					MapField:                  map[string]string{},
				},
				PT2: &T2{
					StringField:               "",
					StringFieldWithValidation: "",
					SliceField:                []string{},
					PointerField:              nil,
					MapField:                  map[string]string{},
				},
			},
			expectedPaths: []string{
				"<nil>", // <nil> entry is for root validateFalse on "type T1"
				"pt2.mapField", "pt2.pointerField", "pt2.sliceField", "pt2.stringField",
				// there are two pt2.stringFieldWithValidation entries as there in inner and a field validation
				"pt2.stringFieldWithValidation", "pt2.stringFieldWithValidation", "pt2.structField",
				"t2.mapField", "t2.pointerField", "t2.sliceField", "t2.stringField",
				// there are two t2.stringFieldWithValidation entries as there in inner and a field validation
				"t2.stringFieldWithValidation", "t2.stringFieldWithValidation", "t2.structField",
			},
			expectErrors: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opCtx := operation.Context{}
			errs := Validate_T1(opCtx, nil, tc.obj, tc.obj)
			if tc.expectErrors && len(errs) == 0 {
				t.Error("expected validation errors but got none")
			}
			if !tc.expectErrors && len(errs) > 0 {
				t.Errorf("unexpected validation errors: %v", errs)
			}

			actualPaths := []string{}
			for _, err := range errs {
				actualPaths = append(actualPaths, err.Field)
			}

			sort.Strings(tc.expectedPaths)
			sort.Strings(actualPaths)

			if tc.expectErrors && !equalStringSlices(tc.expectedPaths, actualPaths) {
				t.Errorf("expected error paths %q, but got %q", tc.expectedPaths, actualPaths)
			}
		})
	}
}

// equalStringSlices compares if two string slices are equal
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func TestNew(t *testing.T) {
	st := localSchemeBuilder.Test(t)

	st.Value(&T3{
		T2: T2{
			StringField:               "",
			StringFieldWithValidation: "",
			SliceField2:               []StructField{{}, {}},
			//PointerField:              nil,
			//MapField:                  map[string]string{},
		},
	}).
		// All ifOptionDisabled validations should fail
		ExpectValidateFalseByPath(map[string][]string{
			"t2.stringFieldWithValidation":  []string{"field T2.StringFieldWithValidation"},
			"t2.stringField":                []string{"subfield T3.T2.StringField"},
			"t2.structField.stringField":    []string{"subfield T3.T2.StructField.StringField"},
			"t2.sliceField2[0].stringField": []string{"subfield T3.T2.SliceField[*].StringField"},
			"t2.sliceField2[1].stringField": []string{"subfield T3.T2.SliceField[*].StringField"},
		})
	////"subfield T3.T2.MapField",
	////"subfield T3.T2.PointerField",

}
