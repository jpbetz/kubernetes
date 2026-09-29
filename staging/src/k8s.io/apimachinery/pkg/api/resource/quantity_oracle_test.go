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

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
)

// oracleQuantity is a math/big reimplementation of Quantity's parse rules,
// used to check ParseQuantity's output rather than trust it.
type oracleQuantity struct {
	coefficient, exponent *big.Int
	binary                bool
}

var (
	quantityGrammar                = regexp.MustCompile(`^([+-]?)([0-9]*)(?:\.([0-9]*))?([numkMGTPE]|[KMGTPE]i|[eE][+-]?[0-9]+)?$`)
	quantityDecimalSuffixExponents = map[string]int64{"": 0, "n": -9, "u": -6, "m": -3, "k": 3, "M": 6, "G": 9, "T": 12, "P": 15, "E": 18}
	quantityBinarySuffixShifts     = map[string]uint{"Ki": 10, "Mi": 20, "Gi": 30, "Ti": 40, "Pi": 50, "Ei": 60}
)

func parseOracleQuantity(input string) (oracleQuantity, bool) {
	match := quantityGrammar.FindStringSubmatch(input)
	if match == nil || match[2]+match[3] == "" {
		return oracleQuantity{}, false
	}
	sign, whole, fraction, suffix := match[1], match[2], match[3], match[4]
	coefficient, _ := new(big.Int).SetString(whole+fraction, 10)
	exponent := big.NewInt(-int64(len(fraction)))
	binary := false
	if shift, ok := quantityBinarySuffixShifts[suffix]; ok {
		coefficient.Lsh(coefficient, shift)
		binary = true
	} else if decimal, ok := quantityDecimalSuffixExponents[suffix]; ok {
		exponent.Add(exponent, big.NewInt(decimal))
	} else {
		decimal, _ := new(big.Int).SetString(suffix[1:], 10)
		exponent.Add(exponent, decimal)
	}
	if sign == "-" {
		coefficient.Neg(coefficient)
	}
	return oracleQuantity{coefficient: coefficient, exponent: exponent, binary: binary}, true
}

func (q oracleQuantity) stripped() (coefficient, exponent *big.Int) {
	coefficient, exponent = new(big.Int).Set(q.coefficient), new(big.Int).Set(q.exponent)
	if coefficient.Sign() == 0 {
		return coefficient, exponent
	}
	ten := big.NewInt(10)
	for {
		quotient, remainder := new(big.Int).QuoRem(coefficient, ten, new(big.Int))
		if remainder.Sign() != 0 {
			return coefficient, exponent
		}
		coefficient = quotient
		exponent.Add(exponent, big.NewInt(1))
	}
}

// String matches Quantity.AsDec, not Quantity.String: exact value, no suffix.
func (q oracleQuantity) String() string {
	coefficient, exponent := q.stripped()
	if coefficient.Sign() == 0 {
		return "0"
	}
	return fmt.Sprintf("%se%s", coefficient, exponent)
}

func (q oracleQuantity) InRange() bool {
	coefficient, exponent := q.stripped()
	return coefficient.Sign() == 0 || exponent.Cmp(big.NewInt(math.MaxInt32)) <= 0
}

// Limit rounds away from zero to a multiple of 1n, and caps a
// binary-suffixed value at 2^63-1.
func (q oracleQuantity) Limit() oracleQuantity {
	magnitude, exponent := new(big.Int).Abs(q.coefficient), new(big.Int).Set(q.exponent)
	nano := big.NewInt(-9)
	if magnitude.Sign() != 0 && exponent.Cmp(nano) < 0 {
		shift := new(big.Int).Sub(nano, exponent)
		if shift.Cmp(big.NewInt(int64(len(magnitude.String())))) >= 0 {
			magnitude.SetInt64(1)
		} else if _, remainder := magnitude.QuoRem(magnitude, new(big.Int).Exp(big.NewInt(10), shift, nil), new(big.Int)); remainder.Sign() != 0 {
			magnitude.Add(magnitude, big.NewInt(1))
		}
		exponent = nano
	}
	if q.binary {
		bound := new(big.Int).Exp(big.NewInt(10), new(big.Int).Neg(exponent), nil)
		if magnitude.Cmp(bound.Mul(bound, big.NewInt(math.MaxInt64))) > 0 {
			magnitude, exponent = big.NewInt(math.MaxInt64), big.NewInt(0)
		}
	}
	if q.coefficient.Sign() < 0 {
		magnitude.Neg(magnitude)
	}
	return oracleQuantity{coefficient: magnitude, exponent: exponent, binary: q.binary}
}

func documentedOutcome(input string) (value string, formatted, inRange bool) {
	parsed, formatted := parseOracleQuantity(input)
	if !formatted {
		return "", false, false
	}
	return parsed.Limit().String(), true, parsed.InRange()
}
