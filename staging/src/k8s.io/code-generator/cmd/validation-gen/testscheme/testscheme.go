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

package testscheme

import (
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/api/operation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// Scheme is similar to runtime.Scheme, but for validation testing purposes. Scheme only supports validation,
// supports registration of any type (not just runtime.Object) and implements Register directly, allowing it
// to also be used as a scheme builder.
// Must only be used with tests that perform all registration before calls to validate.
type Scheme struct {
	validationFuncs    map[reflect.Type]func(opCtx operation.Context, object, oldObject interface{}, subresources ...string) field.ErrorList
	registrationErrors field.ErrorList
}

// New creates a new Scheme.
func New() *Scheme {
	return &Scheme{validationFuncs: map[reflect.Type]func(opCtx operation.Context, object interface{}, oldObject interface{}, subresources ...string) field.ErrorList{}}
}

// AddValidationFunc registers a validation function.
// Last writer wins.
func (s *Scheme) AddValidationFunc(srcType any, fn func(opCtx operation.Context, object, oldObject interface{}, subresources ...string) field.ErrorList) {
	s.validationFuncs[reflect.TypeOf(srcType)] = fn
}

// Validate validates an object using the registered validation function.
func (s *Scheme) Validate(object any, subresources ...string) field.ErrorList {
	if len(s.registrationErrors) > 0 {
		return s.registrationErrors // short circuit with registration errors if any are present
	}
	if fn, ok := s.validationFuncs[reflect.TypeOf(object)]; ok {
		return fn(operation.Context{Operation: operation.Create}, object, nil, subresources...)
	}
	return nil
}

// ValidateUpdate validates an update to an object using the registered validation function.
func (s *Scheme) ValidateUpdate(object, oldObject any, subresources ...string) field.ErrorList {
	if len(s.registrationErrors) > 0 {
		return s.registrationErrors // short circuit with registration errors if any are present
	}
	if fn, ok := s.validationFuncs[reflect.TypeOf(object)]; ok {
		return fn(operation.Context{Operation: operation.Update}, oldObject, object, subresources...)
	}
	return nil
}

// Register adds a scheme setup function to the list.
func (s *Scheme) Register(funcs ...func(*Scheme) error) {
	for _, f := range funcs {
		err := f(s)
		if err != nil {
			s.registrationErrors = append(s.registrationErrors, toRegistrationError(err))
		}
	}
}

func toRegistrationError(err error) *field.Error {
	return field.InternalError(nil, fmt.Errorf("registration error: %w", err))
}
