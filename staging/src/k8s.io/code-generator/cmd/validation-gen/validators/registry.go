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
	"cmp"
	"fmt"
	"slices"
	"sort"
	"sync"
	"sync/atomic"

	"k8s.io/gengo/v2"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/types"
)

// This is the global registry of tag validators. For simplicity this is in
// the same package as the implementations, but it should not be used directly.
var globalValidatorRegistry = &ValidatorRegistry{
	tagValidators: map[string]TagValidator{},
}

// ValidatorRegistry holds a list of registered tags.
type ValidatorRegistry struct {
	lock        sync.Mutex
	initialized atomic.Bool // init() was called

	typeValidators []TypeValidator

	tagValidators map[string]TagValidator // keyed by tagname
	tagIndex      []string                // all tag names
}

func (reg *ValidatorRegistry) addTagValidator(tv TagValidator) {
	if reg.initialized.Load() {
		panic("ValidatorRegistry was modified after init")
	}

	reg.lock.Lock()
	defer reg.lock.Unlock()

	name := tv.TagName()
	if _, exists := globalValidatorRegistry.tagValidators[name]; exists {
		panic(fmt.Sprintf("tag %q was registered twice", name))
	}
	globalValidatorRegistry.tagValidators[name] = tv
}

func (reg *ValidatorRegistry) addTypeValidator(tv TypeValidator) {
	if reg.initialized.Load() {
		panic("ValidatorRegistry was modified after init")
	}

	reg.lock.Lock()
	defer reg.lock.Unlock()

	globalValidatorRegistry.typeValidators = append(globalValidatorRegistry.typeValidators, tv)
}

func (reg *ValidatorRegistry) init(c *generator.Context) {
	if reg.initialized.Load() {
		panic("ValidatorRegistry.init() was called twice")
	}

	reg.lock.Lock()
	defer reg.lock.Unlock()

	for _, tv := range reg.typeValidators {
		tv.Init(c)
	}
	slices.SortFunc(reg.typeValidators, func(a, b TypeValidator) int {
		return cmp.Compare(a.Name(), b.Name())
	})

	for _, tv := range globalValidatorRegistry.tagValidators {
		reg.tagIndex = append(reg.tagIndex, tv.TagName())
		tv.Init(c)
	}
	sort.Strings(reg.tagIndex)

	reg.initialized.Store(true)
}

// ExtractValidations considers the given context (e.g. a type definition) and
// evaluates registered validators.  This includes type validators (which run
// against all types) and tag validators which run only if a specific tag is
// found in the associated comment block.  Any matching validators produce zero
// or more validations, which will later be rendered by the code-generation
// logic.
func (reg *ValidatorRegistry) ExtractValidations(context Context, comments []string) (Validations, error) {
	if !reg.initialized.Load() {
		panic("ValidatorRegistry.init() was not called")
	}

	validations := Validations{}

	if context.Scope == TagScopeType {
		// Run all type-validators.
		for _, tv := range reg.typeValidators {
			if theseValidations, err := tv.GetValidations(context.Type, context.Parent); err != nil {
				return Validations{}, fmt.Errorf("type validator %q: %w", tv.Name(), err)
			} else {
				validations.Add(theseValidations)
			}
		}
	}

	// Extract all known tags so we can iterate them.
	tags, err := gengo.ExtractFunctionStyleCommentTags("+", reg.tagIndex, comments)
	if err != nil {
		return Validations{}, fmt.Errorf("failed to parse tags: %w", err)
	}
	// Run matching tag-validators.
	for tag, vals := range tags {
		tv := reg.tagValidators[tag]
		if scopes := tv.ValidScopes(); !scopes.Has(context.Scope) && !scopes.Has(TagScopeAll) {
			return Validations{}, fmt.Errorf("tag %q cannot be specified on %s", tv.TagName(), context.Scope)
		}
		for _, val := range vals { // tags may have multiple values
			if theseValidations, err := tv.GetValidations(context, val.Args, val.Value); err != nil {
				return Validations{}, fmt.Errorf("tag %q: %w", tv.TagName(), err)
			} else {
				validations.Add(theseValidations)
			}
		}
	}

	return validations, nil
}

// Docs returns documentation for each tag in this registry.
func (reg *ValidatorRegistry) Docs() []TagDoc {
	var result []TagDoc
	for _, v := range reg.tagValidators {
		result = append(result, v.Docs())
	}
	return result
}

// RegisterTagValidator must be called by any validator which wants to run when
// a specific tag is found.
func RegisterTagValidator(tv TagValidator) {
	globalValidatorRegistry.addTagValidator(tv)
}

// RegisterTypeValidator must be called by any validator which wants to run
// against every type definition.
func RegisterTypeValidator(tv TypeValidator) {
	globalValidatorRegistry.addTypeValidator(tv)
}

// InitGlobalValidatorRegistry must be called exactly once by the main
// application to initialize and safely access the global tag registry.  Once
// this is called, no more validators may be registered.
func InitGlobalValidatorRegistry(c *generator.Context) *ValidatorRegistry {
	globalValidatorRegistry.init(c)
	return globalValidatorRegistry
}

/* ---------------- */

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
	// This is temporary until conversion is done.
	ValidatorRegistry *ValidatorRegistry
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

func NewValidator(c *generator.Context) DeclarativeValidator {
	composite := &compositeValidator{
		validators: make([]DeclarativeValidator, 0, len(registry.inits)),
	}
	cfg := &ValidatorConfig{
		GeneratorContext:  c,
		EmbedValidator:    composite,
		ValidatorRegistry: globalValidatorRegistry,
	}
	for _, init := range registry.inits {
		composite.validators = append(composite.validators, init(cfg))
	}
	return composite
}

type compositeValidator struct {
	validators []DeclarativeValidator
}

func (c *compositeValidator) ExtractValidations(t *types.Type, comments []string) (Validations, error) {
	var result Validations
	for _, v := range c.validators {
		validationGen, err := v.ExtractValidations(t, comments)
		if err != nil {
			return result, err
		}
		for _, f := range validationGen.Functions {
			result.Functions = append(result.Functions, f)
		}
		for _, v := range validationGen.Variables {
			result.Variables = append(result.Variables, v)
		}
		for _, v := range validationGen.Comments {
			result.Comments = append(result.Comments, v)
		}
	}
	return result, nil
}

func (c *compositeValidator) Docs() []TagDoc {
	var result []TagDoc
	for _, v := range c.validators {
		result = append(result, v.Docs()...)
	}
	return result
}
