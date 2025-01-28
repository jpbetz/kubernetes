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

package deep

import (
	"testing"
)

func Test(t *testing.T) {
	st := localSchemeBuilder.Test(t)

	st.Value(&Struct{
		StructField: OtherStruct{
			StructField: SmallStruct{StringField: "Not a DNS label"},
			SliceField: []SmallStruct{{
				StringField: "Not a DNS label",
			}, {
				StringField: "Not an IP",
			}},
			MapField: map[string]SmallStruct{
				"a": SmallStruct{StringField: "Not a DNS label"},
				"b": SmallStruct{StringField: "Not an IP"},
			},
		},
		StructPtrField: &OtherStruct{
			StructField: SmallStruct{StringField: "Not an IP"},
			SliceField: []SmallStruct{{
				StringField: "Not an IP",
			}, {
				StringField: "Not a DNS label",
			}},
			MapField: map[string]SmallStruct{
				"b": SmallStruct{StringField: "Not an IP"},
				"a": SmallStruct{StringField: "Not a DNS label"},
			},
		},
	}).ExpectRegexpsByPath(map[string][]string{
		"structField.structField.stringField": []string{
			"Invalid value:.*must contain only lower-case alphanumeric characters or '-'",
			"Invalid value:.*must start and end with lower-case alphanumeric characters",
		},
		"structField.sliceField[0].stringField": []string{
			"Invalid value:.*must contain only lower-case alphanumeric characters or '-'",
			"Invalid value:.*must start and end with lower-case alphanumeric characters",
		},
		"structField.sliceField[1].stringField": []string{
			"Invalid value:.*must contain only lower-case alphanumeric characters or '-'",
			"Invalid value:.*must start and end with lower-case alphanumeric characters",
		},
		"structField.mapField[a].stringField": []string{
			"Invalid value:.*must contain only lower-case alphanumeric characters or '-'",
			"Invalid value:.*must start and end with lower-case alphanumeric characters",
		},
		"structField.mapField[b].stringField": []string{
			"Invalid value:.*must contain only lower-case alphanumeric characters or '-'",
			"Invalid value:.*must start and end with lower-case alphanumeric characters",
		},

		"structPtrField.structField.stringField": []string{
			"Invalid value:.*must be a valid IP address",
		},
		"structPtrField.sliceField[0].stringField": []string{
			"Invalid value:.*must be a valid IP address",
		},
		"structPtrField.sliceField[1].stringField": []string{
			"Invalid value:.*must be a valid IP address",
		},
		"structPtrField.mapField[a].stringField": []string{
			"Invalid value:.*must be a valid IP address",
		},
		"structPtrField.mapField[b].stringField": []string{
			"Invalid value:.*must be a valid IP address",
		},
	})
}
