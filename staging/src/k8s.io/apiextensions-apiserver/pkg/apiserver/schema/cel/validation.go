/*
Copyright 2019 The Kubernetes Authors.

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

package cel

import (
	"fmt"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter"
	"k8s.io/apiextensions-apiserver/third_party/forked/celopenapi/model"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
)

// Validator provides x-kubernetes-validations validation. It compiles CEL programs when instantiated.
type Validator struct {
	Items      *Validator
	Properties map[string]Validator

	AdditionalProperties *Validator

	// Program compilation is pre-checked at CRD creation/update time, so we don't expect compilation to fail here,
	// and it is an internal bug if compilation does fail.
	// But if somehow we get any compilation errors, we track them and then surface them as part of validation.
	compiledRules CompilationResults
}

// NewValidator returns compiles all the CEL programs defined in x-kubernetes-validations extensions
// of the Structural schema and returns a custom resource validator.
func NewValidator(s *schema.Structural) Validator {
	compiledRules := Compile(s)
	result := Validator{compiledRules: compiledRules}
	if s.Items != nil {
		compiledItem := NewValidator(s.Items)
		result.Items = &compiledItem
	}
	if len(s.Properties) > 0 {
		result.Properties = make(map[string]Validator, len(s.Properties))
		for k, prop := range s.Properties {
			compiledProp := NewValidator(&prop)
			result.Properties[k] = compiledProp
		}
	}
	if s.AdditionalProperties != nil && s.AdditionalProperties.Structural != nil {
		compiledProp := NewValidator(s.AdditionalProperties.Structural)
		result.AdditionalProperties = &compiledProp
	}

	return result
}

// Validate validates all x-kubernetes-validations rules in Validator against obj and returns any errors.
func (s *Validator) Validate(fldPath *field.Path, sts *schema.Structural, obj interface{}) field.ErrorList {
	if s == nil || obj == nil {
		return nil
	}

	errs := s.validateExpressions(fldPath, sts, obj)
	switch obj := obj.(type) {
	case []interface{}:
		return append(errs, s.validateArray(fldPath, sts, obj)...)
	case map[string]interface{}:
		return append(errs, s.validateMap(fldPath, sts, obj)...)
	}
	return errs
}

func (s *Validator) validateExpressions(fldPath *field.Path, sts *schema.Structural, obj interface{}) (errs field.ErrorList) {
	bindings := map[string]interface{}{
		ScopedVarName: func() ref.Val { return UnstructuredToVal(obj, sts) },
	}
	if m, ok := obj.(map[string]interface{}); sts.Type == "object" && ok {
		for k, v := range m {
			propSchema := sts.Properties[k]
			val := v
			if !(model.IsRootReserved(k) || k == ScopedVarName) {
				bindings[model.Escape(k)] = func() ref.Val { return UnstructuredToVal(val, &propSchema) }
			}
		}
	}
	activation, err := interpreter.NewActivation(bindings)
	if err != nil {
		errs = append(errs, field.Invalid(fldPath, obj, fmt.Sprintf("failed to bind variables to due to error %v", err.Error())))
		return errs
	}
	if s.compiledRules.Error != nil {
		errs = append(errs, field.Invalid(fldPath, obj, fmt.Sprintf("failed to compile rules due to error %v", err)))
		return errs
	}
	for _, compiled := range s.compiledRules.Results {
		rule := compiled.Rule
		if compiled.Errors != nil {
			for _, err := range compiled.Errors {
				errs = append(errs, field.Invalid(fldPath, obj, fmt.Sprintf("failed to compile rule '%s' due to error %v", rule.Rule, err)))
			}
			continue
		}
		evalResult, _, err := compiled.Program.Eval(activation)
		if err != nil {
			errs = append(errs, field.Invalid(fldPath, obj, fmt.Sprintf("failed to execute rule '%s' due to error %v", rule.Rule, err)))
			continue
		}
		if evalResult.Value() != true {
			if len(rule.Message) != 0 {
				errs = append(errs, field.Invalid(fldPath, obj, rule.Message))
			} else {
				errs = append(errs, field.Invalid(fldPath, obj, fmt.Sprintf("failed rule '%s'", rule.Rule)))
			}
		}
	}
	return errs
}

func (s *Validator) validateMap(fldPath *field.Path, sts *schema.Structural, obj map[string]interface{}) (errs field.ErrorList) {
	if s == nil || obj == nil {
		return nil
	}

	if s.AdditionalProperties != nil && sts.AdditionalProperties != nil && sts.AdditionalProperties.Structural != nil {
		for k, v := range obj {
			errs = append(errs, s.AdditionalProperties.Validate(fldPath.Key(k), sts.AdditionalProperties.Structural, v)...)
		}
	}
	if s.Properties != nil && sts.Properties != nil {
		for k, v := range obj {
			stsProp, stsOk := sts.Properties[k]
			sub, ok := s.Properties[k]
			if ok && stsOk {
				errs = append(errs, sub.Validate(fldPath.Child(k), &stsProp, v)...)
			}
		}
	}

	return errs
}

func (s *Validator) validateArray(fldPath *field.Path, sts *schema.Structural, obj []interface{}) field.ErrorList {
	var errs field.ErrorList

	if s.Items != nil && sts.Items != nil {
		for i := range obj {
			errs = append(errs, s.Items.Validate(fldPath.Index(i), sts.Items, obj[i])...)
		}
	}

	return errs
}
