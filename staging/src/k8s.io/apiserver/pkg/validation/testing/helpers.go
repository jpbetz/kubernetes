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
	"strconv"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// parseObject converts a YAML document into a runtime.Object
func parseObject(data []byte, scheme *runtime.Scheme) (runtime.Object, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
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
	if expected == nil && actual != nil {
		t.Errorf("Expected no validation errors, got %d", len(actual))
		t.Errorf("Actual errors: %v", actual)
		return
	}
	if expected != nil && actual == nil {
		t.Errorf("Expected %d validation errors, got none", len(expected))
		t.Errorf("Expected errors: %v", expected)
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
		act, ok := actualByField[exp.Field]
		if !ok {
			t.Errorf("Error %d: expected error for field %q, but got none", i, exp.Field)
			continue
		}

		// Check error type
		if exp.Type != string(act.Type) && exp.Type != "Unsupported value" {
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

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// validateField checks if a field path is valid
func validateField(field string) error {
	if field == "" {
		return fmt.Errorf("field path cannot be empty")
	}

	parts := strings.Split(field, ".")
	for i, part := range parts {
		if part == "" {
			return fmt.Errorf("field path component at index %d cannot be empty", i)
		}

		// Check for array index notation [n]
		if strings.Contains(part, "[") {
			if !strings.HasSuffix(part, "]") {
				return fmt.Errorf("invalid array index notation in field path component %q", part)
			}
			indexStr := strings.TrimSuffix(strings.TrimPrefix(part, "["), "]")
			if _, err := strconv.Atoi(indexStr); err != nil {
				return fmt.Errorf("invalid array index %q in field path component %q", indexStr, part)
			}
		}
	}

	return nil
}

// validateErrorType checks if an error type is valid
func validateErrorType(errType string) error {
	validTypes := map[string]bool{
		"FieldValueRequired":     true,
		"FieldValueInvalid":      true,
		"FieldValueDuplicate":    true,
		"FieldValueForbidden":    true,
		"FieldValueNotFound":     true,
		"FieldValueNotSupported": true,
		"Unsupported value":      true,
	}

	if errType == "" {
		return fmt.Errorf("error type cannot be empty")
	}

	if !validTypes[errType] {
		return fmt.Errorf("invalid error type %q", errType)
	}

	return nil
}
