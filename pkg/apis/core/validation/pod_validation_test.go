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

package validation

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	validationtesting "k8s.io/apiserver/pkg/validation/testing"
	"k8s.io/kubernetes/pkg/apis/core"
	"k8s.io/kubernetes/pkg/apis/core/install"
)

func TestPodValidationTestSuite(t *testing.T) {
	// Create a scheme with core API types
	scheme := runtime.NewScheme()
	install.Install(scheme)

	// Load and run all test suites from testdata directory
	suite, err := validationtesting.LoadValidationTestSuite("testdata/pod_validation.yaml", scheme)
	if err != nil {
		t.Fatalf("Failed to load test suites: %v", err)
	}

	// Run the validation tests
	suite.RunValidationTests(t, func(obj runtime.Object) field.ErrorList {
		// Convert to internal version if needed
		internalObj, err := scheme.ConvertToVersion(obj, core.SchemeGroupVersion)
		if err != nil {
			t.Fatalf("Failed to convert to internal version: %v", err)
			return nil
		}

		pod, ok := internalObj.(*core.Pod)
		if !ok {
			t.Fatalf("Expected *core.Pod but got: %T", internalObj)
			return nil
		}
		return ValidatePodCreate(pod, PodValidationOptions{ResourceIsPod: true})
	})
}
