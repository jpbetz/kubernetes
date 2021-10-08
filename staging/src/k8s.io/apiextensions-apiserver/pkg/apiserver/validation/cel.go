package validation

import (
	"fmt"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"k8s.io/kube-openapi/pkg/validation/spec"
	"k8s.io/kube-openapi/pkg/validation/validate"
	"reflect"
)

func NewCelValueValidator(/*schema *spec.Schema, */celExpr string) (*CelValueValidator, error) {
	// TODO: Replace this prototyping code. Use cel-openapi's model.NewRuleTypes to build a
	// custom type provider and use cel.NewEnv(cel.CustomTypeProvider(ruleTypes)) to allow the
	// schema types to be used in decls.
	env, err := cel.NewEnv(
		cel.Declarations(
			decls.NewVar("minReplicas", decls.Int),
			decls.NewVar("maxReplicas", decls.Int)))
	if err != nil {
		return nil, err
	}
	ast, issues := env.Compile(celExpr)
	if issues != nil {
		return nil, fmt.Errorf("compilation failed: %v", issues)
	}
	// TODO: add type checking information (Decls)
	prog, err := env.Program(ast)
	if err != nil {
		return nil, err
	}
	return &CelValueValidator{Program: prog}, nil
}

type CelValueValidator struct {
	Path string
	Program cel.Program
}

func (c CelValueValidator) SetPath(path string) {
	c.Path = path
}

func (c CelValueValidator) Applies(source interface{}, kind reflect.Kind) bool {
	switch source.(type) {
	case *spec.Schema:
		return true
	}
	return false
}

func (c CelValueValidator) Validate(data interface{}) *validate.Result {
	// TODO: convert from Unstructured to CEL types
	result, _, err := c.Program.Eval(data)
	if err != nil {
		return &validate.Result{ Errors: []error{err}}
	}
	if result.Value() != true {
		return &validate.Result{ Errors: []error{fmt.Errorf("expected true but got %v", result.Value())}}
	}
	return &validate.Result{}
}
