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
var globalValidatorRegistry = &TagRegistry{
	tagDescriptors: map[string]TagDescriptor{},
}

// TagRegistry holds a list of registered tags.
type TagRegistry struct {
	lock        sync.Mutex
	initialized atomic.Bool // init() was called

	typeValidators []TypeValidator

	tagDescriptors map[string]TagDescriptor // keyed by tagname
	index          []string                 // all tag names
}

func (tr *TagRegistry) add(desc TagDescriptor) {
	if tr.initialized.Load() {
		panic("TagRegistry was modified after init")
	}

	tr.lock.Lock()
	defer tr.lock.Unlock()

	name := desc.TagName()
	if _, exists := globalValidatorRegistry.tagDescriptors[name]; exists {
		panic(fmt.Sprintf("tag %q was registered twice", name))
	}
	globalValidatorRegistry.tagDescriptors[name] = desc
}

func (tr *TagRegistry) addTypeValidator(tv TypeValidator) {
	if tr.initialized.Load() {
		panic("TagRegistry was modified after init")
	}

	tr.lock.Lock()
	defer tr.lock.Unlock()

	globalValidatorRegistry.typeValidators = append(globalValidatorRegistry.typeValidators, tv)
}

func (tr *TagRegistry) init(c *generator.Context) {
	if tr.initialized.Load() {
		panic("TagRegistry.init() was called twice")
	}

	tr.lock.Lock()
	defer tr.lock.Unlock()

	for _, tv := range tr.typeValidators {
		tv.Init(c)
	}
	slices.SortFunc(tr.typeValidators, func(a, b TypeValidator) int {
		return cmp.Compare(a.Name(), b.Name())
	})

	for _, desc := range globalValidatorRegistry.tagDescriptors {
		tr.index = append(tr.index, desc.TagName())
		desc.Init(c)
	}
	sort.Strings(tr.index)

	tr.initialized.Store(true)

}

// ExtractValidations considers the given context (e.g. a type definition) and
// evaluates registered validators.  This includes type validators (which run
// against all types) and tag validators which run only if a specific tag is
// found in the associated comment block.  Any matching validators produce zero
// or more validations, which will later be rendered by the code-generation
// logic.
func (tr *TagRegistry) ExtractValidations(context TagContext, comments []string) (Validations, error) {
	if !tr.initialized.Load() {
		panic("TagRegistry.init() was not called")
	}

	validations := Validations{}

	if context.Scope == TagScopeType {
		// Run all type-validators.
		for _, tv := range tr.typeValidators {
			if theseValidations, err := tv.GetValidations(context.Type, context.Parent); err != nil {
				return Validations{}, fmt.Errorf("type validator %q: %w", tv.Name(), err)
			} else {
				validations.Add(theseValidations)
			}
		}
	}

	// Extract all known tags so we can iterate them.
	tags, err := gengo.ExtractFunctionStyleCommentTags("+", tr.index, comments)
	if err != nil {
		return Validations{}, fmt.Errorf("failed to parse tags: %w", err)
	}
	// Run matching tag-validators.
	for tag, vals := range tags {
		desc := tr.tagDescriptors[tag]
		if scopes := desc.ValidScopes(); !scopes.Has(context.Scope) && !scopes.Has(TagScopeAll) {
			return Validations{}, fmt.Errorf("tag %q cannot be specified on %s", desc.TagName(), context.Scope)
		}
		for _, val := range vals { // tags may have multiple values
			if theseValidations, err := desc.GetValidations(context, val.Args, val.Value); err != nil {
				return Validations{}, fmt.Errorf("taq %q: %w", desc.TagName(), err)
			} else {
				validations.Add(theseValidations)
			}
		}
	}

	return validations, nil
}

// Docs returns documentation for each tag in this registry.
func (tr *TagRegistry) Docs() []TagDoc {
	var result []TagDoc
	for _, v := range tr.tagDescriptors {
		result = append(result, v.Docs())
	}
	return result
}

// RegisterTagDescriptor should be called by each tag implementation to
// register its descriptor with the global tag registry.
func RegisterTagDescriptor(desc TagDescriptor) {
	globalValidatorRegistry.add(desc)
}

func RegisterTypeValidator(tv TypeValidator) {
	globalValidatorRegistry.addTypeValidator(tv)
}

// InitGlobalTagRegistry should be called by the main application to initialize
// and safely access the global tag registry.  Once this is called, no more
// tags may be registered.
func InitGlobalTagRegistry(c *generator.Context) *TagRegistry {
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
	AllTags *TagRegistry
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
		GeneratorContext: c,
		EmbedValidator:   composite,
		AllTags:          globalValidatorRegistry,
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
