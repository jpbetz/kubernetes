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
	"encoding/json"
	"fmt"

	"k8s.io/gengo/v2"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/types"
)

var validateLabelConsistency = types.Name{Package: libValidationPkg, Name: "ValidateLabelConsistency"}

func init() {
	AddToRegistry(InitLabelValidator)
}

type labelValidator struct {
	universe types.Universe
}

const (
	labelConsistencyTag = "labelConsistency"
)

type labelValidationRule struct {
	Label    string `json:"label"`
	Required bool   `json:"required,omitempty"`
}

// rule represents a parsed validation rule
type labelValidation struct {
	fieldName   string
	fieldMember types.Member
	rule        labelValidationRule
}

func InitLabelValidator(c *generator.Context) DeclarativeValidator {
	return &labelValidator{
		universe: c.Universe,
	}
}

func (v *labelValidator) ExtractValidations(t *types.Type, comments []string) (Validations, error) {
	var result Validations
	var validations []labelValidation

	for _, member := range t.Members {
		commentTags := gengo.ExtractCommentTags("+", member.CommentLines)
		if commentTag, ok := commentTags[labelConsistencyTag]; ok {
			if len(commentTag) != 1 {
				return result, fmt.Errorf("must have one %q tag", labelConsistencyTag)
			}
			// Parse the rule
			var rule labelValidationRule
			if len(commentTag[0]) > 0 {
				if err := json.Unmarshal([]byte(commentTag[0]), &rule); err != nil {
					return result, fmt.Errorf("error parsing JSON value for %q: %v (%q)", member.Name, err, commentTag[0])
				}
			}
			// Validate the rule
			if rule.Label == "" {
				return result, fmt.Errorf("field %q has label consistency tag but no label specified", member.Name)
			}
			validations = append(validations, labelValidation{
				fieldName:   member.Name,
				fieldMember: member,
				rule:        rule,
			})
		}
	}

	// Create validation functions for generated code
	for _, validation := range validations {
		fn := Function(labelConsistencyTag, DefaultFlags, validateLabelConsistency,
			[]any{validation.rule.Label, validation.fieldName, validation.rule.Required}...)
		result.Functions = append(result.Functions, fn)
	}
	return result, nil
}

func (v *labelValidator) Docs() []TagDoc {
	return []TagDoc{{
		Tag:         labelConsistencyTag,
		Description: "Validates consistency between a label value and a field value",
		Contexts:    []TagContext{TagContextField},
		Payloads: []TagPayloadDoc{{
			Description: `{"label": "<key>", "required": <bool>}`,
			Docs:        "Validates that the label value matches this field's value",
			Schema: []TagPayloadSchema{{
				Key:   "label",
				Value: "string",
				Docs:  "The label key to validate against this field",
			}, {
				Key:     "required",
				Value:   "bool",
				Default: "false",
				Docs:    "Whether the label must exist",
			}},
		}},
	}}
}
