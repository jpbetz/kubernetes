# Declarative Validation For Kubernetes

How to get started:

```sh
# 1. Clone this branch

$ cd staging/src/k8s.io/sample-apiserver

$ go test ./pkg/apis/wardle/validation
# All validations should be passing!

# 2. Add a IDL tag to pkg/apis/wardle/v1alpha1/types.go, for example:
# 	// +validations=rule:"size(self) > 20"
#	Reference string `json:"reference,omitempty" protobuf:"bytes,1,opt,name=reference"`

# 3. Update openapi and try validation again:

$ hack/update-openapi.sh
$ go test ./pkg/apis/wardle/validation

--- FAIL: TestDeclarativeValidation (0.01s)
    declarativevalidation_test.go:37: spec.reference: Invalid value: "string": failed rule: size(self) > 20

# Note Update pkg/apis/wardle/validation/testdata/01-flunder.yaml to change the data being validated
```

## Validation tags available

CEL:

- `+validations=rule:'<CEL expression>', message:'<rule failure message>', messageExpression:'<CEL expression to produce a rule failure message>'`
- Validation rules work the same as on CRDs, see: https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#validation-rules

## OpenAPI value validations

- `+format=<some valid format>` - format checkers, see https://github.com/kubernetes/kube-openapi/blob/f5883ff37f0c685e2c0ad657c26c52baba49be6d/pkg/validation/strfmt/format.go#L96
- `+pattern="<regex match pattern>"`
- `+minItems=<int>`, `+maxItems=<int>` - array size checks
- `+minProperties=<int>`, `+maxProperties=<int>` - map size checks
- `+minLength=<int>`, `+maxLength=<int>` - string length checks
- `+minimum=<float>`, `+maximum=<float>`, `+exclusiveMinimum=<bool>`, `+exclusiveMaximum=<bool>` - number range checks

## OpenAPI schema options

- `+optional` - required fields are validated automatically
- `+enum` - tagged enums are validated against the supported values automatically

## Kubernetes extensions

- `+listType: set` - uniqueness of sets is validated automatically
- `+listType: map` and `+listMapKey: <fieldname>` - uniqueness of map keys is validated automatically

TODO: What did I miss?
