/*
Copyright 2026 The Kubernetes Authors.

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

import (
	"math"
	"testing"

	inf "gopkg.in/inf.v0"
)

// TestInt64AsCanonicalBytesAtScaleLimits checks that the exponent stays a
// multiple of 3 inside the int32 range at both ends of the Scale range.
func TestInt64AsCanonicalBytesAtScaleLimits(t *testing.T) {
	for _, test := range []struct {
		value    int64
		scale    Scale
		result   string
		exponent int32
	}{
		{10, math.MaxInt32, "100", math.MaxInt32 - 1},
		{1000, math.MaxInt32, "10000", math.MaxInt32 - 1},
		{math.MaxInt64, math.MaxInt32, "92233720368547758070", math.MaxInt32 - 1},
		{1000, math.MinInt32, "10", math.MinInt32 + 2},
		// No multiple of 3 fits at or below these exponents.
		{1, math.MinInt32 + 1, "1", math.MinInt32 + 1},
		{10, math.MinInt32, "1", math.MinInt32 + 1},
		{1, math.MinInt32, "1", math.MinInt32},
	} {
		r, exp := int64Amount{value: test.value, scale: test.scale}.AsCanonicalBytes(nil)
		if string(r) != test.result {
			t.Errorf("%v: unexpected result: %s", test, r)
		}
		if exp != test.exponent {
			t.Errorf("%v: unexpected exponent: %d", test, exp)
		}
	}
}

// TestQuantityStringAtScaleLimits checks String at both ends of the Scale
// range, in the int64 form and after ToDec.
func TestQuantityStringAtScaleLimits(t *testing.T) {
	for _, tc := range []struct {
		in    Quantity
		toDec bool
		want  string
	}{
		{intQuantity(1024, math.MinInt32+1, BinarySI), false, "1024e-2147483647"},
		{intQuantity(1024, math.MinInt32+1, BinarySI), true, "1024e-2147483647"},
		{intQuantity(1, math.MinInt32+1, DecimalSI), false, "1e-2147483647"},
		{intQuantity(1, math.MinInt32+1, DecimalSI), true, "1e-2147483647"},
		{intQuantity(1, math.MinInt32+1, DecimalExponent), false, "1e-2147483647"},
		{intQuantity(1, math.MinInt32+1, DecimalExponent), true, "1e-2147483647"},
		{intQuantity(1, math.MinInt32, DecimalSI), false, "1e-2147483648"},
		{intQuantity(1, math.MinInt32, DecimalExponent), false, "1e-2147483648"},
		{intQuantity(10, math.MinInt32, DecimalSI), false, "1e-2147483647"},
		{intQuantity(10, math.MaxInt32, DecimalExponent), false, "100e2147483646"},
		{intQuantity(10, math.MaxInt32, DecimalExponent), true, "100e2147483646"},
		{intQuantity(1000, math.MaxInt32, DecimalExponent), false, "10000e2147483646"},
		{intQuantity(1000, math.MaxInt32, DecimalExponent), true, "10000e2147483646"},
		// 2147483646 is the largest multiple of 3 that fits in an int32, so the
		// exponent stops there and the extra zeros stay in the mantissa.
		{*NewDecimalQuantity(*inf.NewDec(100, math.MinInt32), DecimalExponent), false, "10000e2147483646"},
	} {
		q := tc.in.DeepCopy()
		if tc.toDec {
			q.ToDec()
		}
		if got := q.String(); got != tc.want {
			t.Errorf("%#v (toDec=%t): String() = %q, want %q", tc.in, tc.toDec, got, tc.want)
		}
	}
}

// TestParseQuantityEmitAtScaleLimits checks the String of parsed quantities at
// both ends of the Scale range, in the int64 and inf.Dec forms and negated.
func TestParseQuantityEmitAtScaleLimits(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"10e2147483647", "100e2147483646"},
		{"1000e2147483646", "1000e2147483646"},
		{"10000e2147483646", "10000e2147483646"},
	} {
		for _, sign := range []string{"", "-"} {
			for _, asDec := range []bool{false, true} {
				q, err := ParseQuantity(sign + tc.in)
				if err != nil {
					t.Errorf("ParseQuantity(%q) failed: %v", sign+tc.in, err)
					continue
				}
				if asDec {
					q.ToDec()
				}
				if got := q.String(); got != sign+tc.want {
					t.Errorf("ParseQuantity(%q) (asDec=%t): String() = %q, want %q", sign+tc.in, asDec, got, sign+tc.want)
				}
			}
		}
	}
}
