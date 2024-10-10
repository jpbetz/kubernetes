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

package validators

import (
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/types"
)

var registry = &Registry{}

// ValidatorConfig defines the configuration provided DeclarativeValidatorInit functions.
type ValidatorConfig struct {
	// GeneratorContext provides gogen's generator Context.
	GeneratorContext *generator.Context
	// EmbedValidator provides a way to compose validations.
	// For example, it is possible to define a validation such as "+myValidator=+format=IP" by using
	// the EmbedValidator to extract the validation for "+format=IP" and use the resulting Validations
	// to create the Validations returned by the "+myValidator" DeclarativeValidator.
	// EmbedValidator.ExtractValidations() SHOULD NOT be called during init, since other validators may not have yet
	// initialized and may not yet be registered for use as an embedded validator.
	EmbedValidator DeclarativeValidator
}

type DeclarativeValidatorInit func(cfg *ValidatorConfig) DeclarativeValidator

// AddToRegistry adds a DeclarativeValidator to the registry by providing the
// registry with an initializer it can use to construct a DeclarativeValidator for each
// generator context.
func AddToRegistry(validator DeclarativeValidatorInit) {
	registry.Add(validator)
}

type Registry struct {
	inits []DeclarativeValidatorInit
}

func (r *Registry) Add(validator DeclarativeValidatorInit) {
	r.inits = append(r.inits, validator)
}

func NewValidator(c *generator.Context, enabledTags, disabledTags []string) DeclarativeValidator {
	composite := &compositeValidator{
		validators:   make([]DeclarativeValidator, 0, len(registry.inits)),
		enabledTags:  sets.New(enabledTags...),
		disabledTags: sets.New(disabledTags...)}
	cfg := &ValidatorConfig{
		GeneratorContext: c,
		EmbedValidator:   composite,
	}
	for _, init := range registry.inits {
		composite.validators = append(composite.validators, init(cfg))
	}
	return composite
}

type compositeValidator struct {
	validators                []DeclarativeValidator
	enabledTags, disabledTags sets.Set[string]
}

func (c *compositeValidator) ExtractValidations(t *types.Type, comments []string) (Validations, error) {
	var result Validations
	for _, v := range c.validators {
		validationGen, err := v.ExtractValidations(t, comments)
		if err != nil {
			return result, err
		}
		for _, f := range validationGen.Functions {
			if c.allow(f.TagName()) {
				result.Functions = append(result.Functions, f)
			}
		}
		for _, v := range validationGen.Variables {
			if c.allow(v.TagName()) {
				result.Variables = append(result.Variables, v)
			}
		}
	}
	return result, nil
}

func (c *compositeValidator) allow(tagName string) bool {
	if c.disabledTags.Has(tagName) {
		return false
	}

	return len(c.enabledTags) == 0 || c.enabledTags.Has(tagName)
}

func (c *compositeValidator) Docs() []TagDoc {
	var result []TagDoc
	for _, v := range c.validators {
		result = append(result, v.Docs()...)
	}
	return result
}
