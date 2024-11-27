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
	"strconv"

	"k8s.io/gengo/v2"
	"k8s.io/gengo/v2/types"
)

func init() {
	AddToRegistry(InitOpenAPIDeclarativeValidator)
}

func InitOpenAPIDeclarativeValidator(_ *ValidatorConfig) DeclarativeValidator {
	return &openAPIDeclarativeValidator{}
}

type openAPIDeclarativeValidator struct{}

const (
	formatTagName    = "k8s:format"
	maxLengthTagName = "k8s:maxLength"
	maxItemsTagName  = "k8s:maxItems"
)

var (
	ipValidator        = types.Name{Package: libValidationPkg, Name: "IP"}
	dnsLabelValidator  = types.Name{Package: libValidationPkg, Name: "DNSLabel"}
	maxLengthValidator = types.Name{Package: libValidationPkg, Name: "MaxLength"}
	maxItemsValidator  = types.Name{Package: libValidationPkg, Name: "MaxItems"}
)

func (openAPIDeclarativeValidator) ExtractValidations(t *types.Type, comments []string) (Validations, error) {
	var result Validations
	commentTags := gengo.ExtractCommentTags("+", comments)

	maxLength, ok, err := extractOptionalIntValue(commentTags, maxLengthTagName)
	if err != nil {
		return result, err
	}
	if ok {
		result.AddFunction(Function(maxLengthTagName, DefaultFlags, maxLengthValidator, maxLength))
	}

	maxItems, ok, err := extractOptionalIntValue(commentTags, maxItemsTagName)
	if err != nil {
		return result, err
	}
	if ok {
		result.AddFunction(Function(maxItemsTagName, ShortCircuit, maxItemsValidator, maxItems))
	}

	if formats := commentTags[formatTagName]; len(formats) > 0 {
		if len(formats) > 1 {
			return result, fmt.Errorf("multiple %s tags found", formatTagName)
		}
		format := formats[0]
		formatFunction := FormatValidationFunction(format)
		if formatFunction != nil {
			result.AddFunction(formatFunction)
		}
	}

	return result, nil
}

func extractOptionalIntValue(commentTags map[string][]string, tagName string) (int, bool, error) {
	values, ok := commentTags[tagName]
	if !ok || len(values) == 0 {
		return 0, false, nil
	}
	if len(values) > 1 {
		return 0, false, fmt.Errorf("multiple %s tags found", tagName)
	}
	intVal, err := strconv.Atoi(values[0])
	if err != nil {
		return 0, false, fmt.Errorf("failed to parse %s value: %v", tagName, err)
	}
	return intVal, true, nil
}

func (openAPIDeclarativeValidator) Docs() []TagDoc {
	return []TagDoc{{
		Tag:         formatTagName,
		Description: "Indicates that a string field has a particular format.",
		Contexts:    []TagContext{TagContextType, TagContextField},
		Payloads: []TagPayloadDoc{{
			Description: "ip",
			Docs:        "This field holds an IP address value, either IPv4 or IPv6.",
		}, {
			Description: "dns-label",
			Docs:        "This field holds a DNS label value.",
		}},
	}, {
		Tag:         maxLengthTagName,
		Description: "Indicates that a string field has a limit on its length.",
		Contexts:    []TagContext{TagContextType, TagContextField},
		Payloads: []TagPayloadDoc{{
			Description: "<non-negative integer>",
			Docs:        "This field must be no more than X characters long.",
		}},
	}, {
		Tag:         maxItemsTagName,
		Description: "Indidates that a slice field has a limit on its size.",
		Contexts:    []TagContext{TagContextType, TagContextField},
		Payloads: []TagPayloadDoc{
			{
				Description: "<non-negative integer>",
				Docs:        "This field must be no more than X items long.",
			},
		},
	}}
}

func FormatValidationFunction(format string) FunctionGen {
	// The naming convention for these formats follows the JSON schema style:
	// all lower-case, dashes between words. See
	// https://json-schema.org/draft/2020-12/json-schema-validation#name-defined-formats
	// for more examples.
	if format == "ip" {
		return Function(formatTagName, DefaultFlags, ipValidator)
	}
	if format == "dns-label" {
		return Function(formatTagName, DefaultFlags, dnsLabelValidator)
	}
	// TODO: Flesh out the list of validation functions

	return nil // TODO: ignore unsupported formats?
}
