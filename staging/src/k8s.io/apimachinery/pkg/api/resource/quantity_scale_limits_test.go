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
	"fmt"
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

func TestQuantityAsScale(t *testing.T) {
	for _, tc := range []struct {
		in    Quantity
		scale Scale
		want  Quantity
		ok    bool
	}{
		{intQuantity(1500, Milli, DecimalSI), Milli, intQuantity(1500, Milli, DecimalSI), true},
		{intQuantity(1500, Milli, DecimalSI), 0, intQuantity(2, 0, DecimalSI), false},
		{intQuantity(-1500, Milli, DecimalSI), 0, intQuantity(-2, 0, DecimalSI), false},
		{intQuantity(5, 0, DecimalSI), math.MinInt32, intQuantity(5, 0, DecimalSI), true},
		{intQuantity(-5, 0, DecimalSI), math.MinInt32, intQuantity(-5, 0, DecimalSI), true},
		{intQuantity(0, 0, DecimalSI), math.MinInt32, intQuantity(0, 0, DecimalSI), true},
		{intQuantity(1, math.MinInt32+1, DecimalSI), math.MinInt32, intQuantity(1, math.MinInt32+1, DecimalSI), true},
		{intQuantity(1, math.MaxInt32, DecimalSI), math.MinInt32, intQuantity(1, math.MaxInt32, DecimalSI), true},
		{*NewDecimalQuantity(*inf.NewDec(5, math.MinInt32), DecimalSI), math.MinInt32, *NewDecimalQuantity(*inf.NewDec(5, math.MinInt32), DecimalSI), true},
		{intQuantity(5, 0, DecimalSI), math.MinInt32 + 1, intQuantity(5, 0, DecimalSI), true},
		{intQuantity(5, 0, DecimalSI), math.MaxInt32, intQuantity(1, math.MaxInt32, DecimalSI), false},
		{intQuantity(5, math.MinInt32+1, DecimalSI), 0, intQuantity(1, 0, DecimalSI), false},
		{intQuantity(-5, math.MinInt32+1, DecimalSI), 0, intQuantity(-1, 0, DecimalSI), false},
		{intQuantity(0, math.MinInt32+1, DecimalSI), 0, intQuantity(0, 0, DecimalSI), true},
	} {
		format := func(d *inf.Dec) string { return fmt.Sprintf("%v*10^%d", d.UnscaledBig(), -int64(d.Scale())) }
		want := tc.want.AsDec()
		for _, asDec := range []bool{false, true} {
			q := tc.in.DeepCopy()
			if asDec {
				q.ToDec()
			}
			result, ok := q.AsScale(tc.scale)
			var got *inf.Dec
			switch v := result.(type) {
			case int64Amount:
				got = v.AsDec()
			case infDecAmount:
				if v.Dec == q.d.Dec {
					t.Errorf("%s (asDec=%t): AsScale returned the receiver's inf.Dec, want a copy", tc.in.String(), asDec)
				}
				got = v.Dec
			}
			if got.Cmp(want) != 0 || ok != tc.ok {
				t.Errorf("%s (asDec=%t) = (%s, %t), want (%s, %t)", tc.in.String(), asDec, format(got), ok, format(want), tc.ok)
			}
		}
	}
}

// TestQuantityRoundUpDecAtScaleLimits checks RoundUp on the inf.Dec form when
// the value is less than one unit of the requested scale.
func TestQuantityRoundUpDecAtScaleLimits(t *testing.T) {
	format := func(d *inf.Dec) string { return fmt.Sprintf("%v*10^%d", d.UnscaledBig(), -int64(d.Scale())) }
	for _, tc := range []struct {
		in    Quantity
		scale Scale
		want  string
		ok    bool
	}{
		{*NewScaledQuantity(1, -math.MaxInt32), 1, "1*10^1", false},
		{*NewScaledQuantity(5, math.MinInt32+2), 2, "1*10^2", false},
		{*NewScaledQuantity(5, math.MinInt32+1), math.MaxInt32, "1*10^2147483647", false},
		{*NewDecimalQuantity(*inf.NewDec(5, math.MaxInt32-1), DecimalSI), math.MaxInt32 - 1, "1*10^2147483646", false},
	} {
		q := tc.in.DeepCopy()
		q.ToDec()
		ok := q.RoundUp(tc.scale)
		if got := format(q.AsDec()); got != tc.want || ok != tc.ok {
			t.Errorf("%s RoundUp(%d) = (%s, %t), want (%s, %t)", format(tc.in.AsDec()), tc.scale, got, ok, tc.want, tc.ok)
		}
	}
}
