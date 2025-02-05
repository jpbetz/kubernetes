# Validation Testing Framework

This package provides a framework for writing validation tests using YAML test definitions.

## Usage

1. Create a YAML file containing your test cases: 

```yaml
# Base valid object
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
  namespace: default
spec:
  containers:
    - name: test-container
      image: test-image
---
# Test Case 1
name: invalid container name
description: Container name must be a valid DNS label
modifications:
"spec.containers[0].name": "invalid.container.name"
expectedErrors:
field: "spec.containers[0].name"
type: "FieldValueInvalid"
detail: "must be a valid DNS label"
```

2. Use the framework in your tests:

```go
func TestPodValidation(t testing.T) {
    scheme := runtime.NewScheme()
    corev1.AddToScheme(scheme)
    suite, err := LoadValidationTestSuite("testdata/pod_validation_test.yaml", scheme)
    if err != nil {
        t.Fatalf("Failed to load test suite: %v", err)
    }
    suite.RunValidationTests(t, func(obj runtime.Object) field.ErrorList {      
        pod := obj.(corev1.Pod)
        return validation.ValidatePod(pod)
    })
}
```

## Test Case Format

Each test case in the YAML file can include:

- `name`: Name of the test case
- `description`: Optional description
- `skip`: Optional boolean to skip the test
- `modifications`: Map of field paths to new values
- `expectedErrors`: List of expected validation errors

## Field Paths

Field paths use dot notation with array indices in square brackets:

- `metadata.name`
- `spec.containers[0].name`
- `spec.containers[0].ports[0].containerPort`