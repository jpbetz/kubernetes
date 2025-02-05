# Validation Testing Framework

This package provides a framework for writing validation tests using YAML test definitions.

## Usage

1. Create a YAML file containing your test cases: 

```yaml
# Base valid object (must be first document)
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
replace:
  "/spec/containers/0/name": "invalid.container.name"
expectedErrors:
- field: spec.containers[0].name
  type: FieldValueInvalid
  detail: must be a valid DNS label  # detail is optional
---
# Test Case 2
name: valid container name
replace:
  "/spec/containers/0/name": "valid-container-name"
expectedErrors: []  # no errors expected
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

## Test File Structure

The test file must follow this structure:
1. The first YAML document must be the base valid object that will be modified in test cases
2. Subsequent documents are individual test cases
3. Each test case must have a unique name
4. The base object must be registered in the provided runtime.Scheme

## Test Case Format

Each test case in the YAML file can include:

- `name`: Name of the test case (required)
- One of the following modification methods (required):
  - `replace`: Map of JSON paths to values for simple replacements
  - `jsonPatch`: List of JSON patch operations to apply
  - `applyConfiguration`: Partial object to be applied as a patch using server-side apply semantics
- `expectedErrors`: List of expected validation errors (required, use empty array for no errors)

### Expected Errors

Each expected error must specify:
- `field`: Dot-separated path to the field (required)
- `type`: Validation error type (required)
- `detail`: Optional error message detail to match

Valid error types:
- `FieldValueRequired`: A required field is missing
- `FieldValueInvalid`: The field value is invalid
- `FieldValueDuplicate`: The field value is duplicated
- `FieldValueForbidden`: The field value is forbidden
- `FieldValueNotFound`: Referenced value not found
- `FieldValueNotSupported`: The field value is not supported

## Modifying the Base Object

There are three ways to modify the base object in test cases. Only one method can be used per test case:

### 1. Replace

Replace provides a simpler syntax for straightforward field replacements. It uses Kubernetes field path notation (e.g. spec.containers[0].name):

```yaml
replace:
  "spec.containers[0].name": "new-name"
  "spec.containers[0].resources.limits.memory": "256Mi"
```

### 2. JSON Patch

JSON patch operations provide fine-grained control over modifications using standard JSON patch syntax:

```yaml
jsonPatch:
- { "op": "replace", "path": "/spec/containers/0/name", "value": "new-name" }
- { "op": "add", "path": "/spec/containers/0/resources/limits/memory", "value": "256Mi" }
- { "op": "remove", "path": "/spec/containers/0/resources/limits/cpu" }
```

### 3. Apply Configuration

Apply configurations use server-side apply semantics to modify the base object. This method requires a TypeConverter which is automatically configured based on the object's GroupVersionKind:

```yaml
applyConfiguration:
  apiVersion: v1
  kind: Pod
  spec:
    containers:
    - name: "new-name"
      resources:
        limits:
          memory: "256Mi"
```

## Field Paths

Field paths use different formats depending on the context:

For `expectedErrors` and `replace`:
- Use dot notation with array indices in square brackets
- Examples:
  - `metadata.name`
  - `spec.containers[0].name`
  - `spec.containers[0].ports[0].containerPort`

For `jsonPatch`:
- Use JSON pointer syntax with forward slashes
- Examples:
  - `/metadata/name`
  - `/spec/containers/0/name`
  - `/spec/containers/0/ports/0/containerPort`

## Error Validation

The framework performs exact matching of validation errors:
1. The number of errors must match exactly
2. Each error's field path must match exactly
3. Error types must match exactly (except "Unsupported value" which matches any type)
4. If a detail is specified, it must be contained within the actual error message or vice versa

## Programmatic Test Case Construction

In addition to YAML-based test cases, you can construct test cases programmatically using Go data literals. This approach provides better type safety and IDE support.

### Example

```go
// Create a base test object
baseObject := &TestObject{
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
}

// Create a new test suite
suite := NewValidationTestSuite(baseObject)

// Add test cases using the fluent builder interface
suite.AddTestCase("invalid string field").
    WithReplace(map[string]interface{}{
        "/spec/stringField": 123, // wrong type
    }).
    ExpectError("spec.stringField", "FieldValueInvalid", "must be a string")

suite.AddTestCase("valid string field").
    WithReplace(map[string]interface{}{
        "/spec/stringField": "valid-value",
    }).
    ExpectNoErrors()

// Run the tests with a validation function
suite.RunValidationTests(t, validateFunc)
```

### Builder Methods

The test case builder provides a fluent interface with the following methods:

- `WithReplace(map[string]interface{})`: Add field replacements
- `WithJSONPatch([]map[string]interface{})`: Add JSON patch operations
- `WithApplyConfiguration(map[string]interface{})`: Add apply configuration
- `ExpectError(field, errType, detail string)`: Add an expected validation error
- `ExpectNoErrors()`: Indicate that no validation errors are expected

Each method returns the builder, allowing method chaining for a concise and readable test definition.

### Choosing Between YAML and Programmatic Approaches

- Use YAML-based tests when:
  - Test cases are primarily data-driven
  - Tests need to be easily readable by non-developers
  - Test data needs to be maintained separately from code

- Use programmatic tests when:
  - Test cases involve complex logic or transformations
  - Type safety and IDE support are important
  - Tests are tightly coupled with the code being tested