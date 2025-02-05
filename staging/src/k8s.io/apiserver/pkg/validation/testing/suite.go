/*
Copyright 2024 The Kubernetes Authors.

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

package testing

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// TestCase represents a single validation test case defined in YAML
type TestCase struct {
	// Name of the test case
	Name string `json:"name"`

	// Modifications is a map of field paths to their new values
	Modifications map[string]interface{} `json:"modifications"`

	// ExpectedErrors is a list of expected validation errors
	ExpectedErrors []ExpectedError `json:"expectedErrors"`
}

// ExpectedError represents an expected validation error
type ExpectedError struct {
	// Field is the dot-separated path to the field
	Field string `json:"field"`

	// Type is the validation error type (e.g. "FieldValueRequired", "FieldValueInvalid")
	Type string `json:"type"`

	// Detail is the expected error message detail (optional)
	Detail string `json:"detail,omitempty"`
}

// ValidationTestSuite loads and runs validation tests from YAML files
type ValidationTestSuite struct {
	// BaseObject is the valid object that will be modified for tests
	BaseObject runtime.Object

	// TestCases are the individual test cases
	TestCases []TestCase
}

// LoadValidationTestSuite loads a test suite from a YAML file
func LoadValidationTestSuite(path string, scheme *runtime.Scheme) (*ValidationTestSuite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read test file: %v", err)
	}

	// Split the YAML documents
	docs := strings.Split(string(data), "\n---\n")
	if len(docs) < 2 {
		return nil, fmt.Errorf("test file must contain at least 2 YAML documents (base object and test cases)")
	}

	// Parse the base object
	baseObj, err := parseObject([]byte(docs[0]), scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base object: %v", err)
	}

	// Parse the test cases
	var testCases []TestCase
	for i, doc := range docs[1:] {
		var tc TestCase
		if err := yaml.Unmarshal([]byte(doc), &tc); err != nil {
			return nil, fmt.Errorf("failed to parse test case %d: %v", i+1, err)
		}
		testCases = append(testCases, tc)
	}

	return &ValidationTestSuite{
		BaseObject: baseObj,
		TestCases:  testCases,
	}, nil
}

// RunValidationTests runs all test cases in the suite
func (s *ValidationTestSuite) RunValidationTests(t *testing.T, validateFunc func(runtime.Object) field.ErrorList) {
	for _, tc := range s.TestCases {
		t.Run(tc.Name, func(t *testing.T) {
			// Create a copy of the base object
			testObj := s.BaseObject.DeepCopyObject()

			// Apply modifications
			for path, value := range tc.Modifications {
				if err := setNestedField(testObj, path, value); err != nil {
					t.Fatalf("Failed to modify object: %v", err)
				}
			}

			// Run validation
			actualErrors := validateFunc(testObj)

			// Check errors
			validateErrors(t, tc.ExpectedErrors, actualErrors)
		})
	}
}
