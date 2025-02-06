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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	jsonpatch "gopkg.in/evanphx/json-patch.v4"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/apiserver/pkg/validation/testing/patch"
	"k8s.io/client-go/openapi/openapitest"
)

// parseObject converts a YAML document into a runtime.Object
func parseObject(data []byte, scheme *runtime.Scheme) (runtime.Object, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data may not be empty")
	}

	if scheme == nil {
		return nil, fmt.Errorf("scheme cannot be nil")
	}

	// First decode into raw JSON to determine the type
	typeMeta := &runtime.TypeMeta{}
	if err := yaml.Unmarshal(data, &typeMeta); err != nil {
		return nil, fmt.Errorf("failed to parse type information: %v", err)
	}

	if typeMeta.APIVersion == "" {
		return nil, fmt.Errorf("apiVersion is required")
	}
	if typeMeta.Kind == "" {
		return nil, fmt.Errorf("kind is required")
	}

	// Get group and version
	gv, err := schema.ParseGroupVersion(typeMeta.APIVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to parse GroupVersion from %q: %v", typeMeta.APIVersion, err)
	}
	gvk := gv.WithKind(typeMeta.Kind)

	// Create a new object of the correct type
	obj, err := scheme.New(gvk)
	if err != nil {
		return nil, fmt.Errorf("failed to create object of type %v: %v", gvk, err)
	}

	// Decode the full object
	if err := yaml.Unmarshal(data, obj); err != nil {
		return nil, fmt.Errorf("failed to parse object: %v", err)
	}

	return obj, nil
}

// validateErrors checks if actual errors match expected errors
func validateErrors(t testing.TB, expected []ExpectedError, actual field.ErrorList) {
	if t == nil {
		panic("testing.TB cannot be nil")
	}

	// Handle nil cases
	if len(expected) == 0 && len(actual) == 0 {
		return
	}
	if len(expected) != len(actual) {
		t.Errorf("Expected %d validation errors, got %d", len(expected), len(actual))
		t.Errorf("Expected errors: %v", expected)
		t.Errorf("Actual errors: %v", actual)
		return
	}

	// Create maps for more flexible matching
	actualByField := make(map[string]*field.Error)
	for i := range actual {
		actualByField[actual[i].Field] = actual[i]
	}

	// Check each expected error
	for i, exp := range expected {
		// TODO: Fix this to handle multiple errors for the same field.
		act, ok := actualByField[exp.Field]
		if !ok {
			t.Errorf("Error %d: expected error for field %q, but got none", i, exp.Field)
			continue
		}

		// Check error type
		if exp.Type != string(act.Type) {
			t.Errorf("Error %d: expected type %q, got %q for field %q", i, exp.Type, act.Type, exp.Field)
		}

		// Check error detail if specified
		if exp.Detail != "" {
			if !strings.Contains(act.Detail, exp.Detail) && !strings.Contains(exp.Detail, act.Detail) {
				t.Errorf("Error %d: expected detail %q, got %q for field %q", i, exp.Detail, act.Detail, exp.Field)
			}
		}
	}
}

// TestCase represents a single validation test case defined in YAML
type TestCase struct {
	// Name of the test case
	Name string `json:"name"`

	// ApplyConfiguration is the partial object to be applied as a patch
	// Only one of ApplyConfiguration, JSONPatch, or Replace may be set
	ApplyConfiguration map[string]interface{} `json:"applyConfiguration"`

	// JSONPatch is a list of JSON patch operations to apply
	// Only one of ApplyConfiguration, JSONPatch, or Replace may be set
	JSONPatch []map[string]interface{} `json:"jsonPatch"`

	// Replace is a map of JSON paths to values for simple replace operations
	// Only one of ApplyConfiguration, JSONPatch, or Replace may be set
	// This is converted to JSONPatch replace operations internally
	Replace map[string]interface{} `json:"replace"`

	// ExpectedErrors is a list of expected validation errors
	ExpectedErrors []ExpectedError `json:"expectedErrors"`
}

// ExpectedError represents an expected validation error
type ExpectedError struct {
	// Field is the dot-separated path to the field
	Field string `json:"field"`

	// Type is the validation error type (e.g. "FieldValueRequired", "FieldValueInvalid")
	// TODO: We don't need the FieldValue prefix on these error types, remove it.
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

// TestObject is a simple test type that implements runtime.Object
type TestObject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TestSpec `json:"spec,omitempty"`
}

