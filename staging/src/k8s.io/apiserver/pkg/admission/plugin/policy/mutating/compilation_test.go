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

package mutating

import (
	"context"
	"github.com/google/go-cmp/cmp"
	"strings"
	"testing"
	"time"

	"k8s.io/api/admissionregistration/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/admission/plugin/policy/mutating/patch"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/cel/openapi/resolver"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/openapi/openapitest"
	"k8s.io/kubernetes/pkg/generated/openapi"
	"k8s.io/utils/ptr"
)

// TestCompilation is an open-box test of mutatingEvaluator.compile
// However, the result is a set of CEL programs, manually invoke them to assert
// on the results.
func TestCompilation(t *testing.T) {
	deploymentGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	deploymentGVK := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	testCases := []struct {
		name           string
		policy         *Policy
		gvr            schema.GroupVersionResource
		object         runtime.Object
		oldObject      runtime.Object
		params         runtime.Object
		namespace      *corev1.Namespace
		expectedErr    string
		expectedResult runtime.Object
	}{
		{
			name: "applyConfiguration then jsonPatch",
			policy: mutations(policy("d1"), v1alpha1.Mutation{
				PatchType: v1alpha1.PatchTypeApplyConfiguration,
				ApplyConfiguration: &v1alpha1.ApplyConfiguration{
					Expression: `Object{
									spec: Object.spec{
										replicas: object.spec.replicas + 100
									}
								}`,
				},
			},
				v1alpha1.Mutation{
					PatchType: v1alpha1.PatchTypeJSONPatch,
					JSONPatch: &v1alpha1.JSONPatch{
						Expression: `[
							JSONPatch{op: "replace", path: "/spec/replicas", value: object.spec.replicas + 10}
						]`,
					},
				}),
			gvr:            deploymentGVR,
			object:         &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)}},
			expectedResult: &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](111)}},
		},
		{
			name: "jsonPatch then applyConfiguration",
			policy: mutations(policy("d1"),
				v1alpha1.Mutation{
					PatchType: v1alpha1.PatchTypeJSONPatch,
					JSONPatch: &v1alpha1.JSONPatch{
						Expression: `[
							JSONPatch{op: "replace", path: "/spec/replicas", value: object.spec.replicas + 10}
						]`,
					},
				},
				v1alpha1.Mutation{
					PatchType: v1alpha1.PatchTypeApplyConfiguration,
					ApplyConfiguration: &v1alpha1.ApplyConfiguration{
						Expression: `Object{
									spec: Object.spec{
										replicas: object.spec.replicas + 100
									}
								}`,
					},
				}),
			gvr:            deploymentGVR,
			object:         &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)}},
			expectedResult: &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](111)}},
		},
		{
			name: "jsonPatch with variable",
			policy: jsonPatches(variables(policy("d1"), v1alpha1.Variable{Name: "desired", Expression: "10"}), v1alpha1.JSONPatch{
				Expression: `[
					JSONPatch{op: "replace", path: "/spec/replicas", value: variables.desired + 1}, 
				]`,
			}),
			gvr:            deploymentGVR,
			object:         &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)}},
			expectedResult: &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](11)}},
		},
		{
			name: "apply configuration with variable",
			policy: applyConfigurations(variables(policy("d1"), v1alpha1.Variable{Name: "desired", Expression: "10"}),
				`Object{
					spec: Object.spec{
						replicas: variables.desired + 1
					}
				}`),
			gvr:            deploymentGVR,
			object:         &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)}},
			expectedResult: &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](11)}},
		},
		{
			name: "complex apply configuration initialization",
			policy: applyConfigurations(policy("d1"),
				`Object{
					spec: Object.spec{
						replicas: 1,
						template: Object.spec.template{
							metadata: Object.spec.template.metadata{
								labels: {"app": "nginx"}
							},
							spec: Object.spec.template.spec{
								containers: [Object.spec.template.spec.containers{
									name: "nginx",
									image: "nginx:1.14.2",
									ports: [Object.spec.template.spec.containers.ports{
										containerPort: 80
									}],
									resources: Object.spec.template.spec.containers.resources{
										limits: {"cpu": "128M"},
									}
								}]
							}
						}
					}
				}`),

			gvr:    deploymentGVR,
			object: &appsv1.Deployment{Spec: appsv1.DeploymentSpec{}},
			expectedResult: &appsv1.Deployment{Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To[int32](1),
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"app": "nginx"},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "nginx",
							Image: "nginx:1.14.2",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 80},
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{corev1.ResourceName("cpu"): resource.MustParse("128M")},
							},
						}},
					},
				},
			}},
		},
		{
			name: "apply configuration with invalid type name",
			policy: applyConfigurations(policy("d1"),
				`Object{
					spec: Object.specx{
						replicas: 1
					}
				}`),
			gvr:         deploymentGVR,
			object:      &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)}},
			expectedErr: "type mismatch: unexpected type name \"Object.specx\", expected \"Object.spec\", which matches field name path from root Object type",
		},
		{
			name: "apply configuration with invalid field name",
			policy: applyConfigurations(policy("d1"),
				`Object{
					spec: Object.spec{
						replicas: int(params.data['k1'])
					}
				}`),
			gvr:         deploymentGVR,
			object:      &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)}},
			expectedErr: "error applying patch: failed to convert patch object to typed object: .spec.replicasx: field not declared in schema",
		},
		{
			name: "apply configuration with invalid return type",
			policy: applyConfigurations(policy("d1"),
				`"I'm a teapot!"`),
			gvr:         deploymentGVR,
			object:      &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)}},
			expectedErr: "must evaluate to Object but got string",
		},
		{
			name: "apply configuration with invalid initializer return type",
			policy: applyConfigurations(policy("d1"),
				`Object.spec.metadata{}`),
			gvr:         deploymentGVR,
			object:      &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)}},
			expectedErr: "must evaluate to Object but got Object.spec.metadata",
		},
		{
			name: "jsonPatch with excessive cost",
			policy: jsonPatches(variables(policy("d1"), v1alpha1.Variable{Name: "list", Expression: "[0,1,2,3,4,5,6,7,8,9]"}), v1alpha1.JSONPatch{
				Expression: `[
					JSONPatch{op: "replace", path: "/spec/replicas", 
						value: variables.list.all(x1, variables.list.all(x2, variables.list.all(x3, variables.list.all(x4, variables.list.all(x5, variables.list.all(x5, "0123456789" == "0123456789"))))))? 1 : 0
					}
				]`,
			}),
			gvr:         deploymentGVR,
			object:      &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)}},
			expectedErr: "operation cancelled: actual cost limit exceeded",
		},
		{
			name: "applyConfiguration with excessive cost",
			policy: variables(applyConfigurations(policy("d1"),
				`Object{
					spec: Object.spec{
						replicas: variables.list.all(x1, variables.list.all(x2, variables.list.all(x3, variables.list.all(x4, variables.list.all(x5, variables.list.all(x5, "0123456789" == "0123456789"))))))? 1 : 0
					}
				}`), v1alpha1.Variable{Name: "list", Expression: "[0,1,2,3,4,5,6,7,8,9]"}),
			gvr:         deploymentGVR,
			object:      &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)}},
			expectedErr: "operation cancelled: actual cost limit exceeded",
		},
		{
			name: "request variable",
			policy: jsonPatches(policy("d1"), v1alpha1.JSONPatch{
				Expression: `[
					JSONPatch{op: "replace", path: "/spec/replicas", 
						value: request.kind.group == 'apps' && request.kind.version == 'v1' && request.kind.kind == 'Deployment' ? 10 : 0
					}
				]`}),
			gvr:            deploymentGVR,
			object:         &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)}},
			expectedResult: &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](10)}},
		},
		{
			name: "namespace request variable",
			policy: jsonPatches(policy("d1"), v1alpha1.JSONPatch{
				Expression: `[
					JSONPatch{op: "replace", path: "/spec/replicas", 
						value: namespaceObject.metadata.name == 'ns1' ? 10 : 0
					}
				]`}),
			namespace:      &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1"}},
			gvr:            deploymentGVR,
			object:         &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)}},
			expectedResult: &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](10)}},
		},
		{
			name: "authorizer check",
			policy: jsonPatches(policy("d1"), v1alpha1.JSONPatch{
				Expression: `[
					JSONPatch{op: "replace", path: "/spec/replicas", 
						value: authorizer.group('').resource('endpoints').check('create').allowed() ? 10 : 0
					}
				]`,
			}),
			gvr:            deploymentGVR,
			object:         &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)}},
			expectedResult: &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](10)}},
		},
		{
			name: "object type has field access",
			policy: jsonPatches(policy("d1"), v1alpha1.JSONPatch{
				Expression: `[
					JSONPatch{
						op: "add", path: "/metadata/labels",
						value: {
							"value": Object.metadata{name: "fieldValue"}.name,
						}
					}
				]`,
			}),
			gvr:    deploymentGVR,
			object: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{}}},
			expectedResult: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
				"value": "fieldValue",
			}}},
		},
		{
			name: "object type has field testing",
			policy: jsonPatches(policy("d1"), v1alpha1.JSONPatch{
				Expression: `[
					JSONPatch{
						op: "add", path: "/metadata/labels",
						value: {
							"field": string(has(Object.metadata{name: "fieldValue"}.name)),
							"field-unset": string(has(Object.metadata{}.name)),
						}
					}
				]`,
			}),
			gvr:    deploymentGVR,
			object: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{}}},
			expectedResult: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
				"field":       "true",
				"field-unset": "false",
			}}},
		},
		{
			name: "object type equality",
			policy: jsonPatches(policy("d1"), v1alpha1.JSONPatch{
				Expression: `[
					JSONPatch{
						op: "add", path: "/metadata/labels",
						value: {
							"empty": string(Object{} == Object{}),
							"same": string(Object.metadata{name: "x"} == Object.metadata{name: "x"}),
							"different": string(Object.metadata{name: "x"} == Object.metadata{name: "y"}),
						}
					}
				]`,
			}),
			gvr:    deploymentGVR,
			object: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{}}},
			expectedResult: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
				"empty":     "true",
				"same":      "true",
				"different": "false",
			}}},
		},
		{
			// TODO: This test documents existing behavior that we should be fixed before
			// MutatingAdmissionPolicy graduates to beta.
			// It is possible to initialize invalid Object types because we do not yet perform
			// a full compilation pass with the types fully bound.  Before beta, we should
			// recompile all expressions with fully bound types before evaluation and report
			// errors if invalid Object types like this are initialized.
			name: "object types are not fully type checked",
			policy: jsonPatches(policy("d1"), v1alpha1.JSONPatch{
				Expression: `[
					JSONPatch{
						op: "add", path: "/spec",
						value: Object.invalid{replicas: 1}
					}
				]`,
			}),
			gvr:    deploymentGVR,
			object: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{}}},
			expectedResult: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{}},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To[int32](1),
				},
			},
		},
		{
			name: "object type checking",
			policy: jsonPatches(policy("d1"), v1alpha1.JSONPatch{
				Expression: `[
					JSONPatch{
						op: "add", path: "/metadata/labels",
						value: {
							"same": string(type(Object.metadata{}) == type(object.metadata)),
							"different": string(type(Object.spec{}) == type(object.metadata)),
							"same-list": string(type([Object.spec.template.spec.containers{}]) == type(object.spec.template.spec.containers)),
							"different-list": string(type([Object.spec{}]) == type(object.spec.template.spec.containers)),
						}
					}
				]`,
			}),
			gvr: deploymentGVR,
			object: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{}},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name: "nginx",
							}},
						},
					},
				},
			},
			expectedResult: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
				"same":           "true",
				"different":      "false",
				"same-list":      "true",
				"different-list": "false",
			}},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name: "nginx",
							}},
						},
					},
				},
			},
		},
	}

	scheme := runtime.NewScheme()
	err := appsv1.AddToScheme(scheme)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	tcManager := patch.NewTypeConverterManager(nil, openapitest.NewEmbeddedFileClient())
	go tcManager.Run(ctx)

	err = wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, time.Second, true, func(context.Context) (done bool, err error) {
		converter := tcManager.GetTypeConverter(deploymentGVK)
		return converter != nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	schemaResolver := resolver.NewDefinitionsSchemaResolver(openapi.GetOpenAPIDefinitions, k8sscheme.Scheme)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var gvk schema.GroupVersionKind
			gvks, _, err := scheme.ObjectKinds(tc.object)
			if err != nil {
				t.Fatal(err)
			}
			if len(gvks) == 1 {
				gvk = gvks[0]
			} else {
				t.Fatalf("Failed to find gvk for type: %T", tc.object)
			}

			policyEvaluator := compilePolicy(tc.policy)
			if policyEvaluator.CompositionEnv != nil {
				ctx = policyEvaluator.CompositionEnv.CreateContext(ctx)
			}
			obj := tc.object

			typeAccessor, err := meta.TypeAccessor(obj)
			if err != nil {
				t.Fatal(err)
			}
			typeAccessor.SetKind(gvk.Kind)
			typeAccessor.SetAPIVersion(gvk.GroupVersion().String())
			typeConverter := tcManager.GetTypeConverter(gvk)

			metaAccessor, err := meta.Accessor(obj)
			if err != nil {
				t.Fatal(err)
			}

			for _, patcher := range policyEvaluator.Mutators {
				attrs := admission.NewAttributesRecord(obj, tc.oldObject, gvk,
					metaAccessor.GetNamespace(), metaAccessor.GetName(), tc.gvr,
					"", admission.Create, &metav1.CreateOptions{}, false, nil)
				vAttrs := &admission.VersionedAttributes{
					Attributes:         attrs,
					VersionedKind:      gvk,
					VersionedObject:    obj,
					VersionedOldObject: tc.oldObject,
				}
				r := patch.Request{
					MatchedResource:     tc.gvr,
					VersionedAttributes: vAttrs,
					ObjectInterfaces:    admission.NewObjectInterfacesFromScheme(scheme),
					OptionalVariables:   cel.OptionalVariableBindings{VersionedParams: tc.params, Authorizer: fakeAuthorizer{}},
					Namespace:           tc.namespace,
					TypeConverter:       typeConverter,
					SchemaResolver:      schemaResolver,
				}
				obj, err = patcher.Patch(ctx, r, celconfig.RuntimeCELCostBudget)
				if len(tc.expectedErr) > 0 {
					if err == nil {
						t.Fatalf("expected error: %s", tc.expectedErr)
					} else {
						if !strings.Contains(err.Error(), tc.expectedErr) {
							t.Fatalf("expected error: %s, got: %s", tc.expectedErr, err.Error())
						}
						return
					}
				}
				if err != nil && len(tc.expectedErr) == 0 {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			got, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
			if err != nil {
				t.Fatal(err)
			}

			wantTypeAccessor, err := meta.TypeAccessor(tc.expectedResult)
			if err != nil {
				t.Fatal(err)
			}
			wantTypeAccessor.SetKind(gvk.Kind)
			wantTypeAccessor.SetAPIVersion(gvk.GroupVersion().String())

			want, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tc.expectedResult)
			if err != nil {
				t.Fatal(err)
			}
			if !equality.Semantic.DeepEqual(want, got) {
				t.Errorf("unexpected result, got diff:\n%s\n", cmp.Diff(want, got))
			}
		})
	}
}

