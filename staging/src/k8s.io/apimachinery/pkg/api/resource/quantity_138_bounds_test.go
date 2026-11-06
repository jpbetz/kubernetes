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
	"math/big"
	"strconv"
	"strings"
	"testing"
)

// TestParseQuantityOfStringsWrittenBy138 checks that ParseQuantity in this
// release returns the same value for the strings that ParseQuantity in 1.38
// writes at the edges of its range.
//
// In 1.38, ParseQuantity caps a value at ±math.MaxFloat64, rounds a nonzero
// value smaller than 1n up to 1n, and keeps at most 29 significant digits. So
// 1.38 writes at most 31 digits (29, plus two zeros that align the exponent to
// a multiple of 3) in front of an exponent from -9 to 308.
func TestParseQuantityOfStringsWrittenBy138(t *testing.T) {
	// Strings that 1.38 writes for its largest and smallest values. Each one
	// must print unchanged after it is parsed.
	unchanged := []string{
		// math.MaxFloat64, which 1.38 writes for "1e400" and any larger value.
		"179769313486231570e291",
		"-179769313486231570e291",
		// 1.38 keeps these as written. 308 is the largest exponent it writes.
		"1.79e308",
		"-1.79e308",
		// The smallest nonzero values. 1.38 writes these for "0.5n" and "1e-400".
		"1n",
		"-1n",
		"1e-9",
		"-1e-9",
		// 1.38 keeps this as written.
		"1.5e-8",
		"0",
		// The BinarySI cap, which 1.38 writes for "8Ei".
		"9223372036854775807",
		"-9223372036854775807",
		// 1.38 writes this for "7.99Ei".
		"9211842821808707338240m",
		// The largest DecimalSI suffix with 29 digits.
		"99999999999999999999999999999E",
	}
	for _, s := range unchanged {
		q, err := ParseQuantity(s)
		if err != nil {
			t.Errorf("ParseQuantity(%q) returned error: %v", s, err)
			continue
		}
		if got := q.String(); got != s {
			t.Errorf("ParseQuantity(%q).String() = %q, want %q", s, got, s)
		}
	}

	// Every exponent that 1.38 writes, with the shortest and the longest digit
	// strings. Near the top of the range the 31 nines are larger than
	// math.MaxFloat64, which 1.38 never writes. Those rows check more than
	// 1.38 needs.
	for exponent := -9; exponent <= 308; exponent++ {
		for _, digits := range []string{"1", strings.Repeat("9", 31)} {
			for _, sign := range []string{"", "-"} {
				s := sign + digits + "e" + strconv.Itoa(exponent)
				q, err := ParseQuantity(s)
				if err != nil {
					t.Errorf("ParseQuantity(%q) returned error: %v", s, err)
					continue
				}
				want, _ := new(big.Rat).SetString(s)
				got, _ := new(big.Rat).SetString(q.AsDec().String())
				if got.Cmp(want) != 0 {
					t.Errorf("ParseQuantity(%q) = %s, want the same value", s, q.String())
				}
			}
		}
	}
}
