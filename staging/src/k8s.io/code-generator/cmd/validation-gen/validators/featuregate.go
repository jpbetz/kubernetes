/*
Copyright 2026 The Kubernetes Authors.

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
	// featureGateDropArg opts a field into automatic dropping when the gate is
	// disabled and the field is not in use: +k8s:featureGate(drop: true)=<GateName>.
	featureGateDropArg = "drop"
)

func init() {
	RegisterTagValidator(&featureGateTagValidator{specsByPath: map[string]*featureGateFieldSpec{}})
}

// featureGateFieldSpec accumulates the gates declared on a field path, in
// declaration order. Each gate's declarative-validation option is the gate name.
type featureGateFieldSpec struct {
	gates []string
}

// featureGateTagValidator handles +k8s:featureGate=<GateName>. On a field it
// gates every other validation on the gate's option being enabled (via
// DefaultConditions), and emits a forbidden validation (with its short-circuit
// optional companion) that fires when the field is set while any gate's option
// is absent. The forbidden pair is aggregated across the field's gates and
// emitted once via a deferred callback. The drop arg records the field's drop
// opt-in for the feature-gate generator.
type featureGateTagValidator struct {
	specsByPath map[string]*featureGateFieldSpec
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
	// Non-pointer structs cannot be unset, so "forbidden when the gate is off"
	// has no well-defined meaning for them.
	if util.NativeType(context.Type).Kind == types.Struct {
		return Validations{}, fmt.Errorf("non-pointer structs cannot use the %q tag", featureGateTagName)
	}

	drop := false
	if arg, ok := tag.NamedArg(featureGateDropArg); ok {
		drop = arg.Value == "true"
	}

	path := context.Path.String()
	spec := fgtv.specsByPath[path]
	if spec == nil {
		spec = &featureGateFieldSpec{}
		fgtv.specsByPath[path] = spec
	}
	spec.gates = append(spec.gates, tag.Value)

	return Validations{
		// Gate the field's other validations on this gate's option (the gate
		// name). Multiple tags merge (AND) via Validations.Add.
		DefaultConditions: &Conditions{OptionsEnabled: []string{tag.Value}},
		FeatureGateSpec:   &FeatureGateSpec{Gates: []string{tag.Value}, Drop: drop},
		Deferred: []DeferredGen{
			Deferred(ThisContext, func() (Validations, error) {
				return fgtv.forbiddenValidations(context)
			}),
		},
	}, nil
}

// forbiddenValidations emits the combined forbidden + optional functions for a
// field. It is idempotent across the deferred callbacks registered for a
// multi-gated field: the first invocation consumes the accumulated options, later
// invocations are no-ops.
func (fgtv *featureGateTagValidator) forbiddenValidations(context Context) (Validations, error) {
	path := context.Path.String()
	spec := fgtv.specsByPath[path]
	if spec == nil || len(spec.gates) == 0 {
		return Validations{}, nil
	}
	gates := spec.gates
	delete(fgtv.specsByPath, path)

	var forbidden, optional types.Name
	switch util.NativeType(context.Type).Kind {
	case types.Slice:
		forbidden, optional = forbiddenSliceValidator, optionalSliceValidator
	case types.Map:
		forbidden, optional = forbiddenMapValidator, optionalMapValidator
	case types.Pointer:
		forbidden, optional = forbiddenPointerValidator, optionalPointerValidator
	case types.Struct:
		return Validations{}, fmt.Errorf("non-pointer structs cannot use the %q tag", featureGateTagName)
	default:
		forbidden, optional = forbiddenValueValidator, optionalValueValidator
	}

	// The field is forbidden when any gate's option is absent: !(A && B && ...).
	cond := Conditions{OptionsEnabled: gates, Inverted: true}
	return Validations{
		Functions: []FunctionGen{
			Function(featureGateTagName, ShortCircuit, forbidden).WithConditions(cond),
			Function(featureGateTagName, ShortCircuit|NonError, optional).WithConditions(cond),
		},
	}, nil
}

func (fgtv *featureGateTagValidator) Docs() TagDoc {
	return TagDoc{
		Tag:            fgtv.TagName(),
		StabilityLevel: TagStabilityLevelAlpha,
		Scopes:         sets.List(fgtv.ValidScopes()),
		Description:    "Declares that a field is gated behind a feature gate. When the gate is disabled the field is forbidden; when enabled, normal validation applies.",
		Args: []TagArgDoc{{
			Name:        featureGateDropArg,
			Description: "<bool>",
			Type:        codetags.ArgTypeBool,
			Docs:        "If true, the field is also automatically cleared (dropped) when the gate is disabled and the field is not in use. Omit for declarative-validation gating only.",
		}},
		Payloads: []TagPayloadDoc{{
			Description: "<gate-name>",
			Docs:        "The name of the feature gate. Use multiple +k8s:featureGate tags to require multiple gates; all must be enabled for the field to be allowed.",
		}},
		PayloadsType:     codetags.ValueTypeString,
		PayloadsRequired: true,
	}
}
