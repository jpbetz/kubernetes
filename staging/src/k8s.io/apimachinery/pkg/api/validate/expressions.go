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

package validate

import (
	"context"
	"fmt"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/interpreter"
	"k8s.io/apimachinery/pkg/api/operation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/util/version"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	valuereflect "k8s.io/apiserver/pkg/cel/common/reflect"
	"k8s.io/apiserver/pkg/cel/environment"
	"math"
)

func NewRule(expression, messageExpression, message, errorType string) *CompiledRule {
	result := &CompiledRule{
		expression: Compile(expression, cel.BoolType),
		message:    message,
		errorType:  field.ErrorType(errorType),
	}
	if len(messageExpression) > 0 {
		result.messageExpression = Compile(messageExpression, cel.StringType)
	}
	return result
}

func Compile(expression string, outputType *types.Type) *CompiledExpression {
	result := &CompiledExpression{}
	env := baseEnv.StoredExpressionsEnv()
	// Use DynType. No point slowing down compilation at runtime for CEL expressions that are baked into apiserver
	// binaries.
	env, err := env.Extend(
		cel.Variable(ScopedVarName, cel.DynType),
		cel.Variable(OldScopedVarName, cel.DynType),
		cel.Variable(SubresourcesVarName, cel.ListType(cel.StringType)),
		cel.Variable(OptionsVarName, cel.ListType(cel.StringType)),
	)
	if err != nil {
		result.err = err
		return result
	}
	ast, issues := env.Compile(expression)
	if issues != nil {
		result.issues = issues
		return result
	}
	if ast.OutputType() != outputType {
		result.err = fmt.Errorf("must evaluate to %s, but got: %v for %s", outputType.String(), ast.OutputType(), expression)
		return result
	}

	checkedExpr, err := cel.AstToCheckedExpr(ast)
	if err != nil {
		// should be impossible since env.Compile returned no issues
		result.err = fmt.Errorf("unexpected compilation error: %w", err)
		return result
	}
	for _, ref := range checkedExpr.ReferenceMap {
		if ref.Name == OldScopedVarName {
			result.usesOldSelf = true
			break
		}
	}

	prog, err := env.Program(ast, cel.InterruptCheckFrequency(celconfig.CheckFrequency))

	result.prog = prog
	result.issues = issues
	return result
}

func Expression[T any](ctx context.Context, op operation.Operation, fldPath *field.Path, value, oldValue *T, rule *CompiledRule) field.ErrorList {
	errs := rule.Check(fldPath)
	if len(errs) > 0 {
		return errs
	}

	self := valuereflect.TypedToVal(value)
	var oldSelf any
	if op.Type == operation.Update {
		oldSelf = valuereflect.TypedToVal(oldValue)
	}
	// TODO: Should we skip create validation for expressions that use oldSelf like we do for CRDs?
	a := &activation{
		self:         self,
		oldSelf:      oldSelf,
		hasOldSelf:   rule.expression.usesOldSelf,
		subresources: op.Request.Subresources,
		options:      op.Options,
	}

	evalResult, _, err := rule.expression.prog.ContextEval(ctx, a)
	if err != nil {
		return field.ErrorList{field.InternalError(fldPath, fmt.Errorf("error evaluating expression: %w", err))}
	}

	if evalResult != types.True {
		message := rule.message
		if rule.messageExpression != nil {
			messageEvalResult, _, err := rule.messageExpression.prog.ContextEval(ctx, a)
			if err != nil {
				return field.ErrorList{field.InternalError(fldPath, fmt.Errorf("error evaluating messageExpression: %w", err))}
			}
			message = messageEvalResult.Value().(string)
		}

		errorType := rule.errorType
		if len(errorType) == 0 {
			errorType = field.ErrorTypeInvalid
		}
		return field.ErrorList{&field.Error{Type: errorType, Detail: message, BadValue: value, Field: fldPath.String()}}
	}
	return nil
}

// Declarative Validation has access to all CEL libraries and options.
var baseEnv = environment.MustBaseEnvSet(version.MajorMinor(math.MaxInt, math.MaxInt), true)

type CompiledRule struct {
	expression        *CompiledExpression
	messageExpression *CompiledExpression
	message           string
	errorType         field.ErrorType
}

func (cr *CompiledRule) Check(fldPath *field.Path) field.ErrorList {
	return append(cr.expression.Check(fldPath), cr.messageExpression.Check(fldPath)...)
}

type CompiledExpression struct {
	name       string
	expression string
	prog       cel.Program
	err        error
	issues     *cel.Issues

	usesOldSelf      bool
	usesSubresources bool
	usesOptions      bool
}

func (ce *CompiledExpression) Check(fldPath *field.Path) field.ErrorList {
	if ce.err != nil {
		return field.ErrorList{field.InternalError(fldPath.Child(ce.name), fmt.Errorf("error initializing CEL environment:  %w", ce.err))}
	}
	if ce.issues != nil {
		return field.ErrorList{field.InternalError(fldPath.Child(ce.name), fmt.Errorf("compilation error: %s", ce.issues))}
	}
	return nil
}

func (ce CompiledExpression) Error() error {
	return ce.err
}

func (ce CompiledExpression) Issues() *cel.Issues {
	return ce.issues
}

const (
	// ScopedVarName is the variable name assigned to the locally scoped data element of a CEL validation
	// expression.
	ScopedVarName = "self"

	// OldScopedVarName is the variable name assigned to the existing value of the locally scoped data element of a
	// CEL validation expression.
	OldScopedVarName = "oldSelf"

	SubresourcesVarName = "subresources"

	OptionsVarName = "options"
)

type activation struct {
	self, oldSelf any
	hasOldSelf    bool

	subresources []string
	options      []string
}

func (a *activation) ResolveName(name string) (interface{}, bool) {
	switch name {
	case ScopedVarName:
		return a.self, true
	case OldScopedVarName:
		return a.oldSelf, a.hasOldSelf
	case SubresourcesVarName:
		return a.subresources, true
	case OptionsVarName:
		return a.options, true
	default:
		return nil, false
	}
}

func (a *activation) Parent() interpreter.Activation {
	return nil
}
