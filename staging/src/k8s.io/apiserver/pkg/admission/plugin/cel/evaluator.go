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

package cel

import (
	"context"
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/cel"
	"k8s.io/apiserver/pkg/cel/environment"
	"k8s.io/apiserver/pkg/cel/mutation"
	"k8s.io/apiserver/pkg/cel/openapi/resolver"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

// evaluatorCompiler implement the interface PatchCompiler.
type evaluatorCompiler struct {
	compiler Compiler
}

// CompilePatch compiles a CEL expression for admission plugins and returns an Patch for executing the
// compiled CEL expression.
func (p *evaluatorCompiler) CompilePatch(expressionAccessor ExpressionAccessor, options OptionalVariableDeclarations, mode environment.Type) Patch {
	compilationResult := p.compiler.CompileCELExpression(expressionAccessor, options, mode)
	return NewPatch(compilationResult, p.compiler, expressionAccessor, options)
}

type patch struct {
	compilationResult CompilationResult // dynamic compilation result

	// Fields needed for schema aware recompilation
	compiler           Compiler
	expressionAccessor ExpressionAccessor
	options            OptionalVariableDeclarations
}

func NewPatch(compilationResult CompilationResult, compiler Compiler, expressionAccessor ExpressionAccessor, options OptionalVariableDeclarations) Patch {
	return &patch{compilationResult: compilationResult, expressionAccessor: expressionAccessor, compiler: compiler, options: options}
}

// ForInput evaluates the compiled CEL expression, recompiling the expression as needed to check it against the
// late bound type information provided by the SchemaResolver.
// Errors from compilation and evaluation are returned in the EvaluationResult.
// runtimeCELCostBudget was added for testing purpose only. Callers should always use const RuntimeCELCostBudget from k8s.io/apiserver/pkg/apis/cel/config.go as input.
func (p *patch) ForInput(ctx context.Context, schemaResolver resolver.SchemaResolver, versionedAttr *admission.VersionedAttributes, request *admissionv1.AdmissionRequest, optionalVars OptionalVariableBindings, namespace *v1.Namespace, runtimeCELCostBudget int64) (EvaluationResult, int64, error) {
	var err error
	var objSchema *spec.Schema
	if p.compilationResult.Error != nil {
		evaluation := EvaluationResult{
			Error: &cel.Error{
				Type:   cel.ErrorTypeInvalid,
				Detail: fmt.Sprintf("compilation error: %v", p.compilationResult.Error),
				Cause:  p.compilationResult.Error,
			},
		}
		return evaluation, -1, nil
	}

	// If we have a schema, recompile against the schema.
	// FIXME: Before MutatingAdmissionPolicy promotes to beta. per-schema recompiled programs need to be cached.
	compilationResult := p.compilationResult
	if lbCompiler, ok := p.compiler.(LateBoundCompiler); ok && schemaResolver != nil {
		objSchema, err = schemaResolver.ResolveSchema(versionedAttr.VersionedKind)
		if err != nil {
			return EvaluationResult{}, -1, err
		}
		schemaTypeResolver := &mutation.SchemaTypeResolver{ObjectSchema: objSchema}
		compilationResult = lbCompiler.CompileWithLateBoundTypes(schemaTypeResolver, p.expressionAccessor, p.options, environment.StoredExpressions)
	}

	activation, err := newActivation(ctx, versionedAttr, request, optionalVars, namespace, schemaResolver)
	if err != nil {
		return EvaluationResult{}, -1, err
	}
	evaluation, remainingBudget, err := evaluateWithActivation(ctx, activation, compilationResult, runtimeCELCostBudget)
	if err != nil {
		return evaluation, -1, err
	}
	return evaluation, remainingBudget, nil
}

// CompilationErrors returns a list of all the errors from the compilation of the patch
func (p *patch) CompilationErrors() (compilationErrors []error) {
	if p.compilationResult.Error != nil {
		return []error{p.compilationResult.Error}
	}
	return nil
}
