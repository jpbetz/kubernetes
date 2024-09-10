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

package mutating

import (
	"fmt"

	"k8s.io/api/admissionregistration/v1alpha1"
	plugincel "k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/admission/plugin/policy/mutating/patch"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/matchconditions"
	apiservercel "k8s.io/apiserver/pkg/cel"
	"k8s.io/apiserver/pkg/cel/environment"
)

// compilePolicy compiles the policy into a PolicyEvaluator
// any error is stored and delayed until invocation.
//
// Each individual mutation is compiled into MutationEvaluationFunc and
// returned is a PolicyEvaluator in the same order as the mutations appeared in the policy.
func compilePolicy(policy *Policy) PolicyEvaluator {
	opts := plugincel.OptionalVariableDeclarations{HasParams: policy.Spec.ParamKind != nil, StrictCost: true, HasAuthorizer: true}
	compiler, err := plugincel.NewCompositedCompiler(environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion(), true))
	if err != nil {
		return PolicyEvaluator{Error: &apiservercel.Error{
			Type:   apiservercel.ErrorTypeInternal,
			Detail: fmt.Sprintf("failed to initialize CEL compiler: %v", err),
		}}
	}

	// Compile and store variables
	compiler.CompileAndStoreVariables(convertv1alpha1Variables(policy.Spec.Variables), opts, environment.StoredExpressions)

	// Compile matchers
	var matcher matchconditions.Matcher = nil
	matchConditions := policy.Spec.MatchConditions
	if len(matchConditions) > 0 {
		matchExpressionAccessors := make([]plugincel.ExpressionAccessor, len(matchConditions))
		for i := range matchConditions {
			matchExpressionAccessors[i] = (*matchconditions.MatchCondition)(&matchConditions[i])
		}
		matcher = matchconditions.NewMatcher(compiler.Compile(matchExpressionAccessors, opts, environment.StoredExpressions), toV1FailurePolicy(policy.Spec.FailurePolicy), "policy", "validate", policy.Name)
	}

	// Compiler patchers
	var patchers []patch.Patcher
	for _, m := range policy.Spec.Mutations {
		switch m.PatchType {
		case v1alpha1.PatchTypeJSONPatch:
			if len(m.JSONPatch) > 0 {
				var ops []patch.JSONPatchOp
				for _, p := range m.JSONPatch {
					var valueEvaluator, pathEvaluator, fromEvaluator *plugincel.Evaluator
					if len(p.ValueExpression) > 0 {
						ce := compiler.CompileEvaluator(&JSONPatchCondition{Expression: p.ValueExpression}, opts, environment.StoredExpressions)
						valueEvaluator = &ce
					}
					if len(p.PathExpression) > 0 {
						ce := compiler.CompileEvaluator(&JSONPatchPathCondition{Expression: p.PathExpression}, opts, environment.StoredExpressions)
						pathEvaluator = &ce
					}
					if len(p.FromExpression) > 0 {
						ce := compiler.CompileEvaluator(&JSONPatchPathCondition{Expression: p.FromExpression}, opts, environment.StoredExpressions)
						fromEvaluator = &ce
					}
					ops = append(ops, patch.JSONPatchOp{Patch: p, ValueEvaluator: valueEvaluator, PathEvaluator: pathEvaluator, FromEvaluator: fromEvaluator})
				}
				patchers = append(patchers, patch.NewJSONPatcher(ops))
			}
		case v1alpha1.PatchTypeApplyConfiguration:
			applyConfOptions := opts
			applyConfOptions.HasObjectTypes = true // Provide Object types to apply configurations
			if m.ApplyConfiguration != nil {
				accessor := &ApplyConfigurationCondition{Expression: m.ApplyConfiguration.Expression}
				compileResult := compiler.CompileEvaluator(accessor, applyConfOptions, environment.StoredExpressions)
				patchers = append(patchers, patch.NewApplyConfigurationPatcher(compileResult))
			}
		}
	}

	return PolicyEvaluator{Matcher: matcher, Mutators: patchers, CompositionEnv: compiler.CompositionEnv}
}
