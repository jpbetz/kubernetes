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
	"encoding/json"
	"fmt"
	"github.com/google/cel-go/cel"
	"k8s.io/apimachinery/pkg/api/validate"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/gengo/v2/types"
)

const (
	ruleTagName = "k8s:rule"
)

func init() {
	RegisterTagValidator(&ruleTagValidator{})
}

type ruleTagValidator struct {
}

func (stv *ruleTagValidator) Init(cfg Config) {
}

func (ruleTagValidator) TagName() string {
	return ruleTagName
}

// FIXME: Scope should be: types and fields.
//	      Before enabling field scope, SupportVars support for field validations must be added.

var expressionTagValidScopes = sets.New(ScopeType)

func (ruleTagValidator) ValidScopes() sets.Set[Scope] {
	return expressionTagValidScopes
}

var (
	validateExpression = types.Name{Package: libValidationPkg, Name: "Expression"}
)

var newRule = types.Name{Package: libValidationPkg, Name: "NewRule"}

// TODO: Add message, reason and messageExpression support so that errors can be reported.
// TODO: Figure out how to handle multi-line expressions.

type expressionParams struct {
	Expression        string `json:"expression"`
	Message           string `json:"message,omitempty"`
	MessageExpression string `json:"messageExpression,omitempty"`
	Reason            string `json:"reason,omitempty"`
}

var supportedPolicyReason = sets.New(
	"NotFound",
	"Required",
	"Duplicate",
	"Invalid",
	"NotSupported",
	"Forbidden",
	"TooLong",
	"TooMany",
	"Internal",
	"TypeInvalid",
)

func (stv ruleTagValidator) GetValidations(context Context, args []string, payload string) (Validations, error) {
	var result Validations

	p := &expressionParams{}
	if len(payload) > 0 {
		// Name may optionally be overridden by tag's memberName field.
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return Validations{}, fmt.Errorf("error parsing JSON value: %v (%q)", err, payload)
		}
	}

	// validate payload
	ce := validate.Compile(p.Expression, cel.BoolType)
	if ce.Error() != nil {
		return result, fmt.Errorf("expression: error initializing CEL environment: %w", ce.Error())
	}
	if ce.Issues() != nil {
		return result, fmt.Errorf("expression: compilation error(s):\n%s\n", ce.Issues())
	}
	if len(p.Message) > 0 && len(p.MessageExpression) > 0 {
		return Validations{}, fmt.Errorf("message and message expression cannot be both specified")
	}
	if len(p.MessageExpression) > 0 {
		ce := validate.Compile(p.MessageExpression, cel.StringType)
		if ce.Error() != nil {
			return result, fmt.Errorf("messageExpression: error initializing CEL environment: %w", ce.Error())
		}
		if ce.Issues() != nil {
			return result, fmt.Errorf("messageExpression: compilation error(s):\n%s\n", ce.Issues())
		}

	}
	if p.Reason == "" {
		p.Reason = "Invalid"
	}
	if !supportedPolicyReason.Has(p.Reason) {
		return Validations{}, fmt.Errorf("reason %q not supported", p.Reason)
	}

	// TODO: Check estimated cost against a sane limit.
	//       Get schemas the same way the VAP informational compiler does.
	//       Output the estimated cost into a comment (in a user friendly way, maybe as ratio of limit output by CRD errors)

	// TODO: Avoid the "local" here. This was added to to avoid errors caused when the package is an empty string.
	//       The correct package would be the output package but is not known here. This does not show up in generated code.
	// TODO: Append a consistent hash suffix to avoid generated name conflicts?
	supportVarName := PrivateVar{Name: "ProgramFor" + context.Type.Name.Name, Package: "local"}
	supportVar := Variable(supportVarName, Function(ruleTagName, DefaultFlags, newRule, p.Expression, p.MessageExpression, p.Message, p.Reason))
	result.AddVariable(supportVar)
	fn := Function(ruleTagName, DefaultFlags, validateExpression, supportVarName)
	result.AddFunction(fn)

	return result, nil
}

func (stv ruleTagValidator) Docs() TagDoc {
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