func policy(name string) *v1alpha1.MutatingAdmissionPolicy {
	return &v1alpha1.MutatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1alpha1.MutatingAdmissionPolicySpec{},
	}
}

func variables(policy *v1alpha1.MutatingAdmissionPolicy, variables ...v1alpha1.Variable) *v1alpha1.MutatingAdmissionPolicy {
	policy.Spec.Variables = append(policy.Spec.Variables, variables...)
	return policy
}

func jsonPatches(policy *v1alpha1.MutatingAdmissionPolicy, jsonPatches ...v1alpha1.JSONPatch) *v1alpha1.MutatingAdmissionPolicy {
	for _, jsonPatch := range jsonPatches {
		policy.Spec.Mutations = append(policy.Spec.Mutations, v1alpha1.Mutation{
			JSONPatch: &jsonPatch,
			PatchType: v1alpha1.PatchTypeJSONPatch,
		})
	}

	return policy
}

func applyConfigurations(policy *v1alpha1.MutatingAdmissionPolicy, expressions ...string) *v1alpha1.MutatingAdmissionPolicy {
	for _, expression := range expressions {
		policy.Spec.Mutations = append(policy.Spec.Mutations, v1alpha1.Mutation{
			ApplyConfiguration: &v1alpha1.ApplyConfiguration{Expression: expression},
			PatchType:          v1alpha1.PatchTypeApplyConfiguration,
		})
	}
	return policy
}

func paramKind(policy *v1alpha1.MutatingAdmissionPolicy, paramKind *v1alpha1.ParamKind) *v1alpha1.MutatingAdmissionPolicy {
	policy.Spec.ParamKind = paramKind
	return policy
}

func mutations(policy *v1alpha1.MutatingAdmissionPolicy, mutations ...v1alpha1.Mutation) *v1alpha1.MutatingAdmissionPolicy {
	policy.Spec.Mutations = append(policy.Spec.Mutations, mutations...)
	return policy
}

func matchConstraints(policy *v1alpha1.MutatingAdmissionPolicy, matchConstraints *v1alpha1.MatchResources) *v1alpha1.MutatingAdmissionPolicy {
	policy.Spec.MatchConstraints = matchConstraints
	return policy
}

type fakeAuthorizer struct{}

func (f fakeAuthorizer) Authorize(ctx context.Context, a authorizer.Attributes) (authorizer.Decision, string, error) {
	return authorizer.DecisionAllow, "", nil
}
