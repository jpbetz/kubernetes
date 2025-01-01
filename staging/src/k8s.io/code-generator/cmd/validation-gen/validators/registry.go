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
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"k8s.io/gengo/v2"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/types"
)

// This is the global registry of tag descriptors. For simplicity this is in
// the same package as tag implementations, but it should not be used directly.
var globalAllTags = &TagRegistry{
	descriptors: map[string]TagDescriptor{},
}

// TagRegistry holds a list of registered tags.
type TagRegistry struct {
	lock        sync.Mutex
	descriptors map[string]TagDescriptor // keyed by tagname
	index       []string                 // all tag names
	initialized atomic.Bool              // init() was called
}

func (tr *TagRegistry) add(desc TagDescriptor) {
	tr.lock.Lock()
	defer tr.lock.Unlock()

	if tr.initialized.Load() {
		panic("TagRegistry was modified after init")
	}

	name := desc.TagName()
	if _, exists := globalAllTags.descriptors[name]; exists {
		panic(fmt.Sprintf("tag %q was registered twice", name))
	}
	globalAllTags.descriptors[name] = desc
}

func (tr *TagRegistry) init(c *generator.Context) {
	tr.lock.Lock()
	defer tr.lock.Unlock()

	if tr.initialized.Load() {
		panic("TagRegistry.init() was called twice")
	}
	tr.initialized.Store(true)

	for _, desc := range globalAllTags.descriptors {
		tr.index = append(tr.index, desc.TagName())
		desc.Init(c)
	}
	sort.Strings(tr.index)
}

// ExtractValidations evaluates a block comments for a given context (e.g. a type
// definition), looking for known tags.  If known tags are found, they are
// executed for the context, producing zero or more validations, which can
// later be rendered by the code-generation logic.
func (tr *TagRegistry) ExtractValidations(context TagContext, comments []string) (Validations, error) {
	if !tr.initialized.Load() {
		panic("TagRegistry.init() was not called")
	}

	// Extract all known tags so we can iterate them.
	tags, err := gengo.ExtractFunctionStyleCommentTags("+", tr.index, comments)
	if err != nil {
		return Validations{}, err
	}
	validations := Validations{}
	for tag, vals := range tags {
		desc := tr.descriptors[tag]
		if scopes := desc.ValidScopes(); !scopes.Has(context.Scope) && !scopes.Has(TagScopeAll) {
			return Validations{}, fmt.Errorf("tag %q cannot be specified on %s", desc.TagName(), context.Scope)
		}
		for _, val := range vals { // tags may have multiple values
			if theseValidations, err := desc.GetValidations(context, val.Args, val.Value); err != nil {
				return Validations{}, err
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
	for _, v := range tr.descriptors {
		result = append(result, v.Docs())
	}
	return result
}

// RegisterTagDescriptor should be called by each tag implementation to
// register its descriptor with the global tag registry.
func RegisterTagDescriptor(desc TagDescriptor) {
	globalAllTags.add(desc)
}

// InitGlobalTagRegistry should be called by the main application to initialize
// and safely access the global tag registry.  Once this is called, no more
// tags may be registered.
func InitGlobalTagRegistry(c *generator.Context) *TagRegistry {
	globalAllTags.init(c)
	return globalAllTags
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
		AllTags:          globalAllTags,
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
