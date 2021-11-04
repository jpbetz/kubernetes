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

package cel

import (
	"fmt"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/third_party/forked/celopenapi/model"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"testing"
)

var celBuiltinMacroIdentifiers = sets.NewString(
	// macros and functions
	"has", "all", "exists", "exists_one", "map", "filter",
	"size", "contains", "dyn", "startsWith", "endsWith", "matches",
	// time related
	"duration", "timestamp",
	"getDate", "getDayOfMonth", "getDayOfWeek", "getDayOfYear", "getFullYear", "getHours", "getMilliseconds", "getMinutes", "getMonth", "getSeconds",
)

// TestPropNameEscaping tests that
func TestPropNameEscaping(t *testing.T) {
	cases := sets.NewString(
		"self",
		"_if", "__if", "___if",
		"_abc", "__abc", "___abc",
	). //
		Union(model.AlwaysReservedIdentifiers).
		// Must not be bound as root variables. Doing so would result in a compilation error:
		//"overlapping identifier for name '<identifier>'":
		Union(model.RootReservedIdentifiers).
		// Are allowed to be used as identifiers because the parser can disambiguate them from the function and
		// macro identifiers:
		Union(celBuiltinMacroIdentifiers)

	for _, prop := range cases.List() {
		escapedProp := model.Escape(prop)
		scopedIfNonRootProp := escapedProp
		if model.IsRootReserved(prop) || prop == ScopedVarName {
			scopedIfNonRootProp = "self." + scopedIfNonRootProp
		}
		t.Run(prop, func(t *testing.T) {
			s := &schema.Structural{
				Generic: schema.Generic{
					Type: "object",
				},
				Properties: map[string]schema.Structural{
					// prop names are not escaped in the schema
					prop: {
						Generic: schema.Generic{
							Type: "array",
						},
						Items: &schema.Structural{
							Generic: schema.Generic{
								Type: "string",
							},
						},
					},
				},
				Extensions: schema.Extensions{
					XValidations: apiextensions.ValidationRules{
						{
							// prop names are escaped in CEL validation rules
							Rule: fmt.Sprintf("has(self.%s) && %s == ['a', 'b', 'c'] && size(%s.filter(e, e == 'c')) == 1", escapedProp, scopedIfNonRootProp, scopedIfNonRootProp),
						},
					},
				},
			}
			obj := map[string]interface{}{
				// prop name remains unescaped here in the object
				prop: []interface{}{"a", "b", "c"},
			}
			celValidator := NewValidator(s)
			errs := celValidator.Validate(field.NewPath("root"), s, obj)
			for _, err := range errs {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
