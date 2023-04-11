/*
Copyright 2022 The Kubernetes Authors.

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
	"sync"

	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"k8s.io/apiserver/pkg/cel/common"

	"github.com/google/cel-go/cel"

	apiservercel "k8s.io/apiserver/pkg/cel"
	"k8s.io/apiserver/pkg/cel/library"
)

const (
	ObjectVarName                    = "object"
	OldObjectVarName                 = "oldObject"
	ParamsVarName                    = "params"
	RequestVarName                   = "request"
	AuthorizerVarName                = "authorizer"
	RequestResourceAuthorizerVarName = "authorizer.requestResource"
)

var (
	initEnvsOnce sync.Once
	initEnvs     envs
	initEnvsErr  error
)

func getEnvs() (envs, error) {
	initEnvsOnce.Do(func() {
		requiredVarsEnv, err := buildRequiredVarsEnv()
		if err != nil {
			initEnvsErr = err
			return
		}

		initEnvs, err = buildWithOptionalVarsEnvs(requiredVarsEnv)
		if err != nil {
			initEnvsErr = err
			return
		}
	})
	return initEnvs, initEnvsErr
}

// This is a similar code as in k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel/compilation.go
// If any changes are made here, consider to make the same changes there as well.
func buildBaseEnv() (*cel.Env, error) {
	var opts []cel.EnvOption
	opts = append(opts, cel.HomogeneousAggregateLiterals())
	// Validate function declarations once during base env initialization,
	// so they don't need to be evaluated each time a CEL rule is compiled.
	// This is a relatively expensive operation.
	opts = append(opts, cel.EagerlyValidateDeclarations(true), cel.DefaultUTCTimeZone(true))
	opts = append(opts, library.ExtensionLibs...)

	return cel.NewEnv(opts...)
}

func buildRequiredVarsEnv() (*cel.Env, error) {
	baseEnv, err := buildBaseEnv()
	if err != nil {
		return nil, err
	}
	var propDecls []cel.EnvOption
	requestType := BuildRequestType()
	typeProvider, err := common.NewOpenAPITypeProvider(requestType)
	if err != nil {
		return nil, err
	}
	opts, err := typeProvider.EnvOptions(baseEnv.TypeProvider())
	if err != nil {
		return nil, err
	}
	propDecls = append(propDecls, cel.Variable(ObjectVarName, cel.DynType))
	propDecls = append(propDecls, cel.Variable(OldObjectVarName, cel.DynType))
	propDecls = append(propDecls, cel.Variable(RequestVarName, requestType.CelType()))

	opts = append(opts, propDecls...)
	env, err := baseEnv.Extend(opts...)
	if err != nil {
		return nil, err
	}
	return env, nil
}

type envs map[OptionalVariableDeclarations]*cel.Env

func buildEnvWithVars(baseVarsEnv *cel.Env, options OptionalVariableDeclarations) (*cel.Env, error) {
	var opts []cel.EnvOption
	if options.HasParams {
		opts = append(opts, cel.Variable(ParamsVarName, cel.DynType))
	}
	if options.HasAuthorizer {
		opts = append(opts, cel.Variable(AuthorizerVarName, library.AuthorizerType))
		opts = append(opts, cel.Variable(RequestResourceAuthorizerVarName, library.ResourceCheckType))
	}
	return baseVarsEnv.Extend(opts...)
}

func buildWithOptionalVarsEnvs(requiredVarsEnv *cel.Env) (envs, error) {
	envs := make(envs, 4) // since the number of variable combinations is small, pre-build a environment for each
	for _, hasParams := range []bool{false, true} {
		for _, hasAuthorizer := range []bool{false, true} {
			opts := OptionalVariableDeclarations{HasParams: hasParams, HasAuthorizer: hasAuthorizer}
			env, err := buildEnvWithVars(requiredVarsEnv, opts)
			if err != nil {
				return nil, err
			}
			envs[opts] = env
		}
	}
	return envs, nil
}

// BuildRequestType generates a DeclType for AdmissionRequest. This may be replaced with a utility that
// converts the native type definition to apiservercel.DeclType once such a utility becomes available.
// The 'uid' field is omitted since it is not needed for in-process admission review.
// The 'object' and 'oldObject' fields are omitted since they are exposed as root level CEL variables.
func BuildRequestType() *common.DeclType {
	field := func(name string, declType *common.DeclType, required bool) *common.DeclField {
		return common.NewDeclField(name, declType, required, nil, nil)
	}
	fields := func(fields ...*common.DeclField) map[string]*common.DeclField {
		result := make(map[string]*common.DeclField, len(fields))
		for _, f := range fields {
			result[f.Name] = f
		}
		return result
	}
	gvkType := common.NewObjectType(nil, "kubernetes.GroupVersionKind", fields(
		field("group", common.StringType, true),
		field("version", common.StringType, true),
		field("kind", common.StringType, true),
	))
	gvrType := common.NewObjectType(nil, "kubernetes.GroupVersionResource", fields(
		field("group", common.StringType, true),
		field("version", common.StringType, true),
		field("resource", common.StringType, true),
	))
	userInfoType := common.NewObjectType(nil, "kubernetes.UserInfo", fields(
		field("username", common.StringType, false),
		field("uid", common.StringType, false),
		field("groups", common.NewListType(nil, common.StringType, -1), false),
		field("extra", common.NewMapType(nil, common.StringType, common.NewListType(nil, common.StringType, -1), -1), false),
	))
	return common.NewObjectType(nil, "kubernetes.AdmissionRequest", fields(
		field("kind", gvkType, true),
		field("resource", gvrType, true),
		field("subResource", common.StringType, false),
		field("requestKind", gvkType, true),
		field("requestResource", gvrType, true),
		field("requestSubResource", common.StringType, false),
		field("name", common.StringType, true),
		field("namespace", common.StringType, false),
		field("operation", common.StringType, true),
		field("userInfo", userInfoType, true),
		field("dryRun", common.BoolType, false),
		field("options", common.DynType, false),
	))
}

// CompilationResult represents a compiled validations expression.
type CompilationResult struct {
	Program            cel.Program
	Error              *apiservercel.Error
	ExpressionAccessor ExpressionAccessor
}

// CompileCELExpression returns a compiled CEL expression.
// perCallLimit was added for testing purpose only. Callers should always use const PerCallLimit from k8s.io/apiserver/pkg/apis/cel/config.go as input.
func CompileCELExpression(expressionAccessor ExpressionAccessor, optionalVars OptionalVariableDeclarations, perCallLimit uint64) CompilationResult {
	var env *cel.Env
	envs, err := getEnvs()
	if err != nil {
		return CompilationResult{
			Error: &apiservercel.Error{
				Type:   apiservercel.ErrorTypeInternal,
				Detail: "compiler initialization failed: " + err.Error(),
			},
			ExpressionAccessor: expressionAccessor,
		}
	}
	env, ok := envs[optionalVars]
	if !ok {
		return CompilationResult{
			Error: &apiservercel.Error{
				Type:   apiservercel.ErrorTypeInvalid,
				Detail: fmt.Sprintf("compiler initialization failed: failed to load environment for %v", optionalVars),
			},
			ExpressionAccessor: expressionAccessor,
		}
	}

	ast, issues := env.Compile(expressionAccessor.GetExpression())
	if issues != nil {
		return CompilationResult{
			Error: &apiservercel.Error{
				Type:   apiservercel.ErrorTypeInvalid,
				Detail: "compilation failed: " + issues.String(),
			},
			ExpressionAccessor: expressionAccessor,
		}
	}
	found := false
	returnTypes := expressionAccessor.ReturnTypes()
	for _, returnType := range returnTypes {
		if ast.OutputType() == returnType {
			found = true
			break
		}
	}
	if !found {
		var reason string
		if len(returnTypes) == 1 {
			reason = fmt.Sprintf("must evaluate to %v", returnTypes[0].String())
		} else {
			reason = fmt.Sprintf("must evaluate to one of %v", returnTypes)
		}

		return CompilationResult{
			Error: &apiservercel.Error{
				Type:   apiservercel.ErrorTypeInvalid,
				Detail: reason,
			},
			ExpressionAccessor: expressionAccessor,
		}
	}

	_, err = cel.AstToCheckedExpr(ast)
	if err != nil {
		// should be impossible since env.Compile returned no issues
		return CompilationResult{
			Error: &apiservercel.Error{
				Type:   apiservercel.ErrorTypeInternal,
				Detail: "unexpected compilation error: " + err.Error(),
			},
			ExpressionAccessor: expressionAccessor,
		}
	}
	prog, err := env.Program(ast,
		cel.EvalOptions(cel.OptOptimize, cel.OptTrackCost),
		cel.OptimizeRegex(library.ExtensionLibRegexOptimizations...),
		cel.InterruptCheckFrequency(celconfig.CheckFrequency),
		cel.CostLimit(perCallLimit),
	)
	if err != nil {
		return CompilationResult{
			Error: &apiservercel.Error{
				Type:   apiservercel.ErrorTypeInvalid,
				Detail: "program instantiation failed: " + err.Error(),
			},
			ExpressionAccessor: expressionAccessor,
		}
	}
	return CompilationResult{
		Program:            prog,
		ExpressionAccessor: expressionAccessor,
	}
}
