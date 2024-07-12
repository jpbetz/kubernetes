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

// TODO: Should this eventually go in the scheme?
var Registry = &ValidationRegistry{validators: map[string]DeclarativeValidation{}}

type ValidationRegistry struct {
	validators map[string]DeclarativeValidation
}

func (r *ValidationRegistry) Register(name string, validator DeclarativeValidation) {
	r.validators[name] = validator
}

func (r *ValidationRegistry) Lookup(name string) (DeclarativeValidation, bool) {
	v, ok := r.validators[name]
	return v, ok
}
