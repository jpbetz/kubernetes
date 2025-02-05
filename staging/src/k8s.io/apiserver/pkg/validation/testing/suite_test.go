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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestValidationSuite(t *testing.T) {
	// Create a simple runtime scheme for testing
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	}, &unstructured.Unstructured{})

	// Load the test suite from the YAML file
	suite, err := LoadValidationTestSuite("testdata/invalid_container_name.yaml", scheme)
	if err != nil {
		t.Fatalf("Failed to load test suite: %v", err)
	}

	// Mock validation function that checks container names
	validateFunc := func(obj runtime.Object) field.ErrorList {
		return field.ErrorList{
			{
				Type:   field.ErrorTypeInvalid,
				Field:  "spec.containers[0].name",
				Detail: "must be a valid DNS label",
			},
		}
	}

	// Run the validation tests
	suite.RunValidationTests(t, validateFunc)
}
