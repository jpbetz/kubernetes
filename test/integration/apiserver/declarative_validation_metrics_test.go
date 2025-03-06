/*
Copyright 2025 The Kubernetes Authors.

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

package apiserver

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	kubeapiservertesting "k8s.io/kubernetes/cmd/kube-apiserver/app/testing"
	"k8s.io/kubernetes/pkg/features"
	"k8s.io/kubernetes/pkg/registry/core/replicationcontroller"
	"k8s.io/kubernetes/test/integration/framework"
)

// TestDeclarativeValidationMismatchMetricWithRC tests that the declarative validation mismatch metric
// is properly collected and exposed by the API server when creating ReplicationControllers
// with various replica configurations.
func TestDeclarativeValidationMismatchMetricWithRC(t *testing.T) {
	testCases := []struct {
		name                  string
		expectMismatch        bool
		rcName                string
		replicas              *int32
		expectValidationError bool
		errorType             string
		// Validator function to override declarative validation behavior
		validator func(ctx context.Context, options sets.Set[string], scheme *runtime.Scheme, obj runtime.Object) field.ErrorList
	}{
		{
			name:                  "valid replicas (1) - no mismatch expected",
			expectMismatch:        false,
			rcName:                "rc-validation-1",
			replicas:              int32Ptr(1),
			expectValidationError: false,
		},
		{
			name:                  "invalid replicas (-1) - no mismatch expected",
			expectMismatch:        false,
			rcName:                "rc-validation-subset",
			replicas:              int32Ptr(-1),
			expectValidationError: true,
			validator: func(ctx context.Context, options sets.Set[string], scheme *runtime.Scheme, obj runtime.Object) field.ErrorList {
				return field.ErrorList{
					field.Invalid(field.NewPath("spec", "replicas"), -1, "must be greater than or equal to 0").WithOrigin("minimum"),
				}
			},
		},
		{
			name:                  "invalid replicas (-1) - mismatch expected, declarative validation missing error (path difference)",
			expectMismatch:        true,
			rcName:                "rc-validation-missing",
			replicas:              int32Ptr(-1),
			expectValidationError: true,
			// Declarative validation is missing an error that's marked as covered in imperative
			validator: func(ctx context.Context, options sets.Set[string], scheme *runtime.Scheme, obj runtime.Object) field.ErrorList {
				return field.ErrorList{
					field.Invalid(field.NewPath("spec", "minReadySeconds"), -1, "must be greater than or equal to 0").WithOrigin("minimum"),
					// Missing the "spec.replicas" invalid (-1) error that should be covered -> mismatch
				}
			},
		},
		{
			name:                  "invalid replicas (-1) - mismatch expected, declarative validation missing error (origin difference)",
			expectMismatch:        true,
			rcName:                "rc-validation-missing",
			replicas:              int32Ptr(-1),
			expectValidationError: true,
			// Declarative validation is missing an error that's marked as covered in imperative
			validator: func(ctx context.Context, options sets.Set[string], scheme *runtime.Scheme, obj runtime.Object) field.ErrorList {
				// Only return one of the two errors that should be covered
				return field.ErrorList{
					field.Invalid(field.NewPath("spec", "replicas"), -1, "must be greater than or equal to 0").WithOrigin("foo"),
					// Missing the "spec.replicas" invalid (-1) error that should be covered -> mismatch
				}
			},
		},
		{
			name:                  "invalid replicas (-1) - mismatch expected, declarative validation has extra error",
			expectMismatch:        true,
			rcName:                "rc-validation-extra",
			replicas:              int32Ptr(-1),
			expectValidationError: true,
			// Declarative validation has an extra error not in imperative
			validator: func(ctx context.Context, options sets.Set[string], scheme *runtime.Scheme, obj runtime.Object) field.ErrorList {
				return field.ErrorList{
					// These match covered errors
					field.Invalid(field.NewPath("spec", "replicas"), -1, "must be greater than or equal to 0").WithOrigin("minimum"),
					field.Invalid(field.NewPath("spec", "selector"), nil, "extra validation error that shouldn't be there").WithOrigin("foo"),
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			originalFunc := replicationcontroller.ValidateDeclarativelyFunc

			if tc.validator != nil {
				replicationcontroller.ValidateDeclarativelyFunc = tc.validator
			}

			defer func() { replicationcontroller.ValidateDeclarativelyFunc = originalFunc }()

			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeclarativeValidation, true)
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeclarativeValidationTakeover, false)

			// Start the test API server
			server := kubeapiservertesting.StartTestServerOrDie(t, nil, framework.DefaultTestServerFlags(), framework.SharedEtcd())
			defer server.TearDownFn()

			// Create a Kubernetes client
			clientset, err := kubernetes.NewForConfig(server.ClientConfig)
			if err != nil {
				t.Fatalf("Error creating clientset: %v", err)
			}

			// Get the initial metric value before creating a ReplicationController
			initialMetricValue, err := getDeclarativeValidationMismatchMetric(t, server.ClientConfig)
			if err != nil {
				t.Fatalf("Error getting initial metric value: %v", err)
			}
			t.Logf("Initial declarative_validation_mismatch_total value: %v", initialMetricValue)

			ns := &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
			}
			_, err = clientset.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
			if err != nil && !apierrors.IsAlreadyExists(err) {
				t.Logf("Namespace creation error (may already exist): %v", err)
			}

			rc := &v1.ReplicationController{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tc.rcName,
					Namespace: "default",
				},
				Spec: v1.ReplicationControllerSpec{
					Replicas: tc.replicas,
					Selector: map[string]string{
						"name": "test-pod",
					},
					Template: &v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"name": "test-pod",
							},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "test-container",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}

			// Create the ReplicationController and check for validation errors
			_, err = clientset.CoreV1().ReplicationControllers("default").Create(context.TODO(), rc, metav1.CreateOptions{})
			if tc.expectValidationError {
				if err == nil {
					t.Errorf("Expected error but got none for ReplicationController %s", tc.rcName)
				} else {
					statusErr, ok := err.(*apierrors.StatusError)
					if !ok {
						t.Errorf("Expected StatusError but got %T: %v", err, err)
					} else if tc.errorType != "" && !strings.Contains(statusErr.Error(), tc.errorType) {
						t.Errorf("Expected error containing %q but got: %v", tc.errorType, statusErr)
					} else {
						t.Logf("Got expected error for ReplicationController %s: %v", tc.rcName, err)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error creating ReplicationController %s: %v", tc.rcName, err)
				} else {
					t.Logf("Successfully created ReplicationController: %s", tc.rcName)
				}
			}

			// Get the updated metric value after attempting to create the ReplicationController
			updatedMetricValue, err := getDeclarativeValidationMismatchMetric(t, server.ClientConfig)
			if err != nil {
				t.Fatalf("Error getting updated metric value: %v", err)
			}
			t.Logf("Updated declarative_validation_mismatch_total value: %v", updatedMetricValue)

			// Check if the metric was incremented
			metricDifference := updatedMetricValue - initialMetricValue
			t.Logf("Metric difference: %v", metricDifference)

			// Assert that the metric was incremented if we expect a mismatch
			if tc.expectMismatch && metricDifference == 0 {
				t.Errorf("Expected declarative_validation_mismatch_total metric to increment but it didn't")
			}

			// Assert that the metric was not incremented if we don't expect a mismatch
			if !tc.expectMismatch && metricDifference != 0 {
				t.Errorf("Unexpected declarative_validation_mismatch_total metric increment: %v", metricDifference)
			}
		})
	}
}

// getDeclarativeValidationMismatchMetric retrieves the current value of the
// declarative_validation_mismatch_total metric from the API server
func getDeclarativeValidationMismatchMetric(t *testing.T, config *restclient.Config) (float64, error) {
	rt, err := restclient.TransportFor(config)
	if err != nil {
		return 0, fmt.Errorf("error creating transport: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, config.Host+"/metrics", nil)
	if err != nil {
		return 0, fmt.Errorf("error creating request: %v", err)
	}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		return 0, fmt.Errorf("error making request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Read the metrics response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("error reading response body: %v", err)
	}
	metricsStr := string(body)

	// Extract the declarative_validation_mismatch_total metric
	re := regexp.MustCompile(`declarative_validation_mismatch_total\s+([0-9.]+)`)
	matches := re.FindStringSubmatch(metricsStr)

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("error parsing metric value: %v", err)
	}

	return value, nil
}
