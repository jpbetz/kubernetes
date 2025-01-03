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

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/types"
)

const (
	formatTagName    = "k8s:format"
	maxLengthTagName = "k8s:maxLength"
	maxItemsTagName  = "k8s:maxItems"
)

func init() {
	RegisterTagValidator(formatTag{})
	RegisterTagValidator(maxLengthTag{})
	RegisterTagValidator(maxItemsTag{})
}

type formatTag struct{}

func (formatTag) Init(_ *generator.Context) {}

func (formatTag) TagName() string {
	return formatTagName
}

var formatTagScopes = sets.New(TagScopeAll)

func (formatTag) ValidScopes() sets.Set[TagScope] {
	return formatTagScopes
}

var (
	ipSloppyValidator = types.Name{Package: libValidationPkg, Name: "IPSloppy"}
	dnsLabelValidator = types.Name{Package: libValidationPkg, Name: "DNSLabel"}
)

func (formatTag) GetValidations(context Context, _ []string, payload string) (Validations, error) {
	var result Validations
	if formatFunction, err := getFormatValidationFunction(payload); err != nil {
		return result, err
	} else if formatFunction == nil {
		return result, fmt.Errorf("internal error: no validation function found for format %q", payload)
	} else {
		result.AddFunction(formatFunction)
	}
	return result, nil
}

func getFormatValidationFunction(format string) (FunctionGen, error) {
	// The naming convention for these formats follows the JSON schema style:
	// all lower-case, dashes between words. See
	// https://json-schema.org/draft/2020-12/json-schema-validation#name-defined-formats
	// for more examples.
	if format == "ip-sloppy" {
		return Function(formatTagName, DefaultFlags, ipSloppyValidator), nil
	}
	if format == "dns-label" {
		return Function(formatTagName, DefaultFlags, dnsLabelValidator), nil
	}
	// TODO: Flesh out the list of validation functions

	return nil, fmt.Errorf("unsupported validation format %q", format)
}

func (ft formatTag) Docs() TagDoc {
	return TagDoc{
		Tag:         ft.TagName(),
		Contexts:    ft.ValidScopes().UnsortedList(),
		Description: "Indicates that a string field has a particular format.",
		Payloads: []TagPayloadDoc{{
			Description: "ip-sloppy",
			Docs:        "This field holds an IPv4 or IPv6 address value. IPv4 octets may have leading zeros.",
		}, {
			Description: "dns-label",
			Docs:        "This field holds a DNS label value.",
		}},
	}
}

type maxLengthTag struct{}

func (maxLengthTag) Init(_ *generator.Context) {}

func (maxLengthTag) TagName() string {
	return maxLengthTagName
}

var maxLengthTagScopes = sets.New(TagScopeAll)

func (maxLengthTag) ValidScopes() sets.Set[TagScope] {
	return maxLengthTagScopes
}

var (
	maxLengthValidator = types.Name{Package: libValidationPkg, Name: "MaxLength"}
)

func (maxLengthTag) GetValidations(context Context, _ []string, payload string) (Validations, error) {
	var result Validations

	t := context.Type
	if t.Kind == types.Alias {
		t = t.Underlying
	}
	if t != types.String {
		return result, fmt.Errorf("can only be used on string types")
	}

	intVal, err := strconv.Atoi(payload)
	if err != nil {
		return result, fmt.Errorf("failed to parse tag payload as int: %v", err)
	}
	if intVal < 0 {
		return result, fmt.Errorf("must be greater than or equal to zero")
	}
	result.AddFunction(Function(maxLengthTagName, DefaultFlags, maxLengthValidator, intVal))
	return result, nil
}

func (mlt maxLengthTag) Docs() TagDoc {
	return TagDoc{
		Tag:         mlt.TagName(),
		Contexts:    mlt.ValidScopes().UnsortedList(),
		Description: "Indicates that a string field has a limit on its length.",
		Payloads: []TagPayloadDoc{{
			Description: "<non-negative integer>",
			Docs:        "This field must be no more than X characters long.",
		}},
	}
}

type maxItemsTag struct{}

func (maxItemsTag) Init(_ *generator.Context) {}

func (maxItemsTag) TagName() string {
	return maxItemsTagName
}

var maxItemsTagScopes = sets.New(
	TagScopeType,
	TagScopeField,
	TagScopeListVal,
	TagScopeMapVal,
)

func (maxItemsTag) ValidScopes() sets.Set[TagScope] {
	return maxItemsTagScopes
}

var (
	maxItemsValidator = types.Name{Package: libValidationPkg, Name: "MaxItems"}
)

func (maxItemsTag) GetValidations(context Context, _ []string, payload string) (Validations, error) {
	var result Validations

	t := context.Type
	if t.Kind == types.Alias {
		t = t.Underlying
	}
	if t.Kind != types.Slice && t.Kind != types.Array {
		return result, fmt.Errorf("can only be used on list types")
	}

	intVal, err := strconv.Atoi(payload)
	if err != nil {
		return result, fmt.Errorf("failed to parse tag payload as int: %v", err)
	}
	if intVal < 0 {
		return result, fmt.Errorf("must be greater than or equal to zero")
	}
	result.AddFunction(Function(maxItemsTagName, ShortCircuit, maxItemsValidator, intVal))
	return result, nil
}

func (mit maxItemsTag) Docs() TagDoc {
	return TagDoc{
		Tag:         mit.TagName(),
		Contexts:    mit.ValidScopes().UnsortedList(),
		Description: "Indicates that a list field has a limit on its size.",
		Payloads: []TagPayloadDoc{
			{
				Description: "<non-negative integer>",
				Docs:        "This field must be no more than X items long.",
			},
		},
	}
}
