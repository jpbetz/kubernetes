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
	"math/big"
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

// TestQuantityAddSubAcrossScales checks that Add and Sub are exact while the
// result fits in maxAddDigits significant digits, and round away from zero to
// maxAddDigits digits beyond that, in every combination of int64 and inf.Dec
// forms.
func TestQuantityAddSubAcrossScales(t *testing.T) {
	pow10 := func(n int) *big.Int { return new(big.Int).Exp(bigTen, big.NewInt(int64(n)), nil) }
	format := func(d *inf.Dec) string { return fmt.Sprintf("%v*10^%d", d.UnscaledBig(), -int64(d.Scale())) }
	for _, k := range []int{18, 19, 27, 28, 37, 38, 39, 100, math.MaxInt32} {
		// 1eK + 1 has k+1 digits and 1eK - 1 has k digits. Past maxAddDigits
		// digits, 1eK + 1 rounds up to 1, maxAddDigits-2 zeros and 1, and 1eK - 1
		// rounds up to 1eK.
		plusOne, minusOne := dec(1, k), dec(1, k)
		if k+1 <= maxAddDigits {
			plusOne = bigDec(new(big.Int).Add(pow10(k), bigOne), 0)
		} else {
			plusOne = bigDec(new(big.Int).Add(pow10(maxAddDigits-1), bigOne), k+1-maxAddDigits)
		}
		if k <= maxAddDigits {
			minusOne = bigDec(new(big.Int).Sub(pow10(k), bigOne), 0)
		}
		// shift moves a want down by k digits, from 1eK and 1 to 1 and 1e-K.
		shift := func(d infDecAmount) *inf.Dec { return inf.NewDecBig(d.UnscaledBig(), d.Scale()+inf.Scale(k)) }
		large, small, one := intQuantity(1, Scale(k), DecimalSI), intQuantity(1, Scale(-k), DecimalSI), intQuantity(1, 0, DecimalSI)
		for _, tc := range []struct {
			name string
			x, y Quantity
			op   func(q *Quantity, y Quantity)
			want *inf.Dec
		}{
			{fmt.Sprintf("1e%d + 1", k), large, one, (*Quantity).Add, plusOne.Dec},
			{fmt.Sprintf("1 + 1e%d", k), one, large, (*Quantity).Add, plusOne.Dec},
			{fmt.Sprintf("1e%d - 1", k), large, one, (*Quantity).Sub, minusOne.Dec},
			{fmt.Sprintf("1 - 1e%d", k), one, large, (*Quantity).Sub, new(inf.Dec).Neg(minusOne.Dec)},
			{fmt.Sprintf("1 + 1e-%d", k), one, small, (*Quantity).Add, shift(plusOne)},
			{fmt.Sprintf("1 - 1e-%d", k), one, small, (*Quantity).Sub, shift(minusOne)},
		} {
			for _, xDec := range []bool{false, true} {
				for _, yDec := range []bool{false, true} {
					x, y := tc.x.DeepCopy(), tc.y.DeepCopy()
					if xDec {
						x.ToDec()
					}
					if yDec {
						y.ToDec()
					}
					tc.op(&x, y)
					if got := x.AsDec(); got.Cmp(tc.want) != 0 {
						t.Errorf("%s (xDec=%t, yDec=%t) = %s, want %s", tc.name, xDec, yDec, format(got), format(tc.want))
					}
				}
			}
		}
	}
}

// TestQuantityAddSubZeroScale checks that adding or subtracting zero keeps the
// other operand's scale, in both the int64 and inf.Dec forms.
func TestQuantityAddSubZeroScale(t *testing.T) {
	for _, tc := range []struct {
		name  string
		x, y  Quantity
		op    func(q *Quantity, y Quantity)
		want  int64
		scale inf.Scale
	}{
		{"5 + 0n", intQuantity(5, 0, DecimalSI), intQuantity(0, Nano, DecimalSI), (*Quantity).Add, 5, 0},
		{"0n + 5", intQuantity(0, Nano, DecimalSI), intQuantity(5, 0, DecimalSI), (*Quantity).Add, 5, 0},
		{"5 - 0n", intQuantity(5, 0, DecimalSI), intQuantity(0, Nano, DecimalSI), (*Quantity).Sub, 5, 0},
		{"0n - 5", intQuantity(0, Nano, DecimalSI), intQuantity(5, 0, DecimalSI), (*Quantity).Sub, -5, 0},
		{"5k + 0", intQuantity(5, Kilo, DecimalSI), intQuantity(0, 0, DecimalSI), (*Quantity).Add, 5000, -3},
	} {
		for _, xDec := range []bool{false, true} {
			for _, yDec := range []bool{false, true} {
				x, y := tc.x.DeepCopy(), tc.y.DeepCopy()
				if xDec {
					x.ToDec()
				}
				if yDec {
					y.ToDec()
				}
				tc.op(&x, y)
				if x.CmpInt64(tc.want) != 0 {
					t.Errorf("%s (xDec=%t, yDec=%t) = %s, want %d", tc.name, xDec, yDec, x.String(), tc.want)
				}
				if got := x.AsDec().Scale(); got != tc.scale {
					t.Errorf("%s (xDec=%t, yDec=%t): AsDec().Scale() = %d, want %d", tc.name, xDec, yDec, got, tc.scale)
				}
			}
		}
	}
}
