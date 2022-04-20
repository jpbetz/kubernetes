/*
Copyright 2022 The Kubernetes Authors.

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

package validatingruleconfiguration

import (
	"context"
	"reflect"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/storage/names"

	"k8s.io/kubernetes/pkg/api/legacyscheme"
	"k8s.io/kubernetes/pkg/apis/admissionregistration"
	"k8s.io/kubernetes/pkg/apis/admissionregistration/validation"
)

// validatingRuleConfigurationStrategy implements verification logic for validatingRuleConfiguration.
type validatingRuleConfigurationStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

// Strategy is the default logic that applies when creating and updating validatingRuleConfiguration objects.
var Strategy = validatingRuleConfigurationStrategy{legacyscheme.Scheme, names.SimpleNameGenerator}

// NamespaceScoped returns false because validatingRuleConfiguration is cluster-scoped resource.
func (validatingRuleConfigurationStrategy) NamespaceScoped() bool {
	return false
}

// PrepareForCreate clears the status of an validatingRuleConfiguration before creation.
func (validatingRuleConfigurationStrategy) PrepareForCreate(ctx context.Context, obj runtime.Object) {
	ic := obj.(*admissionregistration.ValidatingRuleConfiguration)
	ic.Generation = 1
}

// WarningsOnCreate returns warnings for the creation of the given object.
func (validatingRuleConfigurationStrategy) WarningsOnCreate(ctx context.Context, obj runtime.Object) []string {
	return nil
}

// PrepareForUpdate clears fields that are not allowed to be set by end users on update.
func (validatingRuleConfigurationStrategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
	newIC := obj.(*admissionregistration.ValidatingRuleConfiguration)
	oldIC := old.(*admissionregistration.ValidatingRuleConfiguration)

	// Any changes to the spec increment the generation number, any changes to the
	// status should reflect the generation number of the corresponding object.
	// See metav1.ObjectMeta description for more information on Generation.
	if !reflect.DeepEqual(oldIC.ValidatingRules, newIC.ValidatingRules) {
		newIC.Generation = oldIC.Generation + 1
	}
}

// Validate validates a new validatingRuleConfiguration.
func (validatingRuleConfigurationStrategy) Validate(ctx context.Context, obj runtime.Object) field.ErrorList {
	ic := obj.(*admissionregistration.ValidatingRuleConfiguration)
	return validation.ValidateValidatingRuleConfiguration(ic)
}

// Canonicalize normalizes the object after validation.
func (validatingRuleConfigurationStrategy) Canonicalize(obj runtime.Object) {
}

// AllowCreateOnUpdate is true for validatingRuleConfiguration; this means you may create one with a PUT request.
func (validatingRuleConfigurationStrategy) AllowCreateOnUpdate() bool {
	return false
}

// ValidateUpdate is the default update validation for an end user.
func (validatingRuleConfigurationStrategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	return validation.ValidateValidatingRuleConfigurationUpdate(obj.(*admissionregistration.ValidatingRuleConfiguration), old.(*admissionregistration.ValidatingRuleConfiguration))
}

// WarningsOnUpdate returns warnings for the given update.
func (validatingRuleConfigurationStrategy) WarningsOnUpdate(ctx context.Context, obj, old runtime.Object) []string {
	return nil
}

// AllowUnconditionalUpdate is the default update policy for validatingRuleConfiguration objects. Status update should
// only be allowed if version match.
func (validatingRuleConfigurationStrategy) AllowUnconditionalUpdate() bool {
	return false
}
