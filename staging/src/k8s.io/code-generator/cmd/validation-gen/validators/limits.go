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
	"k8s.io/gengo/v2/parser/tags"
	"k8s.io/gengo/v2/types"
	pointer "k8s.io/utils/ptr"
)

const (
	maxLengthTagName = "k8s:maxLength"
	maxItemsTagName  = "k8s:maxItems"
	minimumTagName   = "k8s:minimum"
	maximumTagName   = "k8s:maximum"
)

func init() {
	RegisterTagValidator(maxLengthTagValidator{})
	RegisterTagValidator(maxItemsTagValidator{})

	shared := map[*types.Type]refLimits{}
	RegisterTagValidator(minimumTagValidator{shared})
	RegisterTagValidator(maximumTagValidator{shared})
	RegisterTypeValidator(maximumTypeValidator{shared})
}

type refLimit struct {
	min, max *string // name of the field that is referenced and provides a min or max limit
}

// member name -> limits
type refLimits map[*types.Member]refLimit

type minimumTagValidator struct {
	shared map[*types.Type]refLimits
}
type maxLengthTagValidator struct{}

func (maxLengthTagValidator) Init(_ Config) {}

func (maxLengthTagValidator) TagName() string {
	return maxLengthTagName
}

var maxLengthTagValidScopes = sets.New(ScopeAny)

func (maxLengthTagValidator) ValidScopes() sets.Set[Scope] {
	return maxLengthTagValidScopes
}

var (
	maxLengthValidator = types.Name{Package: libValidationPkg, Name: "MaxLength"}
)

