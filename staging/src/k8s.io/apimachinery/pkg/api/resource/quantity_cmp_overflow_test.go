/*
Copyright The Kubernetes Authors.

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

package resource

import "testing"

// Cmp is bounded and correct when the two operands' scales differ by more than
// an int32 can represent.
func TestCmpLargeScaleDifference(t *testing.T) {
	// ParseQuantity caps these values, so they are built directly.
	cases := []struct {
		a, b Quantity
		want int
	}{
		{*NewScaledQuantity(1, 2147483647), MustParse("1n"), 1},
		{MustParse("1n"), *NewScaledQuantity(1, 2147483647), -1},
		{*NewScaledQuantity(1, 2147483647), MustParse("500m"), 1},
		{*NewScaledQuantity(1, 2147483647), MustParse("1"), 1},
		{*NewScaledQuantity(-1, 2147483647), MustParse("1n"), -1},
		{*NewScaledQuantity(-1, 2147483647), MustParse("-1n"), -1},
		{*NewScaledQuantity(1, 2147483647), *NewScaledQuantity(1, 2147483647), 0},
		{*NewScaledQuantity(1, 2147483647), *NewScaledQuantity(1, 2147483646), 1},
		{*NewScaledQuantity(1, 10000000), MustParse("1Gi"), 1},
		{MustParse("1Gi"), *NewScaledQuantity(1, 10000000), -1},
	}
	for _, tc := range cases {
		a, b := tc.a, tc.b
		if got := a.Cmp(b); got != tc.want {
			t.Errorf("%s.Cmp(%s) = %d, want %d", a.String(), b.String(), got, tc.want)
		}
	}
}

// CmpInt64 is bounded the same way for a decimal-backed receiver.
func TestCmpInt64LargeScaleDifference(t *testing.T) {
	q := *NewScaledQuantity(1, 2147483647)
	q.ToDec()
	if got := q.CmpInt64(1); got != 1 {
		t.Errorf("CmpInt64(1) = %d, want 1", got)
	}
}
