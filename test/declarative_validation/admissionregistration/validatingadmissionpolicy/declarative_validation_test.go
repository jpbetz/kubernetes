/*
Copyright 2025 The Kubernetes Authors.

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

package validatingadmissionpolicy

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	apitesting "k8s.io/kubernetes/pkg/api/testing"
	"k8s.io/kubernetes/pkg/apis/admissionregistration"
	"k8s.io/kubernetes/pkg/registry/admissionregistration/resolver"
	registry "k8s.io/kubernetes/pkg/registry/admissionregistration/validatingadmissionpolicy"
)

var resourceResolver resolver.ResourceResolverFunc = func(gvk schema.GroupVersionKind) (schema.GroupVersionResource, error) {
	return schema.GroupVersionResource{
		Group:    "rules.example.com",
		Version:  "v1",
		Resource: "replicalimits",
	}, nil
}

func TestDeclarativeValidate(t *testing.T) {
	for _, apiVersion := range apiVersions {
		t.Run(apiVersion, func(t *testing.T) {
			testDeclarativeValidate(t, apiVersion)
		})
	}
}

func testDeclarativeValidate(t *testing.T, apiVersion string) {
	ctx := genericapirequest.WithRequestInfo(genericapirequest.NewDefaultContext(), &genericapirequest.RequestInfo{
		APIGroup:          "admissionregistration.k8s.io",
		APIVersion:        apiVersion,
		Resource:          "validatingadmissionpolicies",
		IsResourceRequest: true,
		Verb:              "create",
	})

	strategy := registry.NewStrategy(nil, resourceResolver)

	testCases := map[string]struct {
		input        admissionregistration.ValidatingAdmissionPolicy
		expectedErrs field.ErrorList
	}{
		"valid": {
			input: mkValidPolicy(),
		},
		"spec.paramKind.kind: required": {
			input: mkValidPolicy(tweakParamKindKind("")),
			expectedErrs: field.ErrorList{
				field.Required(field.NewPath("spec", "paramKind", "kind"), "").MarkAlpha(),
			},
		},
		"spec.paramKind.kind: too long": {
			input: mkValidPolicy(tweakParamKindKind("abcdef")),
			expectedErrs: field.ErrorList{
				field.TooLong(field.NewPath("spec", "paramKind", "kind"), "", 4).WithOrigin("maxLength").MarkAlpha(),
			},
		},
	}
	for k, tc := range testCases {
		t.Run(k, func(t *testing.T) {
			apitesting.VerifyValidationEquivalence(t, ctx, &tc.input, strategy, tc.expectedErrs)
		})
	}
}

func TestDeclarativeValidateUpdate(t *testing.T) {
	for _, apiVersion := range apiVersions {
		t.Run(apiVersion, func(t *testing.T) {
			testDeclarativeValidateUpdate(t, apiVersion)
		})
	}
}

func testDeclarativeValidateUpdate(t *testing.T, apiVersion string) {
	ctx := genericapirequest.WithRequestInfo(genericapirequest.NewDefaultContext(), &genericapirequest.RequestInfo{
		APIPrefix:         "apis",
		APIGroup:          "admissionregistration.k8s.io",
		APIVersion:        apiVersion,
		Resource:          "validatingadmissionpolicies",
		Name:              "valid-policy",
		IsResourceRequest: true,
		Verb:              "update",
	})

	strategy := registry.NewStrategy(nil, resourceResolver)

	testCases := map[string]struct {
		oldObj       admissionregistration.ValidatingAdmissionPolicy
		updateObj    admissionregistration.ValidatingAdmissionPolicy
		expectedErrs field.ErrorList
	}{
		"valid update": {
			oldObj:    mkValidPolicy(),
			updateObj: mkValidPolicy(),
		},
		"update with spec.paramKind.kind too long": {
			oldObj:    mkValidPolicy(),
			updateObj: mkValidPolicy(tweakParamKindKind("abcdef")),
			expectedErrs: field.ErrorList{
				field.TooLong(field.NewPath("spec", "paramKind", "kind"), "", 4).WithOrigin("maxLength").MarkAlpha(),
			},
		},
	}
	for k, tc := range testCases {
		t.Run(k, func(t *testing.T) {
			apitesting.VerifyUpdateValidationEquivalence(t, ctx, &tc.updateObj, &tc.oldObj, strategy, tc.expectedErrs)
		})
	}
}

func mkValidPolicy(tweaks ...func(obj *admissionregistration.ValidatingAdmissionPolicy)) admissionregistration.ValidatingAdmissionPolicy {
	ignore := admissionregistration.Ignore
	obj := admissionregistration.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: "valid-policy",
		},
		Spec: admissionregistration.ValidatingAdmissionPolicySpec{
			ParamKind: &admissionregistration.ParamKind{
				Kind:       "abcd",
				APIVersion: "rules.example.com/v1",
			},
			Validations: []admissionregistration.Validation{
				{Expression: "object.spec.replicas <= params.maxReplicas"},
			},
			MatchConstraints: &admissionregistration.MatchResources{
				MatchPolicy: func() *admissionregistration.MatchPolicyType {
					r := admissionregistration.MatchPolicyType("Exact")
					return &r
				}(),
				ObjectSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"a": "b"},
				},
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"a": "b"},
				},
				ResourceRules: []admissionregistration.NamedRuleWithOperations{
					{
						RuleWithOperations: admissionregistration.RuleWithOperations{
							Operations: []admissionregistration.OperationType{"CREATE"},
							Rule: admissionregistration.Rule{
								APIGroups:   []string{"a"},
								APIVersions: []string{"a"},
								Resources:   []string{"a"},
							},
						},
					},
				},
			},
			FailurePolicy: &ignore,
		},
	}
	obj.ResourceVersion = "1"
	for _, tweak := range tweaks {
		tweak(&obj)
	}
	return obj
}

func tweakParamKindKind(kind string) func(obj *admissionregistration.ValidatingAdmissionPolicy) {
	return func(obj *admissionregistration.ValidatingAdmissionPolicy) {
		obj.Spec.ParamKind.Kind = kind
	}
}
