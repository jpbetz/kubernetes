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

	"k8s.io/apimachinery/pkg/util/validation/field"
)

// TestCheckDeclarativeValidationMismatches tests all scenarios for
// the checkDeclarativeValidationMismatches function
func TestCheckDeclarativeValidationMismatches(t *testing.T) {
	replicasPath := field.NewPath("spec").Child("replicas")
	minReadySecondsPath := field.NewPath("spec").Child("minReadySeconds")
	selectorPath := field.NewPath("spec").Child("selector")

	errA := field.Invalid(replicasPath, nil, "regular error A")
	errB := field.Invalid(minReadySecondsPath, -1, "covered error B").WithOrigin("minimum")
	coveredErrB := field.Invalid(minReadySecondsPath, -1, "covered error B").WithOrigin("minimum")
	coveredErrB.CoveredByDeclarative = true
	errC := field.Invalid(replicasPath, nil, "covered error C").WithOrigin("minimum")
	coveredErrC := field.Invalid(replicasPath, nil, "covered error C").WithOrigin("minimum")
	coveredErrC.CoveredByDeclarative = true
	errCWithDiffOrigin := field.Invalid(replicasPath, nil, "covered error C").WithOrigin("maximum")
	errD := field.Invalid(selectorPath, nil, "regular error D")

	tests := []struct {
		name                    string
		imperativeErrors        field.ErrorList
		declarativeErrors       field.ErrorList
		expectMismatches        bool
		expectDetailsContaining []string
	}{
		{
			name:                    "Declarative and imperative return 0 errors - no mismatch",
			imperativeErrors:        field.ErrorList{},
			declarativeErrors:       field.ErrorList{},
			expectMismatches:        false,
			expectDetailsContaining: []string{},
		},
		{
			name: "Declarative returns multiple errors with different origins, errors match - no mismatch",
			imperativeErrors: field.ErrorList{
				errA,
				coveredErrB,
				coveredErrC,
				errD,
			},
			declarativeErrors: field.ErrorList{
				errB,
				errC,
			},
			expectMismatches:        false,
			expectDetailsContaining: []string{},
		},
		{
			name: "Declarative returns multiple errors with different origins, errors don't match - mismatch case",
			imperativeErrors: field.ErrorList{
				errA,
				coveredErrB,
				coveredErrC,
			},
			declarativeErrors: field.ErrorList{
				errB,
				errCWithDiffOrigin,
			},
			expectMismatches: true,
			expectDetailsContaining: []string{
				"Unexpected difference between hand written validation and declarative validation error results",
				"unmatched error(s) found",
				"extra error(s) found",
				"replicas",
			},
		},
		{
			name: "Declarative and imperative return exactly 1 error, errors match - no mismatch",
			imperativeErrors: field.ErrorList{
				coveredErrB,
			},
			declarativeErrors: field.ErrorList{
				errB,
			},
			expectMismatches:        false,
			expectDetailsContaining: []string{},
		},
		{
			name: "Declarative and imperative exactly 1 error, errors don't match - mismatch",
			imperativeErrors: field.ErrorList{
				coveredErrB,
			},
			declarativeErrors: field.ErrorList{
				errC,
			},
			expectMismatches: true,
			expectDetailsContaining: []string{
				"Unexpected difference between hand written validation and declarative validation error results",
				"unmatched error(s) found",
				"minReadySeconds",
				"extra error(s) found",
				"replicas",
			},
		},
		{
			name: "Declarative returns 0 errors, imperative returns 1 covered error - mismatch",
			imperativeErrors: field.ErrorList{
				coveredErrB,
			},
			declarativeErrors: field.ErrorList{},
			expectMismatches:  true,
			expectDetailsContaining: []string{
				"Unexpected difference between hand written validation and declarative validation error results",
				"unmatched error(s) found",
				"minReadySeconds",
			},
		},
		{
			name: "Declarative returns 0 errors, imperative returns 1 uncovered error - no mismatch",
			imperativeErrors: field.ErrorList{
				errB,
			},
			declarativeErrors:       field.ErrorList{},
			expectMismatches:        false,
			expectDetailsContaining: []string{},
		},
		{
			name:             "Declarative returns 1 error, imperative returns 0 error - mismatch",
			imperativeErrors: field.ErrorList{},
			declarativeErrors: field.ErrorList{
				errB,
			},
			expectMismatches: true,
			expectDetailsContaining: []string{
				"Unexpected difference between hand written validation and declarative validation error results",
				"extra error(s) found",
				"minReadySeconds",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			details := gatherDeclarativeValidationMismatches(tt.imperativeErrors, tt.declarativeErrors)
			// Check if mismatches were found if expected
			if tt.expectMismatches && len(details) == 0 {
				t.Errorf("Expected mismatches but got none")
			}
			// Check if details contain expected text
			detailsStr := strings.Join(details, " ")
			for _, expectedContent := range tt.expectDetailsContaining {
				if !strings.Contains(detailsStr, expectedContent) {
					t.Errorf("Expected details to contain: %q, but they didn't.\nDetails were:\n%s",
						expectedContent, strings.Join(details, "\n"))
				}
			}
			// If we don't expect any details, make sure none provided
			if len(tt.expectDetailsContaining) == 0 && len(details) > 0 {
				t.Errorf("Expected no details, but got %d details: %v", len(details), details)
			}
		})
	}
}

// Helper function to create CoveredByDeclarative errors
func createCoveredError(path *field.Path, value interface{}, detail string) *field.Error {
	err := field.Invalid(path, value, detail)
	err.CoveredByDeclarative = true
	return err
}
