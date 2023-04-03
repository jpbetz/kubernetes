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

package validatingadmissionpolicy

import (
	"testing"

	admissionregistrationv1alpha1 "k8s.io/api/admissionregistration/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"k8s.io/apiserver/pkg/cel/apivalidation"
	openapiresolver "k8s.io/apiserver/pkg/cel/openapi/resolver"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	"k8s.io/kubernetes/pkg/apis/admissionregistration"
	"k8s.io/kubernetes/pkg/generated/openapi"

	// Ensure that admissionregistration package is initialized.
	_ "k8s.io/kubernetes/pkg/apis/admissionregistration/install"
)

func TestValidatingAdmissionPolicyStrategy(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.OpenAPIEnums, true)()

	schemaResolver := openapiresolver.NewDefinitionsSchemaResolver(k8sscheme.Scheme, openapi.GetOpenAPIDefinitions)
	declarativeValidator := apivalidation.NewDeclarativeValidator(schemaResolver, celconfig.PerCallLimit)
	strategy := NewStrategy(nil, nil, declarativeValidator)
	ctx := genericapirequest.WithRequestInfo(genericapirequest.NewDefaultContext(),
		&genericapirequest.RequestInfo{
			APIGroup:   admissionregistrationv1alpha1.SchemeGroupVersion.Group,
			APIVersion: admissionregistrationv1alpha1.SchemeGroupVersion.Version,
		},
	)
	if strategy.NamespaceScoped() {
		t.Error("ValidatingAdmissionPolicy strategy must be cluster scoped")
	}
	if strategy.AllowCreateOnUpdate() {
		t.Errorf("ValidatingAdmissionPolicy should not allow create on update")
	}

	configuration := validValidatingAdmissionPolicy()
	strategy.PrepareForCreate(ctx, configuration)
	errs := strategy.Validate(ctx, configuration)
	if len(errs) != 0 {

		t.Errorf("Unexpected error validating %v", errs)
	}
	invalidConfiguration := &admissionregistration.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: ""},
	}
	strategy.PrepareForUpdate(ctx, invalidConfiguration, configuration)
	errs = strategy.ValidateUpdate(ctx, invalidConfiguration, configuration)
	if len(errs) == 0 {
		t.Errorf("Expected a validation error")
	}
}
func validValidatingAdmissionPolicy() *admissionregistration.ValidatingAdmissionPolicy {

	// ignore := admissionregistration.Ignore
	invalid := admissionregistration.FailurePolicyType("invalid")
	return &admissionregistration.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
		Spec: admissionregistration.ValidatingAdmissionPolicySpec{
			ParamKind: &admissionregistration.ParamKind{
				Kind:       "ReplicaLimit",
				APIVersion: "rules.example.com/v1",
			},
			Validations: []admissionregistration.Validation{
				{
					// Expression: "object.spec.replicas <= params.maxReplicas",
					Message: "nope",
				},
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
			AuditAnnotations: []admissionregistration.AuditAnnotation{
				// {
				// 	Key:             "01234567890012345678900123456789001234567890",
				// 	ValueExpression: "'OK'",
				// },
				{Key: "a1", ValueExpression: "'OK'"},
				{Key: "a2", ValueExpression: "'OK'"},
				{Key: "a3", ValueExpression: "'OK'"},
				{Key: "a4", ValueExpression: "'OK'"},
				{Key: "a5", ValueExpression: "'OK'"},
				{Key: "a6", ValueExpression: "'OK'"},
				{Key: "a7", ValueExpression: "'OK'"},
				{Key: "a8", ValueExpression: "'OK'"},
				{Key: "a9", ValueExpression: "'OK'"},
				{Key: "a0", ValueExpression: "'OK'"},

				{Key: "b1", ValueExpression: "'OK'"},
				{Key: "b2", ValueExpression: "'OK'"},
				{Key: "b3", ValueExpression: "'OK'"},
				{Key: "b4", ValueExpression: "'OK'"},
				{Key: "b5", ValueExpression: "'OK'"},
				{Key: "b6", ValueExpression: "'OK'"},
				{Key: "b7", ValueExpression: "'OK'"},
				{Key: "b8", ValueExpression: "'OK'"},
				{Key: "b9", ValueExpression: "'OK'"},
				{Key: "b0", ValueExpression: "'OK'"},

				{Key: "c1", ValueExpression: "'OK'"},
				{Key: "c2", ValueExpression: "'OK'"},
				{Key: "c3", ValueExpression: "'OK'"},
				{Key: "c4", ValueExpression: "'OK'"},
				{Key: "c5", ValueExpression: "'OK'"},
				{Key: "c6", ValueExpression: "'OK'"},
				{Key: "c7", ValueExpression: "'OK'"},
				{Key: "c8", ValueExpression: "'OK'"},
				{Key: "c9", ValueExpression: "'OK'"},
				{Key: "c0", ValueExpression: "'OK'"},
			},
			FailurePolicy: &invalid,
		},
	}
}
