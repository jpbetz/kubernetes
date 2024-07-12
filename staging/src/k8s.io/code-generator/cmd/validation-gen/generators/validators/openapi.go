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
	"k8s.io/gengo/v2/types"
	"k8s.io/kube-openapi/pkg/generators"
)

const (
	markerPrefix = "+k8s:validation:"

	utilValidationPkg = "k8s.io/apimachinery/pkg/util/validation"
)

var (
	isFullyQualifiedNameValidator = types.Name{Package: utilValidationPkg, Name: "IsFullyQualifiedName"}
	isValidIPValidator            = types.Name{Package: utilValidationPkg, Name: "IsValidIP"}
	maxLengthValidator            = types.Name{Package: utilValidationPkg, Name: "ValidateMaxLength"}
	enumValidator                 = types.Name{Package: utilValidationPkg, Name: "ValidateEnum"}
)

func ExtractOpenAPIValidations(t *types.Type, comments []string) ([]DeclarativeValidator, error) {
	var v []DeclarativeValidator

	// Leverage the kube-openapi parser for 'k8s:validation:' validations.
	schema, err := generators.ParseCommentTags(t, comments, markerPrefix)
	if err != nil {
		return nil, err
	}
	if schema.MaxLength != nil {
		v = append(v, NewValidator(maxLengthValidator, *schema.MaxLength))
	}
	if len(schema.Format) > 0 {
		v = append(v, NewFormatValidator(schema.Format))
	}

	return v, nil
}

func NewFormatValidator(arg string) DeclarativeValidator {
	if arg == "fullyQualifiedName" {
		return NewValidator(isFullyQualifiedNameValidator)
	}
	if arg == "ip" {
		return NewValidator(isValidIPValidator)
	}
	return nil
}
