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

package validation

import (
	"fmt"
	"testing"
)

type (
	Y struct {
		I int
		B bool
		F float32
		U uint
	}
	X struct {
		Ptr   *X
		Y     Y
		Map   map[string]int
		Slice []int
	}
)

func TestTweak(t *testing.T) {
	testCases := []struct {
		path  Path
		in    any
		value any
	}{
		{
			path:  Path{Part{Field: "B"}},
			in:    &Y{B: true},
			value: false,
		},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%v", tc.path), func(t *testing.T) {
			err := Tweak(tc.in, tc.path, tc.value)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			fmt.Printf("%#+v\n", tc.in)
		})
	}
}
