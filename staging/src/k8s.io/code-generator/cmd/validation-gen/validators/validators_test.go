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
	"reflect"
	"testing"

	"k8s.io/gengo/v2/types"
)

func TestSortFunctions(t *testing.T) {
	typeX := types.Name{Name: "x", Package: "x/x"}
	cases := []struct {
		name  string
		input []FunctionGen
		want  []FunctionGen
	}{
		{
			name: "flags",
			input: []FunctionGen{
				Function("x", DefaultFlags, types.Name{Name: "a"}),
				Function("x", PtrOK, types.Name{Name: "b"}),
				Function("x", IsFatal, types.Name{Name: "c"}),
				Function("x", PtrOK|IsFatal, types.Name{Name: "d"}),
			},
			want: []FunctionGen{
				Function("x", PtrOK|IsFatal, types.Name{Name: "d"}),
				Function("x", IsFatal, types.Name{Name: "c"}),
				Function("x", PtrOK, types.Name{Name: "b"}),
				Function("x", DefaultFlags, types.Name{Name: "a"}),
			},
		},
		{
			name: "names",
			input: []FunctionGen{
				Function("x", DefaultFlags, types.Name{Name: "b"}),
				Function("x", DefaultFlags, types.Name{Name: "a"}),
			},
			want: []FunctionGen{
				Function("x", DefaultFlags, types.Name{Name: "a"}),
				Function("x", DefaultFlags, types.Name{Name: "b"}),
			},
		},
		{
			name: "arg counts",
			input: []FunctionGen{
				Function("x", DefaultFlags, typeX, "a", "b"),
				Function("x", DefaultFlags, typeX, "a"),
			},
			want: []FunctionGen{
				Function("x", DefaultFlags, typeX, "a"),
				Function("x", DefaultFlags, typeX, "a", "b"),
			},
		},
		{
			name: "string literal args",
			input: []FunctionGen{
				Function("x", DefaultFlags, typeX, "b", "d"),
				Function("x", DefaultFlags, typeX, "b", "c"),
				Function("x", DefaultFlags, typeX, "b"),
				Function("x", DefaultFlags, typeX, "a"),
			},
			want: []FunctionGen{
				Function("x", DefaultFlags, typeX, "a"),
				Function("x", DefaultFlags, typeX, "b"),
				Function("x", DefaultFlags, typeX, "b", "c"),
				Function("x", DefaultFlags, typeX, "b", "d"),
			},
		},
		{
			name: "variable args",
			input: []FunctionGen{
				Function("x", DefaultFlags, typeX, PrivateVar{Name: "b"}, PrivateVar{Name: "d"}),
				Function("x", DefaultFlags, typeX, PrivateVar{Name: "b"}, PrivateVar{Name: "c"}),
				Function("x", DefaultFlags, typeX, PrivateVar{Name: "b"}),
				Function("x", DefaultFlags, typeX, PrivateVar{Name: "a"}),
			},
			want: []FunctionGen{
				Function("x", DefaultFlags, typeX, PrivateVar{Name: "a"}),
				Function("x", DefaultFlags, typeX, PrivateVar{Name: "b"}),
				Function("x", DefaultFlags, typeX, PrivateVar{Name: "b"}, PrivateVar{Name: "c"}),
				Function("x", DefaultFlags, typeX, PrivateVar{Name: "b"}, PrivateVar{Name: "d"}),
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SortFunctions(c.input)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("SortFunctions(%s) = %s, want %s", c.input, got, c.want)
			}
		})
	}
}
