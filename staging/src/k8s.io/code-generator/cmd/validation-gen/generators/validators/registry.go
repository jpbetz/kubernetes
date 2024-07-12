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
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/types"
)

func BuildValidatorContext(c *generator.Context) *ValidatorContext {
	return &ValidatorContext{enumContext: parseEnums(c)}
}

type ValidatorContext struct {
	enumContext enumMap
}

func ExtractValidations(c *ValidatorContext, t *types.Type, comments []string) ([]DeclarativeValidator, error) {
	// TODO: extract additional validations (e.g. for SMD), here.

	// TODO: Organize this as an extensible registry...
	v, err := ExtractOpenAPIValidations(t, comments)
	if err != nil {
		return nil, err
	}
	if enum, ok := c.enumContext[t.Name]; ok {
		symbols := make([]any, len(enum.Values))
		for i, s := range enum.Values {
			symbols[i] = s.Value
		}
		v = append(v, NewValidator(enumValidator, symbols...))
	}
	return v, nil
}
