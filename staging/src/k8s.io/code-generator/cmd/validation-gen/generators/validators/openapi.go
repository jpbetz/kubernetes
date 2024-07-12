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

import "k8s.io/gengo/v2/types"

func NewFormatValidator(universe types.Universe, arg string) DeclarativeValidator {
	// TODO: Optimize into consts
	if arg == "fullyQualifiedName" {
		return &FormatValidator{
			function: universe.Type(types.Name{Package: "k8s.io/apimachinery/pkg/util/validation", Name: "IsFullyQualifiedName"}),
		}
	}
	if arg == "ip" {
		return &FormatValidator{
			function: universe.Type(types.Name{Package: "k8s.io/apimachinery/pkg/util/validation", Name: "IsValidIP"}),
		}
	}
	return nil
}

type FormatValidator struct {
	function *types.Type
}

func (v *FormatValidator) ValidationSignature() (function *types.Type, args []string) {
	return v.function, nil
}

func NewMaxLengthValidator(universe types.Universe, arg string) DeclarativeValidator {
	return &MaxLengthValidator{
		function: universe.Type(types.Name{Package: "k8s.io/apimachinery/pkg/util/validation", Name: "ValidateMaxLength"}),
		length:   arg,
	}
}

type MaxLengthValidator struct {
	function *types.Type
	length   string
}

func (v *MaxLengthValidator) ValidationSignature() (function *types.Type, args []string) {
	return v.function, []string{v.length}
}

func init() {
	Registry.Register("k8s:validation:format", NewFormatValidator)
	Registry.Register("k8s:validation:maxLength", NewMaxLengthValidator)
}
