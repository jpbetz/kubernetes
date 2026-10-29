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
	"strings"
	"testing"
	"time"
)

// TestParseQuantityLargeExponent logs how long ParseQuantity takes for each
// input. Run one case at a time with a timeout, for example:
//
//	go test ./staging/src/k8s.io/apimachinery/pkg/api/resource -v -timeout 5m \
//	  -run 'TestParseQuantityLargeExponent/^d35_e2147483645$'
func TestParseQuantityLargeExponent(t *testing.T) {
	d35 := "98765432109876543210987654321098765"
	tests := []struct {
		name  string
		input string
	}{
		// The same 35 digits at growing exponents.
		{name: "d35_e1000", input: d35 + "e1000"},
		{name: "d35_e10000000", input: d35 + "e10000000"},
		{name: "d35_e30000000", input: d35 + "e30000000"},
		{name: "d35_e100000000", input: d35 + "e100000000"},

		// The inputs from the kubernetes/kubernetes#143245 test commit.
		{name: "d35_e2147483645", input: d35 + "e2147483645"},
		{name: "neg_d35_e2147483645", input: "-" + d35 + "e2147483645"},
		{name: "d35_e2147483641", input: d35 + "e2147483641"},
		{name: "d35_e2147483642", input: d35 + "e2147483642"},
		{name: "d30_frac_e2147483647", input: "987654321098765432109876543210.5e2147483647"},
		{name: "1_29zeros_e2147483647", input: "1" + strings.Repeat("0", 29) + "e2147483647"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			_, err := ParseQuantity(tt.input)
			t.Logf("ParseQuantity(%q) returned err=%v after %v", tt.input, err, time.Since(start))
		})
	}
}
