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
# Test case 1
name: invalid container name
replace:
  "spec.containers[0].name": "invalid.container.name"
expectedErrors:
- type: FieldValueInvalid
  detail: must be a valid DNS label
---
# Test case 2
# ...
```

2. Use the framework in your tests:

```go
func TestPodValidation(t testing.T) {
	// Register any needed schemes
    scheme := runtime.NewScheme()
    corev1.AddToScheme(scheme)
	// Load the test cases
    suite, err := LoadValidationTestSuite("testdata/pod_validations.yaml", scheme)
    if err != nil {
        t.Fatalf("Failed to load test suite: %v", err)
    }
    // Run the tests against the appropriate validation implementation
    suite.RunValidationTests(t, func(obj runtime.Object) field.ErrorList {
      pod := obj.(*corev1.Pod)
      opts := // ...
      return corevalidation.ValidatePodCreate(pod, opts)
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
  - `replace`: Map of field paths to values for simple replacements
  - `jsonPatch`: List of JSON patch operations to apply
  - `applyConfiguration`: Partial object to be applied as a patch using server-side apply semantics
- `expectedErrors`: List of expected validation errors (omit if no errors are expected). If the test
  case performs a replace of a single field value, the field in the expected error can be omitted,
  and the framework will automatically use the field from the replace.
- TODO: Add `expectedDeclarativeErrors` for declarative validation errors. If unset, the 
  expectedErrors will be used for declarative validation.

### Expected Errors

Each expected error must specify:
- `type`: Validation error type (required)
- `field`: Dot-separated path to the field (optional when using a single Replace field)
- `detail`: Optional error message detail to match
- TODO: Add containers/prefix matching options for details?

Valid error types:
- `FieldValueRequired`: A required field is missing
- `FieldValueInvalid`: The field value is invalid
- `FieldValueDuplicate`: The field value is duplicated
- `FieldValueForbidden`: The field value is forbidden
- `FieldValueNotFound`: Referenced value not found
- `FieldValueNotSupported`: The field value is not supported

## Modifying the Base Object

There are three ways to modify the base object in test cases:

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
// Create a new test suite
suite := NewValidationTestSuite(&corev1.Pod{...})
suite.TestCases = []TestCase{
{
  Name: "invalid string field",
  Replace: map[string]interface{}{
    "spec.stringField": "invalid",
  },
  ExpectedErrors: []ExpectedError{
    {Type: "FieldValueInvalid", Detail: "must not be 'invalid'"},
  },
},

// Run the tests against the appropriate validation implementation
suite.RunValidationTests(t, func(obj runtime.Object) field.ErrorList {
  pod := obj.(*corev1.Pod)
  opts := // ...
  return corevalidation.ValidatePodCreate(pod, opts)
})
```

### TODO

- Add update validation support
  - sometimes the baseObject is a reasonable oldObject
  - sometimes we need to do 2 sets of modifications of the base object, one set to prepare the old object
    and another to prepare the new object
- Make opts configuration easy to do from test cases
- Add support for validating declarative validation
  - Allow declarative validation errors to be different from handwritten validation errors
  - Make it easy to know if a test case is covered by declarative validation or not