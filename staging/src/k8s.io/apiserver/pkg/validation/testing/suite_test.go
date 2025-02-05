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

func TestValidationSuite(t *testing.T) {
	scheme := runtime.NewScheme()
	podGVK := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	}
	scheme.AddKnownTypeWithName(podGVK, &unstructured.Unstructured{})

	// Mock the validation function
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

	// Mock the validation function
	validValidateFunc := func(obj runtime.Object) field.ErrorList {
		return nil
	}
	validSuite, err := LoadValidationTestSuite("testdata/valid.yaml", scheme)
	if err != nil {
		t.Fatalf("Failed to load valid test suite: %v", err)
	}
	validSuite.RunValidationTests(t, validValidateFunc)
}

func TestValidationSuiteWithDataLiterals(t *testing.T) {
	suite := NewValidationTestSuite(&TestObject{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "TestObject",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: TestSpec{
			StringField: "test",
			IntField:    42,
		},
	})
	suite.TestCases = []TestCase{
		{
			Name: "invalid string field",
			Replace: map[string]interface{}{
				"spec.stringField": "invalid",
			},
			ExpectedErrors: []ExpectedError{
				{Field: "spec.stringField", Type: "FieldValueInvalid", Detail: "must not be 'invalid'"},
			},
		},
	}

	// Mock the validation function
	invalidValidateFunc := func(obj runtime.Object) field.ErrorList {
		return field.ErrorList{
			field.Invalid(field.NewPath("spec", "stringField"), obj, "must not be 'invalid'"),
		}
	}
	suite.RunValidationTests(t, invalidValidateFunc)
}
