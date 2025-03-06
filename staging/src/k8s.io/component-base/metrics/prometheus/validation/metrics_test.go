/*
Copyright 2025 The Kubernetes Authors.

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
	"strings"
	"testing"

	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/component-base/metrics/testutil"
)

var (
	testedMetrics = []string{"declarative_validation_mismatch_total"}
)

func TestCheckValidationMetrics(t *testing.T) {
	defer legacyregistry.Reset()
	defer ResetValidationMetricsInstance()

	testCases := []struct {
		desc    string
		name    string
		stage   string
		enabled bool
		want    string
	}{
		{
			desc: "increment declarative_validation_mismatch_total metric",
			want: `
			# HELP apiserver_validation_declarative_validation_mismatch_total [BETA] Number of times declarative validation results differed from handwritten validation results for core types.
			# TYPE apiserver_validation_declarative_validation_mismatch_total counter
			apiserver_validation_declarative_validation_mismatch_total 1
			`,
		},
	}

	for _, test := range testCases {
		t.Run(test.desc, func(t *testing.T) {
			defer ResetValidationMetricsInstance()
			// Increment the declarative_validation_mismatch_total metric
			Metrics.EmitDeclarativeValidationMismatchMetric()

			if err := testutil.GatherAndCompare(legacyregistry.DefaultGatherer, strings.NewReader(test.want), testedMetrics...); err != nil {
				t.Fatal(err)
			}
		})
	}
}
