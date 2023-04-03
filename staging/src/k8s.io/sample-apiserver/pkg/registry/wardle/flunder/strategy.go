/*
Copyright 2017 The Kubernetes Authors.

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

package flunder

import (
	"context"
	"fmt"
	"math"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"k8s.io/apiserver/pkg/cel/apivalidation"
	openapiresolver "k8s.io/apiserver/pkg/cel/openapi/resolver"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/names"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/sample-apiserver/pkg/apis/wardle/validation"

	"k8s.io/sample-apiserver/pkg/apis/wardle"
	"k8s.io/sample-apiserver/pkg/apis/wardle/install"
	"k8s.io/sample-apiserver/pkg/generated/openapi"
)

// NewStrategy creates and returns a flunderStrategy instance
func NewStrategy(typer runtime.ObjectTyper) flunderStrategy {
	schemaResolver := openapiresolver.NewDefinitionsSchemaResolver(k8sscheme.Scheme, openapi.GetOpenAPIDefinitions)
	declarativeValidator := apivalidation.NewDeclarativeValidator(schemaResolver, celconfig.PerCallLimit)
	return flunderStrategy{typer, names.SimpleNameGenerator, declarativeValidator}
}

// GetAttrs returns labels.Set, fields.Set, and error in case the given runtime.Object is not a Flunder
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	apiserver, ok := obj.(*wardle.Flunder)
	if !ok {
		return nil, nil, fmt.Errorf("given object is not a Flunder")
	}
	return labels.Set(apiserver.ObjectMeta.Labels), SelectableFields(apiserver), nil
}

// MatchFlunder is the filter used by the generic etcd backend to watch events
// from etcd to clients of the apiserver only interested in specific labels/fields.
func MatchFlunder(label labels.Selector, field fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    field,
		GetAttrs: GetAttrs,
	}
}

// SelectableFields returns a field set that represents the object.
func SelectableFields(obj *wardle.Flunder) fields.Set {
	return generic.ObjectMetaFieldsSet(&obj.ObjectMeta, true)
}

type flunderStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
	declarativeValidator *apivalidation.DeclarativeValidator
}

func (flunderStrategy) NamespaceScoped() bool {
	return true
}

func (flunderStrategy) PrepareForCreate(ctx context.Context, obj runtime.Object) {
}

func (flunderStrategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
}

var scheme = runtime.NewScheme()

func init() {
	install.Install(scheme)
}

func (f flunderStrategy) Validate(ctx context.Context, obj runtime.Object) (errors field.ErrorList) {
	// TODO: This is a hack for prototyping declarative validation.
	// The hack grabs the GroupVersion from the request info and then converts the object from the internal
	// type to the GroupVersion and validates it declaratively.
	if requestInfo, found := genericapirequest.RequestInfoFrom(ctx); found {
		groupVersion := schema.GroupVersion{Group: requestInfo.APIGroup, Version: requestInfo.APIVersion}
		versionedObj, err := scheme.ConvertToVersion(obj, groupVersion)
		if err != nil {
			errors = field.ErrorList{field.InternalError(field.NewPath("root"), fmt.Errorf("unexpected error converting to versioned type: %w", err))}
			return errors
		}
		declErrors, _ := f.declarativeValidator.ValidateSpec(ctx, versionedObj, nil, math.MaxInt64)
		errors = append(errors, declErrors...)
	}

	flunder := obj.(*wardle.Flunder)
	return validation.ValidateFlunder(flunder)
}

// WarningsOnCreate returns warnings for the creation of the given object.
func (flunderStrategy) WarningsOnCreate(ctx context.Context, obj runtime.Object) []string { return nil }

func (flunderStrategy) AllowCreateOnUpdate() bool {
	return false
}

func (flunderStrategy) AllowUnconditionalUpdate() bool {
	return false
}

func (flunderStrategy) Canonicalize(obj runtime.Object) {
}

func (flunderStrategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	return field.ErrorList{}
}

// WarningsOnUpdate returns warnings for the given update.
func (flunderStrategy) WarningsOnUpdate(ctx context.Context, obj, old runtime.Object) []string {
	return nil
}