type TestSpec struct {
	StringField string `json:"stringField"`
	IntField    int64  `json:"intField"`
}

func (t *TestObject) DeepCopyObject() runtime.Object {
	return nil // not needed for testing
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

	// TODO: The tcProvider is only used once. This code is needlessly complex.
	// We should just create the type converter once and reuse it, which would
	// require having some type that LoadValidationTestSuite can be called multiple
	// times from the same test suite.
	tcProvider, err := patch.NewStaticTypeConverterProvider(openapitest.NewEmbeddedFileClient())
	if err != nil {
		return nil, fmt.Errorf("failed to create type converter: %v", err)
	}

	typeConverter, err := tcProvider(baseObj.GetObjectKind().GroupVersionKind())
	if err != nil {
		return nil, fmt.Errorf("failed to create type converter: %v", err)
	}

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
			u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(s.BaseObject)
			if err != nil {
				t.Fatalf("Failed to convert base object to unstructured: %v", err)
			}
			var testObj runtime.Object = &unstructured.Unstructured{Object: u}

			// Validate that only one of ApplyConfiguration, JSONPatch, or Replace is set
			// TODO: Move this validation into a separate function.
			setFields := 0
			if tc.ApplyConfiguration != nil {
				setFields++
			}
			if tc.JSONPatch != nil {
				setFields++
			}
			if tc.Replace != nil {
				setFields++
			}
			if setFields > 1 {
				t.Fatalf("Test case %s can only have one of ApplyConfiguration, JSONPatch, or Replace set", tc.Name)
			}

			if tc.ApplyConfiguration != nil {
				// TODO: Split this out into a separate function.
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

			if tc.JSONPatch != nil || tc.Replace != nil {
				// TODO: Split this out into separate functions.
				//       One function to build a JSON Patch from a Replace map.
				//       One function to apply a JSON Patch to an object.
				// Convert test object to JSON
				originalJSON, err := runtime.Encode(unstructured.UnstructuredJSONScheme, testObj)
				if err != nil {
					t.Fatalf("Failed to encode test object to JSON: %v", err)
				}

				var patchJSON []byte
				if tc.JSONPatch != nil {
					// Convert JSONPatch to JSON
					patchJSON, err = json.Marshal(tc.JSONPatch)
					if err != nil {
						t.Fatalf("Failed to marshal JSON patch: %v", err)
					}
				} else {
					// If there's only one Replace entry, use its field as the default for zero-valued ExpectedError.Field
					var defaultField string
					if len(tc.Replace) == 1 {
						for field := range tc.Replace {
							defaultField = field
							// Update any zero-valued ExpectedError.Field
							for i := range tc.ExpectedErrors {
								if tc.ExpectedErrors[i].Field == "" {
									tc.ExpectedErrors[i].Field = defaultField
								}
							}
							break
						}
					}

					// Convert Replace map to JSONPatch operations
					var patchOps []map[string]interface{}
					for field, value := range tc.Replace {
						// Convert Kubernetes field path to JSON pointer
						jsonPath, err := fieldPathToJSONPointer(field)
						if err != nil {
							t.Fatalf("Failed to convert field path %q to JSON pointer: %v", field, err)
						}
						patchOps = append(patchOps, map[string]interface{}{
							"op":    "replace",
							"path":  jsonPath,
							"value": value,
						})
					}
					patchJSON, err = json.Marshal(patchOps)
					if err != nil {
						t.Fatalf("Failed to marshal Replace operations to JSON patch: %v", err)
					}
				}

				// Parse the patch
				patch, err := jsonpatch.DecodePatch(patchJSON)
				if err != nil {
					t.Fatalf("Failed to decode JSON patch: %v", err)
				}

				// Apply the patch
				patchedJSON, err := patch.Apply(originalJSON)
				if err != nil {
					t.Fatalf("Failed to apply JSON patch: %v", err)
				}

				// Decode the patched JSON back into an object
				patchedObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, patchedJSON)
				if err != nil {
					t.Fatalf("Failed to decode patched JSON: %v", err)
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

// NewValidationTestSuite creates a new test suite with the given base object
func NewValidationTestSuite(baseObject runtime.Object) *ValidationTestSuite {
	return &ValidationTestSuite{
		BaseObject:    baseObject,
		TestCases:     []TestCase{},
		TypeConverter: nil, // Will be initialized on first use
	}
}
