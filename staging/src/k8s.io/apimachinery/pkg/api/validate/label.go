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

package validate

import (
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/api/operation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ValidateLabelConsistency validates that a label value matches a field value
func ValidateLabelConsistency(opCtx operation.Context, fldPath *field.Path, obj, oldObj interface{},
	labelKey string, fieldName string, required bool) field.ErrorList {

	var errs field.ErrorList

	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return errs
		}
		v = v.Elem()
	}

	// Get field value
	if !v.FieldByName(fieldName).IsValid() {
		return append(errs, field.Invalid(fldPath, labelKey,
			fmt.Sprintf("field %s not found", v.FieldByName(fieldName))))
	}
	fieldStr := fmt.Sprintf("%v", v.FieldByName(fieldName).Interface())

	// Check metadata
	if !v.FieldByName("Metadata").IsValid() {
		return errs
	}

	// Get labels
	labelsField := v.FieldByName("Metadata").FieldByName("Labels")
	if !labelsField.IsValid() || labelsField.IsNil() {
		if required {
			return append(errs, field.Required(
				field.NewPath("metadata").Child("labels").Key(labelKey),
				fmt.Sprintf("label %s is required", labelKey)))
		}
		return errs
	}

	labels := labelsField.Interface().(map[string]string)
	// Check if label is required but missing
	if labelValue, hasLabel := labels[labelKey]; required && !hasLabel {
		return append(errs, field.Required(
			field.NewPath("metadata").Child("labels").Key(labelKey),
			fmt.Sprintf("label %s is required", labelKey)))

		// If label exists, it must match
	} else if hasLabel && labelValue != fieldStr {
		return append(errs, field.Invalid(
			field.NewPath("metadata").Child("labels").Key(labelKey),
			labelValue,
			fmt.Sprintf("must match %s (%s)", fieldName, fieldStr)))
	}
	return errs
}