func (maxLengthTagValidator) GetValidations(context Context, _ []string, payload string) (Validations, error) {
	var result Validations

	// This tag can apply to value and pointer fields, as well as typedefs
	// (which should never be pointers). We need to check the concrete type.
	if t := nonPointer(nativeType(context.Type)); t != types.String {
		return Validations{}, fmt.Errorf("can only be used on string types (%s)", rootTypeString(context.Type, t))
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

func (mltv maxLengthTagValidator) Docs() TagDoc {
	return TagDoc{
		Tag:         mltv.TagName(),
		Scopes:      mltv.ValidScopes().UnsortedList(),
		Description: "Indicates that a string field has a limit on its length.",
		Payloads: []TagPayloadDoc{{
			Description: "<non-negative integer>",
			Docs:        "This field must be no more than X characters long.",
		}},
	}
}

type maxItemsTagValidator struct{}

func (maxItemsTagValidator) Init(_ Config) {}

func (maxItemsTagValidator) TagName() string {
	return maxItemsTagName
}

var maxItemsTagValidScopes = sets.New(
	ScopeType,
	ScopeField,
	ScopeListVal,
	ScopeMapVal,
)

func (maxItemsTagValidator) ValidScopes() sets.Set[Scope] {
	return maxItemsTagValidScopes
}

var (
	maxItemsValidator = types.Name{Package: libValidationPkg, Name: "MaxItems"}
)

func (maxItemsTagValidator) GetValidations(context Context, _ []string, payload string) (Validations, error) {
	var result Validations

	// NOTE: pointers to lists are not supported, so we should never see a pointer here.
	if t := nativeType(context.Type); t.Kind != types.Slice && t.Kind != types.Array {
		return Validations{}, fmt.Errorf("can only be used on list types (%s)", rootTypeString(context.Type, t))
	}

	intVal, err := strconv.Atoi(payload)
	if err != nil {
		return result, fmt.Errorf("failed to parse tag payload as int: %v", err)
	}
	if intVal < 0 {
		return result, fmt.Errorf("must be greater than or equal to zero")
	}
	// Note: maxItems short-circuits other validations.
	result.AddFunction(Function(maxItemsTagName, ShortCircuit, maxItemsValidator, intVal))
	return result, nil
}

func (mitv maxItemsTagValidator) Docs() TagDoc {
	return TagDoc{
		Tag:         mitv.TagName(),
		Scopes:      mitv.ValidScopes().UnsortedList(),
		Description: "Indicates that a list field has a limit on its size.",
		Payloads: []TagPayloadDoc{{
			Description: "<non-negative integer>",
			Docs:        "This field must be no more than X items long.",
		}},
	}
}

func (minimumTagValidator) Init(_ Config) {}

func (minimumTagValidator) TagName() string {
	return minimumTagName
}

var minimumTagValidScopes = sets.New(
	ScopeAny,
)

func (minimumTagValidator) ValidScopes() sets.Set[Scope] {
	return minimumTagValidScopes
}

var (
	minimumValidator = types.Name{Package: libValidationPkg, Name: "Minimum"}
)

func (mtv minimumTagValidator) GetValidations(context Context, args []string, payload string) (Validations, error) {
	var result Validations

	// This tag can apply to value and pointer fields, as well as typedefs
	// (which should never be pointers). We need to check the concrete type.
	if t := nonPointer(nativeType(context.Type)); !types.IsInteger(t) {
		return result, fmt.Errorf("can only be used on integer types (%s)", rootTypeString(context.Type, t))
	}

	if len(args) == 1 {
		// TODO
		panic("not implemented")
	}

	intVal, err := strconv.Atoi(payload)
	if err != nil {
		return result, fmt.Errorf("failed to parse tag payload as int: %w", err)
	}
	result.AddFunction(Function(minimumTagName, DefaultFlags, minimumValidator, intVal))
	return result, nil
}

func (mtv minimumTagValidator) Docs() TagDoc {
	return TagDoc{
		Tag:         mtv.TagName(),
		Scopes:      mtv.ValidScopes().UnsortedList(),
		Description: "Indicates that a numeric field has a minimum value.",
		Payloads: []TagPayloadDoc{{
			Description: "<integer>",
			Docs:        "This field must be greater than or equal to x.",
		}},
	}
}

type maximumTagValidator struct {
	shared map[*types.Type]refLimits
}

func (maximumTagValidator) Init(_ Config) {}

func (maximumTagValidator) TagName() string {
	return maximumTagName
}

var maximumTagValidScopes = sets.New(
	ScopeAny,
)

func (maximumTagValidator) ValidScopes() sets.Set[Scope] {
	return maximumTagValidScopes
}

var (
	maximumValidator = types.Name{Package: libValidationPkg, Name: "Maximum"}
)

func (mtv maximumTagValidator) GetValidations(context Context, args []string, payload string) (Validations, error) {
	var result Validations

	if len(args) == 1 {
		refFieldName := args[0]
		limits, ok := mtv.shared[context.Parent]
		if !ok {
			limits = map[*types.Member]refLimit{}
			mtv.shared[context.Parent] = limits
		}
		limits[context.Member] = refLimit{max: pointer.To(refFieldName)}
		fmt.Printf("added limits: %v -> %+v\n", context.Type, limits)
		return result, nil
	}

	if t := nonPointer(nativeType(context.Type)); !types.IsInteger(t) {
		return result, fmt.Errorf("can only be used on integer types (%s)", rootTypeString(context.Type, t))
	}

	intVal, err := strconv.Atoi(payload)
	if err != nil {
		return result, fmt.Errorf("failed to parse tag payload as int: %w", err)
	}
	result.AddFunction(Function(maximumTagName, DefaultFlags, maximumValidator, intVal))
	return result, nil
}

func (mtv maximumTagValidator) Docs() TagDoc {
	return TagDoc{
		Tag:         mtv.TagName(),
		Scopes:      mtv.ValidScopes().UnsortedList(),
		Description: "Indicates that a numeric field has a maximum value.",
		Payloads: []TagPayloadDoc{{
			Description: "<integer>",
			Docs:        "This field must be less than or equal to x.",
		}},
	}
}

type maximumTypeValidator struct {
	shared map[*types.Type]refLimits
}

func (maximumTypeValidator) Init(_ Config) {}

func (maximumTypeValidator) Name() string {
	return "limitTypeValidator"
}

func (mtv maximumTypeValidator) GetValidations(context Context) (Validations, error) {
	result := Validations{}

	t := nonPointer(nativeType(context.Type))
	if t.Kind != types.Struct {
		return Validations{}, nil
	}

	limits := mtv.shared[context.Type]
	fmt.Printf("checking limits: %v -> %+v\n", context.Type, limits)
	if len(limits) == 0 {
		return result, nil
	}

	fmt.Printf("found limits: %+v\n", limits)
	// TODO: sort before output
	for submemb, limit := range limits {
		jsonTag, ok := tags.LookupJSON(*submemb)
		if !ok {
			return Validations{}, fmt.Errorf("no json tag for %s", submemb.Name)
		}
		// TODO: l.min support
		if limit.max != nil {
			subname := jsonTag.Name
			ref := getMemberByJSON(t, *limit.max)
			if ref == nil {
				return Validations{}, fmt.Errorf("no field for json name %q", *limit.max)
			}
			vfn := Function(maximumTagName, DefaultFlags, maximumValidator, *ref)

			nilableStructType := context.Type
			if !isNilableType(nilableStructType) {
				nilableStructType = types.PointerTo(nilableStructType)
			}
			nilableFieldType := submemb.Type
			fieldExprPrefix := ""
			if !isNilableType(nilableFieldType) {
				nilableFieldType = types.PointerTo(nilableFieldType)
				fieldExprPrefix = "&"
			}

			getFn := FunctionLiteral{
				Parameters: []ParamResult{{"o", nilableStructType}},
				Results:    []ParamResult{{"", nilableFieldType}},
			}
			getFn.Body = fmt.Sprintf("return %so.%s", fieldExprPrefix, submemb.Name)
			f := Function(subfieldTagName, vfn.Flags, validateSubfield, subname, getFn, WrapperFunction{vfn, submemb.Type})
			result.Functions = append(result.Functions, f)
		}
	}
	return result, nil
}
