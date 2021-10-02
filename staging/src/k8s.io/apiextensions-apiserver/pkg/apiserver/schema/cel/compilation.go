/*
Copyright 2021 The Kubernetes Authors.

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
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	celmodel "k8s.io/apiextensions-apiserver/third_party/forked/celopenapi/model"
)

// ScopedTypeName is the placeholder type name used for the type of ScopedVarName if it is an object type.
const ScopedTypeName = "apiextensions.k8s.io.v1alpha1.ValidationExpressionSelf"

// ScopedVarName is the variable name assigned to the locally scoped data element of a CEL valid.
const ScopedVarName = "self"

// CompilationResults represents the compilation results got from cel compilation
type CompilationResults struct {
	Results []CompilationResult
	Error   error
}

// CompilationResult represents the cel compilation result for one rule
type CompilationResult struct {
	Rule      apiextensions.ValidationRule
	Program   cel.Program
	Errors    []error
	RuleIndex int
}

// Compile compiles all the CEL validation rules in the CelRules and returns a slice containing a compiled program for each provided CelRule, or an array of errors.
func Compile(s *schema.Structural) CompilationResults {
	var compilationResults CompilationResults
	if len(s.Extensions.XValidations) == 0 {
		return compilationResults
	}
	celRules := s.Extensions.XValidations

	var propDecls []*expr.Decl
	var root *celmodel.DeclType
	var ok bool
	env, _ := cel.NewEnv()
	reg := celmodel.NewRegistry(env)
	rt, err := celmodel.NewRuleTypes(ScopedTypeName, s, reg)
	if err != nil {
		compilationResults.Error = err
		return compilationResults
	}
	opts, err := rt.EnvOptions(env.TypeProvider())
	if err != nil {
		compilationResults.Error = err
		return compilationResults
	}
	root, ok = rt.FindDeclType(ScopedTypeName)
	if !ok {
		root = celmodel.SchemaDeclType(s).MaybeAssignTypeName(ScopedTypeName)
	}
	// if the type is object, will traverse each field in the object tree and declare
	if root.IsObject() {
		for k, f := range root.Fields {
			// TODO: There is a much larger set of identifiers that collide if unnested
			// Option 1: Escape them
			// Option 2: Exclude them and required they be accessed via 'self' (can we give a very clear compilation error for this if the identifiers are used in CEL programs if we do this?)
			// Option 3: Require everything be accessed via 'self'

			if !(celmodel.IsRootReserved(k) || k == ScopedVarName) {
				propDecls = append(propDecls, decls.NewVar(k, f.Type.ExprType()))
			}
		}
	}
	propDecls = append(propDecls, decls.NewVar(ScopedVarName, root.ExprType()))
	opts = append(opts, cel.Declarations(propDecls...))
	env, err = env.Extend(opts...)
	if err != nil {
		compilationResults.Error = err
		return compilationResults
	}
	compResults := make([]CompilationResult, len(celRules))
	for i, rule := range celRules {
		var compilationResult CompilationResult
		compilationResult.RuleIndex = i
		compilationResult.Rule = rule
		var errors []error
		if rule.Rule == "" {
			errors = append(errors, fmt.Errorf("rule is not specified"))
			compilationResult.Errors = errors
		} else {
			ast, issues := env.Compile(rule.Rule)
			if issues != nil {
				for _, issue := range issues.Errors() {
					errors = append(errors, fmt.Errorf("compilation failed for rule: %v with message: %v", rule, issue.Message))
				}
				compilationResult.Errors = errors
			} else {
				prog, err := env.Program(ast)
				if err != nil {
					errors = append(errors, fmt.Errorf("program instantiation failed for rule: %v with message: %v", rule, err))
					compilationResult.Errors = errors
				} else {
					compilationResult.Program = prog
				}
			}
		}

		compResults[i] = compilationResult
	}
	compilationResults.Results = compResults

	return compilationResults
}
