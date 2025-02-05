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
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestValidationTestSuite(t *testing.T) {
	// Create a scheme with core/v1
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add core/v1 to scheme: %v", err)
	}

	// Get test files
	testFiles, err := filepath.Glob("testdata/*.yaml")
	if err != nil {
		t.Fatalf("Failed to list test files: %v", err)
	}

	if len(testFiles) == 0 {
		t.Fatal("No test files found in testdata directory")
	}

	// Run each test file
	for _, testFile := range testFiles {
		testName := filepath.Base(testFile)
		t.Run(testName, func(t *testing.T) {
			// Load and run the test suite
			suite, err := LoadValidationTestSuite(testFile, scheme)
			if err != nil {
				t.Fatalf("Failed to load test suite: %v", err)
			}

			// Mock validation function that returns the expected error
			validateFunc := func(obj runtime.Object) field.ErrorList {
				return field.ErrorList{
					&field.Error{
						Type:   field.ErrorTypeInvalid,
						Field:  "spec.containers[0].name",
						Detail: "must be a valid DNS label",
					},
				}
			}

			suite.RunValidationTests(t, validateFunc)
		})
	}
}
