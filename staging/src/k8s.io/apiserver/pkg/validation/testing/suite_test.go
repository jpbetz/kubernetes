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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

type testStruct struct {
	StringField    string
	IntField       int64
	BoolField      bool
	FloatField     float64
	SliceField     []string
	MapField       map[string]string
	NestedStruct   *nestedStruct
	StringSlice    []string
	IntSlice       []int64
	StructSlice    []nestedStruct
	PtrStructSlice []*nestedStruct
}

type nestedStruct struct {
	Name  string
	Value int64
}

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

func TestParseObject(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "TestObject"}, &TestObject{})

	tests := []struct {
		name        string
		yaml        []byte
		wantErr     bool
		errContains string
	}{
		{
			name: "valid object",
			yaml: []byte(`
apiVersion: v1
kind: TestObject
metadata:
  name: test
spec:
  stringField: "test"
  intField: 42
`),
			wantErr: false,
		},
		{
			name: "invalid yaml",
			yaml: []byte(`{
  invalid yaml
}`),
			wantErr:     true,
			errContains: "apiVersion is required",
		},
		{
			name: "invalid group version",
			yaml: []byte(`
apiVersion: invalid/version
kind: TestObject
`),
			wantErr:     true,
			errContains: "failed to create object of type invalid/version",
		},
		{
			name: "unknown type",
			yaml: []byte(`
apiVersion: v1
kind: UnknownType
`),
			wantErr:     true,
			errContains: "failed to create object of type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := parseObject(tt.yaml, scheme)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %v", tt.errContains, err)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if obj == nil {
				t.Error("expected object but got nil")
				return
			}

			if obj.GetObjectKind().GroupVersionKind().Kind != "TestObject" {
				t.Errorf("expected kind TestObject, got %s", obj.GetObjectKind().GroupVersionKind().Kind)
			}
		})
	}
}

func TestValidateErrors(t *testing.T) {
	tests := []struct {
		name     string
		expected []ExpectedError
		actual   field.ErrorList
		wantErr  bool
	}{
		{
			name: "matching errors - exact match",
			expected: []ExpectedError{
				{Field: "spec.field1", Type: "FieldValueRequired"},
				{Field: "spec.field2", Type: "FieldValueInvalid", Detail: "must be positive"},
			},
			actual: field.ErrorList{
				field.Required(field.NewPath("spec", "field1"), "field is required"),
				field.Invalid(field.NewPath("spec", "field2"), -1, "must be positive"),
			},
			wantErr: false,
		},
		{
			name: "matching errors - unsupported value type",
			expected: []ExpectedError{
				{Field: "spec.field1", Type: "Unsupported value"},
			},
			actual: field.ErrorList{
				field.NotSupported(field.NewPath("spec", "field1"), "value", []string{"allowed"}),
			},
			wantErr: false,
		},
		{
			name: "matching errors - detail substring match",
			expected: []ExpectedError{
				{Field: "spec.field1", Type: "FieldValueInvalid", Detail: "must be positive"},
			},
			actual: field.ErrorList{
				field.Invalid(field.NewPath("spec", "field1"), -1, "value must be positive number"),
			},
			wantErr: false,
		},
		{
			name: "matching errors - detail reverse substring match",
			expected: []ExpectedError{
				{Field: "spec.field1", Type: "FieldValueInvalid", Detail: "value must be positive number"},
			},
			actual: field.ErrorList{
				field.Invalid(field.NewPath("spec", "field1"), -1, "must be positive"),
			},
			wantErr: false,
		},
		{
			name: "different number of errors",
			expected: []ExpectedError{
				{Field: "spec.field1", Type: "FieldValueRequired"},
			},
			actual: field.ErrorList{
				field.Required(field.NewPath("spec", "field1"), "field is required"),
				field.Invalid(field.NewPath("spec", "field2"), -1, "must be positive"),
			},
			wantErr: true,
		},
		{
			name: "different field paths",
			expected: []ExpectedError{
				{Field: "spec.field1", Type: "FieldValueRequired"},
			},
			actual: field.ErrorList{
				field.Required(field.NewPath("spec", "field2"), "field is required"),
			},
			wantErr: true,
		},
		{
			name: "different error types",
			expected: []ExpectedError{
				{Field: "spec.field1", Type: "FieldValueRequired"},
			},
			actual: field.ErrorList{
				field.Invalid(field.NewPath("spec", "field1"), nil, "field is invalid"),
			},
			wantErr: true,
		},
		{
			name: "different error details with no substring match",
			expected: []ExpectedError{
				{Field: "spec.field1", Type: "FieldValueInvalid", Detail: "must be positive"},
			},
			actual: field.ErrorList{
				field.Invalid(field.NewPath("spec", "field1"), -1, "must be non-negative"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &errorRecorder{TB: t}
			validateErrors(recorder, tt.expected, tt.actual)

			if tt.wantErr && !recorder.hasErrors {
				t.Error("expected validation errors but got none")
			}
			if !tt.wantErr && recorder.hasErrors {
				t.Error("expected no validation errors but got some")
			}
		})
	}
}

// errorRecorder implements testing.TB and records if any errors were reported
type errorRecorder struct {
	testing.TB
	hasErrors bool
}

func (r *errorRecorder) Errorf(format string, args ...interface{}) {
	r.hasErrors = true
}

func (r *errorRecorder) Error(args ...interface{}) {
	r.hasErrors = true
}

func TestValidationSuite(t *testing.T) {
	// Create a simple runtime scheme for testing
	scheme := runtime.NewScheme()

	// Register Pod type with proper GVK
	podGVK := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	}
	scheme.AddKnownTypeWithName(podGVK, &unstructured.Unstructured{})
	// Test invalid cases
	invalidValidateFunc := func(obj runtime.Object) field.ErrorList {
		return field.ErrorList{
			field.Invalid(field.NewPath("spec", "containers").Index(0).Child("name"), obj, "must be a valid DNS label"),
		}
	}
	invalidSuite, err := LoadValidationTestSuite("testdata/invalid.yaml", scheme)
	if err != nil {
		t.Fatalf("Failed to load invalid test suite: %v", err)
	}
	invalidSuite.RunValidationTests(t, invalidValidateFunc)

	// Test valid cases
	validValidateFunc := func(obj runtime.Object) field.ErrorList {
		return field.ErrorList{}
	}
	validSuite, err := LoadValidationTestSuite("testdata/valid.yaml", scheme)
	if err != nil {
		t.Fatalf("Failed to load valid test suite: %v", err)
	}

	validSuite.RunValidationTests(t, validValidateFunc)
}
