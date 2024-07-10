// This represents what we roughly want to generate.

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func Validate_Flunder(s *Flunder) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, Validate_FlunderSpec(&s.Spec, field.NewPath("spec"))...)
	return allErrs
}

func Validate_FlunderSpec(s *FlunderSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if len(s.Reference) > 128 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("reference"), s.Reference, "must not be longer than 128 characters"))
	}
	return allErrs
}
