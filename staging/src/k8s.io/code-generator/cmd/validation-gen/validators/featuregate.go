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
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/code-generator/cmd/validation-gen/util"
	"k8s.io/gengo/v2/codetags"
	"k8s.io/gengo/v2/types"
)

const (
	featureGateTagName = "k8s:featureGate"
)

func init() {
	// Shared state between the tag validator and field validator.
	// The tag validator records gate names per field path; the field
	// validator uses them to produce forbidden functions.
	byPath := map[string][]string{}
	RegisterTagValidator(&featureGateTagValidator{byPath: byPath})
	RegisterFieldValidator(&featureGateFieldValidator{byPath: byPath})
}

// featureGateTagValidator handles +k8s:featureGate=GateName.
// It records the gate name and returns DefaultConditions so that all
// other validations on this field are conditioned on the gate being enabled.
type featureGateTagValidator struct {
	byPath map[string][]string // field path -> gate names
}

func (*featureGateTagValidator) Init(_ Config) {}

func (*featureGateTagValidator) TagName() string {
	return featureGateTagName
}

var featureGateTagValidScopes = sets.New(ScopeField)

func (*featureGateTagValidator) ValidScopes() sets.Set[Scope] {
	return featureGateTagValidScopes
}

func (fgtv *featureGateTagValidator) GetValidations(context Context, tag codetags.Tag) (Validations, error) {
	if tag.Value == "" {
		return Validations{}, fmt.Errorf("missing required feature gate name")
	}

	// Validate the type is suitable for feature gating.
	if util.NativeType(context.Type).Kind == types.Struct {
		return Validations{}, fmt.Errorf("non-pointer structs cannot use the %q tag", featureGateTagName)
	}

	// Record the gate name for this field path.
	fgtv.byPath[context.Path.String()] = append(fgtv.byPath[context.Path.String()], tag.Value)

	// Return DefaultConditions to gate all other validations on this field.
	return Validations{
		DefaultConditions: &Conditions{OptionsEnabled: []string{tag.Value}},
	}, nil
}

func (fgtv *featureGateTagValidator) Docs() TagDoc {
	return TagDoc{
		Tag:            fgtv.TagName(),
		StabilityLevel: TagStabilityLevelAlpha,
		Scopes:         sets.List(fgtv.ValidScopes()),
		Description:    "Declares that a field is gated behind a feature gate. When the gate is disabled, the field is forbidden. When enabled, normal validation applies.",
		Payloads: []TagPayloadDoc{{
			Description: "<gate-name>",
			Docs:        "The name of the feature gate. Use multiple +k8s:featureGate tags for multiple gates; all must be enabled for the field to be allowed.",
		}},
		PayloadsType:     codetags.ValueTypeString,
		PayloadsRequired: true,
	}
}

// featureGateFieldValidator runs after all tag validators and produces
// the forbidden + optional short-circuit functions for gated fields.
type featureGateFieldValidator struct {
	byPath map[string][]string // field path -> gate names (shared with tag validator)
}

func (*featureGateFieldValidator) Init(_ Config) {}

func (*featureGateFieldValidator) Name() string {
	return "featureGate"
}

func (fv *featureGateFieldValidator) GetValidations(context Context) (Validations, error) {
	gates := fv.byPath[context.Path.String()]
	if len(gates) == 0 {
		return Validations{}, nil
	}

	fns, err := forbiddenFunctionsForType(context.Type, gates)
	if err != nil {
		return Validations{}, fmt.Errorf("field validator %q: %w", fv.Name(), err)
	}

	return Validations{
		Functions: fns,
	}, nil
}

// forbiddenFunctionsForType returns the forbidden + optional short-circuit
// function pair for the given type, with conditions set to the inverted gate
// check (i.e., they fire when any gate is disabled).
func forbiddenFunctionsForType(t *types.Type, gates []string) ([]FunctionGen, error) {
	var forbidden, optional types.Name
	switch util.NativeType(t).Kind {
	case types.Slice:
		forbidden = forbiddenSliceValidator
		optional = optionalSliceValidator
	case types.Map:
		forbidden = forbiddenMapValidator
		optional = optionalMapValidator
	case types.Pointer:
		forbidden = forbiddenPointerValidator
		optional = optionalPointerValidator
	case types.Struct:
		return nil, fmt.Errorf("non-pointer structs cannot use the %q tag", featureGateTagName)
	default:
		forbidden = forbiddenValueValidator
		optional = optionalValueValidator
	}

	cond := Conditions{OptionsEnabled: gates, Inverted: true}
	return []FunctionGen{
		Function(featureGateTagName, ShortCircuit, forbidden).WithConditions(cond),
		Function(featureGateTagName, ShortCircuit|NonError, optional).WithConditions(cond),
	}, nil
}
