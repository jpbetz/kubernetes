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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/apiserver/pkg/admission/plugin/policy/mutating/patch"
	openapitest "k8s.io/client-go/openapi/openapitest"
)

// TestCase represents a single validation test case defined in YAML
type TestCase struct {
	// Name of the test case
	Name string `json:"name"`

	// ApplyConfiguration is the partial object to be applied as a patch
	ApplyConfiguration map[string]interface{} `json:"applyConfiguration"`

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

	// TypeConverter is used for structured merge diff operations
	TypeConverter managedfields.TypeConverter
}

// MockAPIResource is a minimal API resource type for testing
type TestResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
}

func (r *TestResource) DeepCopyObject() runtime.Object {
	return &TestResource{
		TypeMeta:   r.TypeMeta,
		ObjectMeta: r.ObjectMeta,
	}
}

// LoadValidationTestSuite loads a test suite from a YAML file
func LoadValidationTestSuite(path string, scheme *runtime.Scheme) (*ValidationTestSuite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read test file: %v", err)
	}

	// Use YAML reader to properly split documents
	reader := yaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))

	// Read first document (base object)
	baseDoc, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read base object: %v", err)
	}

	baseObj, err := parseObject(baseDoc, scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base object: %v", err)
	}

	// Create type converter
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tcManager := patch.NewTypeConverterManager(nil, openapitest.NewEmbeddedFileClient())
	go tcManager.Run(ctx)

	err = wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, time.Second, true, func(context.Context) (done bool, err error) {
		converter := tcManager.GetTypeConverter(baseObj.GetObjectKind().GroupVersionKind())
		return converter != nil, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create type converter: %v", err)
	}
	typeConverter := tcManager.GetTypeConverter(baseObj.GetObjectKind().GroupVersionKind())

	// Read remaining documents as test cases
	var testCases []TestCase
	for {
		doc, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read test case: %v", err)
		}

		var tc TestCase
		if err := yaml.Unmarshal(doc, &tc); err != nil {
			return nil, fmt.Errorf("failed to parse test case: %v", err)
		}

		testCases = append(testCases, tc)
	}

	if len(testCases) == 0 {
		return nil, fmt.Errorf("test file must contain at least one test case")
	}

	return &ValidationTestSuite{
		BaseObject:    baseObj,
		TestCases:     testCases,
		TypeConverter: typeConverter,
	}, nil
}

// RunValidationTests runs all test cases in the suite
func (s *ValidationTestSuite) RunValidationTests(t *testing.T, validateFunc func(runtime.Object) field.ErrorList) {
	for _, tc := range s.TestCases {
		t.Run(tc.Name, func(t *testing.T) {
			// Create a copy of the base object
			testObj := s.BaseObject.DeepCopyObject()

			if tc.ApplyConfiguration != nil {
				applyConfig := &unstructured.Unstructured{Object: tc.ApplyConfiguration}
				accessor, err := meta.TypeAccessor(applyConfig)
				if err != nil {
					t.Fatalf("Failed to get type accessor: %v", err)
				}

				// stamp the apply configuration with the base object's group version kind
				accessor.SetAPIVersion(s.BaseObject.GetObjectKind().GroupVersionKind().GroupVersion().String())
				accessor.SetKind(s.BaseObject.GetObjectKind().GroupVersionKind().Kind)

				// Apply patch using structured merge diff
				patchedObj, err := patch.ApplyStructuredMergeDiff(s.TypeConverter, testObj, applyConfig)
				if err != nil {
					t.Fatalf("Failed to apply patch: %v", err)
				}

				testObj = patchedObj
			}

			// Run validation
			actualErrors := validateFunc(testObj)

			// Check errors
			validateErrors(t, tc.ExpectedErrors, actualErrors)
		})
	}
}
