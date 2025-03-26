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

package validators

import (
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/gengo/v2/types"
)

const (
	expressionTagName = "k8s:expression"
)

func init() {
	RegisterTagValidator(&expressionTagValidator{})
}

type expressionTagValidator struct {
}

func (stv *expressionTagValidator) Init(cfg Config) {
}

func (expressionTagValidator) TagName() string {
	return expressionTagName
}

var expressionTagValidScopes = sets.New(ScopeType)

func (expressionTagValidator) ValidScopes() sets.Set[Scope] {
	return expressionTagValidScopes
}

var (
	validateExpression = types.Name{Package: libValidationPkg, Name: "Expression"}
)

var compile = types.Name{Package: libValidationPkg, Name: "Compile"}

func (stv expressionTagValidator) GetValidations(context Context, args []string, payload string) (Validations, error) {
	var result Validations

	// TODO: Avoid the "local" here. This was added to to avoid errors caused when the package is an empty string.
	//       The correct package would be the output package but is not known here. This does not show up in generated code.
	// TODO: Append a consistent hash suffix to avoid generated name conflicts?
	supportVarName := PrivateVar{Name: "ProgramFor" + context.Type.Name.Name, Package: "local"}
	supportVar := Variable(supportVarName, Function(expressionTagName, DefaultFlags, compile, payload))
	result.AddVariable(supportVar)
	fn := Function(expressionTagName, DefaultFlags, validateExpression, supportVarName)
	result.AddFunction(fn)

	return result, nil
}

func (stv expressionTagValidator) Docs() TagDoc {
	doc := TagDoc{
		Tag:         stv.TagName(),
		Scopes:      stv.ValidScopes().UnsortedList(),
		Description: "Declares CEL expression validation rule",
		Args: []TagArgDoc{{
			Description: "<CEL-expression>",
		}},
		Docs: "TODO: Document CEL environment",
	}
	return doc
}
