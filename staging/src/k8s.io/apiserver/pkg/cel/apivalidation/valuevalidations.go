/*
Copyright 2023 The Kubernetes Authors.

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

package apivalidation

import (
	"fmt"
	"reflect"
	"regexp"

	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/cel/common"
	"k8s.io/apiserver/pkg/cel/library"
)

func validateValueValidations(fldPath *field.Path, sts common.Schema, obj any) (errs field.ErrorList) {
	if sts == nil {
		return errs
	}
	// TODO: Optimize.
	switch sts.Type() {
	case "object":
		errs = append(errs, validateLength(fldPath, obj, sts.MinProperties(), sts.MaxProperties())...)
		errs = append(errs, validateRequired(fldPath, sts.Required(), obj)...)
	case "array":
		errs = append(errs, validateLength(fldPath, obj, sts.MaxItems(), sts.MaxItems())...)
		if sts.XListType() == "set" {
			errs = append(errs, validateUniqueSet(fldPath, obj)...)
		}
		if sts.XListType() == "map" {
			errs = append(errs, validateUniqueMapKeys(fldPath, obj, sts.XListMapKeys())...)
		}
	case "integer", "number":
		errs = append(errs, validateNumber(fldPath, obj, sts.Minimum(), sts.Maximum(), sts.MultipleOf(), sts.ExclusiveMinimum(), sts.ExclusiveMaximum())...)
	case "string":
		errs = append(errs, validateLength(fldPath, obj, sts.MinLength(), sts.MaxLength())...)
		errs = append(errs, validateFormat(fldPath, sts.Format(), obj)...)
		errs = append(errs, validateEnum(fldPath, sts, obj)...)
		errs = append(errs, validatePattern(fldPath, sts.Pattern(), obj)...)
	}
	return errs
}

func validateRequired(fldPath *field.Path, required []string, obj any) (errs field.ErrorList) {
	if required != nil {
		o, ok := obj.(map[string]any)
		if !ok {
			errs = append(errs, field.InternalError(fldPath, fmt.Errorf("expected map")))
			return
		}
		for _, requiredField := range required {
			if _, ok := o[requiredField]; !ok {
				errs = append(errs, field.Required(fldPath, ""))
			}
		}
	}
	return errs
}

func validatePattern(fldPath *field.Path, pattern string, obj any) (errs field.ErrorList) {
	if len(pattern) > 0 { // TODO: cache to set for faster validation?
		data, ok := obj.(string)
		if !ok {
			errs = append(errs, field.InternalError(fldPath, fmt.Errorf("expected string")))
			return
		}

		matched, err := regexp.Match(pattern, []byte(data)) // TODO: pre-compile and cache
		if err != nil {
			errs = append(errs, field.InternalError(fldPath, fmt.Errorf("regex pattern compilation failed: %v", err)))
			return
		}
		if !matched {
			errs = append(errs, field.Invalid(fldPath, data, fmt.Sprintf("must match pattern '%s'", pattern)))
		}
	}
	return errs
}

func validateEnum(fldPath *field.Path, sts common.Schema, obj any) (errs field.ErrorList) {
	if len(sts.Enum()) > 0 { // TODO: cache to set for faster validation?
		data, ok := obj.(string)
		if !ok {
			errs = append(errs, field.InternalError(fldPath, fmt.Errorf("expected string")))
			return
		}

		found := false
		supported := make([]string, len(sts.Enum()))
		for i, e := range sts.Enum() {
			es, ok := e.(string)
			if !ok {
				errs = append(errs, field.InternalError(fldPath, fmt.Errorf("expected string")))
				return
			}
			if e == data {
				found = true
				break
			}
			supported[i] = es
		}
		if !found {
			errs = append(errs, field.NotSupported(fldPath, data, supported))
		}
	}
	return errs
}

func validateFormat(fldPath *field.Path, format string, obj any) (errs field.ErrorList) {
	if len(format) > 0 {
		data, ok := obj.(string)
		if !ok {
			errs = append(errs, field.InternalError(fldPath, fmt.Errorf("expected string")))
			return
		}
		registry := library.Registry
		if ok := registry.ContainsName(format); !ok {
			errs = append(errs, field.Invalid(fldPath, data, "invalid format"))
		} else if ok := registry.Validates(format, data); !ok {
			errs = append(errs, field.Invalid(fldPath, data, fmt.Sprintf("must be of format: %s", format)))
		}
	}
	return errs
}

// TODO: simplify with constraints.Ordered in the future
func validateNumber(fldPath *field.Path, number any, min, max, multipleOf *float64, exclusiveMin, exclusiveMax bool) (errs field.ErrorList) {
	if min == nil && max == nil {
		return nil
	}
	f := reflect.ValueOf(number).Convert(reflect.TypeOf(float64(0))).Float() // TODO: optimize if needed
	if min != nil {
		if exclusiveMin {
			if f <= *min {
				errs = append(errs, field.Invalid(fldPath, number, fmt.Sprintf("must be greater than %f", *min)))
			}
		} else {
			if f < *min {
				errs = append(errs, field.Invalid(fldPath, number, fmt.Sprintf("must be greater than of equal to %f", *min)))
			}
		}
	}
	if max != nil {
		if exclusiveMax {
			if f >= *max {
				errs = append(errs, field.Invalid(fldPath, number, fmt.Sprintf("must be less than %f", *max)))
			}
		} else {
			if f > *max {
				errs = append(errs, field.Invalid(fldPath, number, fmt.Sprintf("must be less than of equal to %f", *max)))
			}
		}
	}
	if multipleOf != nil {
		if *multipleOf <= 0 {
			errs = append(errs, field.InternalError(fldPath, fmt.Errorf("multipleOf must be postive")))
			return
		}
		if f / *multipleOf != 0 {
			errs = append(errs, field.Invalid(fldPath, number, fmt.Sprintf("must be a multiple of %f", *max)))
		}
	}
	return errs
}

func validateLength(fldPath *field.Path, obj any, min, max *int64) (errs field.ErrorList) {
	// TODO: OpenAPI uses rune count for length, what should we do here noting that we won't want to break existing validation for k8s?
	if min == nil && max == nil {
		return nil
	}
	length := reflect.ValueOf(obj).Len() // TODO: Optimize if needed

	if min != nil && length < int(*min) {
		errs = append(errs, field.Invalid(fldPath, length, fmt.Sprintf("must have at least %d items", int(*min))))
	}
	if max != nil && length > int(*max) {
		errs = append(errs, field.TooMany(fldPath, length, int(*max)))
	}
	return errs
}

// only primitives are supported
func validateUniqueSet(fldPath *field.Path, obj any) (errs field.ErrorList) {
	data, ok := obj.([]any)
	if !ok {
		errs = append(errs, field.InternalError(fldPath, fmt.Errorf("expected slice")))
		return
	}
	set := map[any]struct{}{}
	for _, v := range data {
		if _, found := set[v]; found {
			errs = append(errs, field.Duplicate(fldPath, v))
			return
		}
		set[v] = struct{}{}
	}
	return errs
}

func validateUniqueMapKeys(fldPath *field.Path, obj any, mapKeys []string) (errs field.ErrorList) {
	data, ok := obj.([]any)
	if !ok {
		errs = append(errs, field.InternalError(fldPath, fmt.Errorf("expected slice")))
		return
	}
	keys := map[any]struct{}{}
	for _, v := range data {
		m, ok := v.(map[string]any)
		if !ok {
			errs = append(errs, field.InternalError(fldPath, fmt.Errorf("expected map")))
			return
		}
		k := toMapKey(m, mapKeys)

		if _, found := keys[k]; found {
			errs = append(errs, field.Duplicate(fldPath, v))
			return
		}
		keys[k] = struct{}{}
	}
	return errs
}

func toMapKey(eObj map[string]any, mapKeys []string) any {
	if len(mapKeys) == 1 {
		return eObj[mapKeys[0]]
	}
	if len(mapKeys) == 2 {
		return [2]interface{}{eObj[mapKeys[0]], eObj[mapKeys[1]]}
	}
	if len(mapKeys) == 3 {
		return [3]interface{}{eObj[mapKeys[0]], eObj[mapKeys[1]], eObj[mapKeys[2]]}
	}

	key := make([]interface{}, len(mapKeys))
	for i, kf := range mapKeys {
		key[i] = eObj[kf]
	}
	return fmt.Sprintf("%v", key)
}
