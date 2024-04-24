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

package embedded

import (
	"context"
	"fmt"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel/model"
	structurallisttype "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/listtype"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	apiextensionsfeatures "k8s.io/apiextensions-apiserver/pkg/features"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	util "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"k8s.io/apiserver/pkg/cel/common"
	"k8s.io/apiserver/pkg/cel/openapi/resolver"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/kube-openapi/pkg/validation/spec"

	"k8s.io/apimachinery/pkg/util/json"
)

type Validator struct {
	resolver   resolver.SchemaResolver
	options    []validation.ValidationOption
	celOptions []cel.Option
}

func NewValidator(resolver resolver.SchemaResolver) *Validator {
	return &Validator{resolver: resolver}
}

// Validate validates embedded resources that have x-embedded-resource-validation set to true.
func (ev *Validator) Validate(ctx context.Context, pth *field.Path, x interface{}, s *structuralschema.Structural) field.ErrorList {
	if s == nil {
		return nil
	}

	var allErrs field.ErrorList

	switch x := x.(type) {
	case map[string]interface{}:
		if s.XEmbeddedResource && s.XEmbeddedResourceValidation {
			allErrs = append(allErrs, ev.validateEmbeddedResource(ctx, pth, x)...)
		}

		for k, v := range x {
			prop, ok := s.Properties[k]
			if ok {
				allErrs = append(allErrs, ev.Validate(ctx, pth.Child(k), v, &prop)...)
			} else if s.AdditionalProperties != nil {
				allErrs = append(allErrs, ev.Validate(ctx, pth.Key(k), v, s.AdditionalProperties.Structural)...)
			}
		}
	case []interface{}:
		for i, v := range x {
			allErrs = append(allErrs, ev.Validate(ctx, pth.Index(i), v, s.Items)...)
		}
	default:
		// scalars, do nothing
	}

	return allErrs
}

func (ev *Validator) ValidateUpdate(ctx context.Context, pth *field.Path, x any, correlatedObject *common.CorrelatedObject, s *structuralschema.Structural) field.ErrorList {
	if s == nil {
		return nil
	}

	var allErrs field.ErrorList

	switch x := x.(type) {
	case map[string]interface{}:
		if s.XEmbeddedResource && s.XEmbeddedResourceValidation {
			allErrs = append(allErrs, ev.validateEmbeddedResourceUpdate(ctx, pth, x, correlatedObject.OldValue.(map[string]any))...)
		}

		for k, v := range x {
			prop, ok := s.Properties[k]
			if ok {
				allErrs = append(allErrs, ev.ValidateUpdate(ctx, pth.Child(k), v, correlatedObject.Key(k), &prop)...)
			} else if s.AdditionalProperties != nil {
				allErrs = append(allErrs, ev.ValidateUpdate(ctx, pth.Key(k), v, correlatedObject.Key(k), s.AdditionalProperties.Structural)...)
			}
		}
	case []interface{}:
		for i, v := range x {
			allErrs = append(allErrs, ev.ValidateUpdate(ctx, pth.Index(i), v, correlatedObject.Index(i), s.Items)...)
		}
	default:
		// scalars, do nothing
	}

	return allErrs
}

func (ev *Validator) validateEmbeddedResource(ctx context.Context, fldPath *field.Path, obj map[string]interface{}) (errs field.ErrorList) {
	u := unstructured.Unstructured{Object: obj}
	kind := u.GetObjectKind()
	if kind == nil {
		// Nothing needs to be done. The object metadata validator already reports this as missing.
		return nil
	}
	embeddedSchema, err := ev.resolver.ResolveSchema(kind.GroupVersionKind())
	if err != nil {
		util.HandleError(err)
		// TODO: Warn if the schema is not found? This requires wiring into WarningOnCreate and WarningOnUpdate..
		return nil
	}

	structural, err := toStructural(embeddedSchema)
	if err != nil {
		errs = append(errs, field.Invalid(fldPath, obj, fmt.Sprintf("Unable to create structural schema for: %v: %v", u.GetObjectKind().GroupVersionKind(), err)))
		return errs
	}
	// Validate using the resolved schema
	errs = append(errs, validation.ValidateCustomResource(fldPath, obj, validation.NewSchemaValidatorFromOpenAPI(embeddedSchema))...)
	errs = append(errs, structurallisttype.ValidateListSetsAndMaps(nil, structural, u.Object)...)
	// TODO: sort out cost
	celErrs, _ := cel.NewValidator(structural, false, celconfig.PerCallLimit).Validate(ctx, nil, structural, u.Object, nil, celconfig.RuntimeCELCostBudget)
	errs = append(errs, celErrs...)

	return errs
}

func (ev *Validator) validateEmbeddedResourceUpdate(ctx context.Context, fldPath *field.Path, obj, oldObj map[string]interface{}) (errs field.ErrorList) {
	u := unstructured.Unstructured{Object: obj}
	kind := u.GetObjectKind()
	if kind == nil {
		// Nothing needs to be done. The object metadata validator already reports this as missing.
		return nil
	}
	embeddedSchema, err := ev.resolver.ResolveSchema(kind.GroupVersionKind())
	if err != nil {
		util.HandleError(err)
		// TODO: Warn if the schema is not found? This requires wiring into WarningOnCreate and WarningOnUpdate..
		return nil
	}

	structural, err := toStructural(embeddedSchema)
	if err != nil {
		errs = append(errs, field.Invalid(fldPath, obj, fmt.Sprintf("Unable to create structural schema for: %v: %v", u.GetObjectKind().GroupVersionKind(), err)))
		return errs
	}

	var options []validation.ValidationOption
	var celOptions []cel.Option
	var correlatedObject *common.CorrelatedObject
	if utilfeature.DefaultFeatureGate.Enabled(apiextensionsfeatures.CRDValidationRatcheting) {
		correlatedObject = common.NewCorrelatedObject(obj, oldObj, &model.Structural{Structural: structural})
		options = append(options, validation.WithRatcheting(correlatedObject))
		celOptions = append(celOptions, cel.WithRatcheting(correlatedObject))
	}

	// Validate using the resolved schema
	errs = append(errs, validation.ValidateCustomResourceUpdate(fldPath, obj, oldObj, validation.NewSchemaValidatorFromOpenAPI(embeddedSchema), options...)...)
	errs = append(errs, structurallisttype.ValidateListSetsAndMaps(nil, structural, u.Object)...)

	celValidator := cel.NewValidator(structural, false, celconfig.PerCallLimit)
	celErrs, _ := celValidator.Validate(ctx, nil, structural, u.Object, oldObj, celconfig.RuntimeCELCostBudget, celOptions...)
	errs = append(errs, celErrs...)

	return errs
}

// TODO: Avoid this conversion. Caching or extending the schema resolver to keep track of this are both options.
// Also, don't marshall through json for the actual conversion.
func toStructural(s *spec.Schema) (*structuralschema.Structural, error) {
	bs, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	v1beta1Schema := &apiextensionsv1beta1.JSONSchemaProps{}
	err = json.Unmarshal(bs, v1beta1Schema)
	if err != nil {
		return nil, err
	}
	internalSchema := &apiextensions.JSONSchemaProps{}
	err = apiextensionsv1beta1.Convert_v1beta1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(v1beta1Schema, internalSchema, nil)
	if err != nil {
		return nil, err
	}
	return structuralschema.NewStructural(internalSchema)
}
