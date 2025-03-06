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
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog/v2"

	fldtest "k8s.io/apimachinery/pkg/util/validation/field/testing"
	validationmetrics "k8s.io/component-base/metrics/prometheus/validation"
)

// gatherDeclarativeValidationMismatches compares imperative and declarative validation errors
// and returns detailed information about any mismatches found.  Errors are compared via type, field, and origin
func gatherDeclarativeValidationMismatches(imperativeErrs, declarativeErrs field.ErrorList) []string {
	var mismatchDetails []string

	// short circuit here to minimize allocs for usual case of 0 validation errors
	if len(imperativeErrs) == 0 && len(declarativeErrs) == 0 {
		return mismatchDetails
	}

	matcher := fldtest.ErrorMatcher{}.ByType().ByField().ByOrigin().RequireOriginWhenInvalid()
	matchedDeclarative := make([]bool, len(declarativeErrs))

	// Match each "covered" imperative error to a declarative error.
	for _, iErr := range imperativeErrs {
		if !iErr.CoveredByDeclarative {
			continue
		}

		foundMatch := false

		for j, dErr := range declarativeErrs {
			if !matchedDeclarative[j] && matcher.Matches(iErr, dErr) {
				matchedDeclarative[j] = true
				foundMatch = true
				break
			}
		}

		if !foundMatch {
			mismatchDetails = append(mismatchDetails,
				fmt.Sprintf(
					"Unexpected difference between hand written validation and declarative validation error results, unmatched error(s) found %s. "+
						"This indicates a major bug in the implementation of declarative validation, please disable DeclarativeValidation feature gate to correct this problem",
					matcher.Render(iErr),
				),
			)
		}
	}

	// Any remaining unmatched declarative errors are considered "extra".
	for j, dErr := range declarativeErrs {
		if !matchedDeclarative[j] {
			mismatchDetails = append(mismatchDetails,
				fmt.Sprintf(
					"Unexpected difference between hand written validation and declarative validation error results, extra error(s) found %s. "+
						"This indicates a major bug in the implementation of declarative validation, please disable DeclarativeValidation feature gate to correct this problem",
					matcher.Render(dErr),
				),
			)
		}
	}
	return mismatchDetails
}

// CompareDeclarativeErrorsAndEmitMismatches checks for mismatches between imperative and declarative validation
// and logs + emits metrics when inconsistencies are found
func CompareDeclarativeErrorsAndEmitMismatches(imperativeErrs, declarativeErrs field.ErrorList) {
	mismatchDetails := gatherDeclarativeValidationMismatches(imperativeErrs, declarativeErrs)
	for _, detail := range mismatchDetails {
		// Log information about the mismatch
		klog.Warning(detail)

		// Increment the metric for the mismatch
		validationmetrics.Metrics.EmitDeclarativeValidationMismatchMetric()
	}
}
