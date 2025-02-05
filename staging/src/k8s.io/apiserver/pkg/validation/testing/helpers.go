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
	"reflect"
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
	// First decode into raw JSON to determine the type
	typeMeta := &runtime.TypeMeta{}
	if err := yaml.Unmarshal(data, &typeMeta); err != nil {
		return nil, fmt.Errorf("failed to parse type information: %v", err)
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

// setNestedField sets a value in an object using a field path
func setNestedField(obj interface{}, path string, value interface{}) error {
	fields := strings.Split(path, ".")
	current := reflect.ValueOf(obj)

	// Dereference pointer if needed
	if current.Kind() == reflect.Ptr {
		current = current.Elem()
	}

	for i, field := range fields {
		// Handle array indexing
		if idx := strings.Index(field, "["); idx != -1 {
			arrayField := field[:idx]
			indexStr := field[idx+1 : len(field)-1]
			index, err := strconv.Atoi(indexStr)
			if err != nil {
				return fmt.Errorf("invalid array index in path %s: %v", path, err)
			}

			// Get the field by name
			current = current.FieldByName(strings.Title(arrayField))
			if !current.IsValid() {
				return fmt.Errorf("field %s not found", arrayField)
			}

			if current.Kind() != reflect.Slice {
				return fmt.Errorf("field %s is not a slice", arrayField)
			}

			// Extend slice if needed
			for current.Len() <= index {
				current = reflect.Append(current, reflect.Zero(current.Type().Elem()))
			}

			current = current.Index(index)
			continue
		}

		// Handle regular fields
		current = current.FieldByName(strings.Title(field))
		if !current.IsValid() {
			return fmt.Errorf("field %s not found", field)
		}

		// If this is the last field, set the value
		if i == len(fields)-1 {
			if !current.CanSet() {
				return fmt.Errorf("field %s cannot be set", field)
			}

			val := reflect.ValueOf(value)
			if val.Type().ConvertibleTo(current.Type()) {
				current.Set(val.Convert(current.Type()))
			} else {
				return fmt.Errorf("cannot convert value of type %v to field type %v", val.Type(), current.Type())
			}
		}
	}

	return nil
}

// validateErrors checks if actual errors match expected errors
func validateErrors(t *testing.T, expected []ExpectedError, actual field.ErrorList) {
	if len(expected) != len(actual) {
		t.Errorf("Expected %d validation errors, got %d", len(expected), len(actual))
		t.Errorf("Expected errors: %v", expected)
		t.Errorf("Actual errors: %v", actual)
		return
	}

	for i, exp := range expected {
		act := actual[i]
		if exp.Field != act.Field {
			t.Errorf("Error %d: expected field %q, got %q", i, exp.Field, act.Field)
		}
		if exp.Type != string(act.Type) {
			t.Errorf("Error %d: expected type %q, got %q", i, exp.Type, act.Type)
		}
		if exp.Detail != "" && !strings.Contains(act.Detail, exp.Detail) {
			t.Errorf("Error %d: expected detail %q, got %q", i, exp.Detail, act.Detail)
		}
	}
}
