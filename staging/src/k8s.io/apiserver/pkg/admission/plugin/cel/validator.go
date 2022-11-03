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

package cel

import (
	"fmt"
	"reflect"
	"strings"

	celtypes "github.com/google/cel-go/common/types"
	"github.com/google/cel-go/interpreter"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/api/admissionregistration/v1alpha1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/admission/plugin/cel/matching"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

var _ ValidatorCompiler = &CELValidatorCompiler{}
var _ matching.MatchCriteria = &matchCriteria{}

type matchCriteria struct {
	constraints *v1alpha1.MatchResources
}

// GetParsedNamespaceSelector returns the converted LabelSelector which implements labels.Selector
func (m *matchCriteria) GetParsedNamespaceSelector() (labels.Selector, error) {
	return metav1.LabelSelectorAsSelector(m.constraints.NamespaceSelector)
}

// GetParsedObjectSelector returns the converted LabelSelector which implements labels.Selector
func (m *matchCriteria) GetParsedObjectSelector() (labels.Selector, error) {
	return metav1.LabelSelectorAsSelector(m.constraints.ObjectSelector)
}

// GetMatchResources returns the matchConstraints
func (m *matchCriteria) GetMatchResources() v1alpha1.MatchResources {
	return *m.constraints
}

// CELValidatorCompiler implement the interface ValidatorCompiler.
type CELValidatorCompiler struct {
	Matcher *matching.Matcher
}

// SetExternalKubeInformerFactory registers the namespaceLister into Matcher.
func (c *CELValidatorCompiler) SetExternalKubeInformerFactory(factory informers.SharedInformerFactory) {
	c.Matcher.SetNamespaceLister(factory.Core().V1().Namespaces().Lister())
}

// SetExternalKubeClientSet registers client into Matcher
func (c *CELValidatorCompiler) SetExternalKubeClientSet(client kubernetes.Interface) {
	c.Matcher.SetExternalKubeClientSet(client)
}

// DefinitionMatches returns whether this ValidatingAdmissionPolicy matches the provided admission resource request
func (c *CELValidatorCompiler) DefinitionMatches(definition *v1alpha1.ValidatingAdmissionPolicy, a admission.Attributes, o admission.ObjectInterfaces) (bool, error) {
	criteria := matchCriteria{constraints: definition.Spec.MatchConstraints}
	return c.Matcher.Matches(&criteria, a, o)
}

// BindingMatches returns whether this ValidatingAdmissionPolicyBinding matches the provided admission resource request
func (c *CELValidatorCompiler) BindingMatches(binding *v1alpha1.ValidatingAdmissionPolicyBinding, a admission.Attributes, o admission.ObjectInterfaces) (bool, error) {
	if binding.Spec.MatchResources == nil {
		return true, nil
	}
	criteria := matchCriteria{constraints: binding.Spec.MatchResources}
	return c.Matcher.Matches(&criteria, a, o)
}

// ValidateInitialization checks if Matcher is initialized.
func (c *CELValidatorCompiler) ValidateInitialization() error {
	return c.Matcher.ValidateInitialization()
}

type validationActivation struct {
	object, oldObject, params, request interface{}
}

// ResolveName returns a value from the activation by qualified name, or false if the name
// could not be found.
func (a *validationActivation) ResolveName(name string) (interface{}, bool) {
	switch name {
	case ObjectVarName:
		return a.object, true
	case OldObjectVarName:
		return a.oldObject, true
	case ParamsVarName:
		return a.params, true
	case RequestVarName:
		return a.request, true
	default:
		return nil, false
	}
}

// Parent returns the parent of the current activation, may be nil.
// If non-nil, the parent will be searched during resolve calls.
func (a *validationActivation) Parent() interpreter.Activation {
	return nil
}

// Compile compiles the cel expression defined in ValidatingAdmissionPolicy
func (c *CELValidatorCompiler) Compile(p *v1alpha1.ValidatingAdmissionPolicy) Validator {
	if len(p.Spec.Validations) == 0 {
		return nil
	}
	hasParam := false
	if p.Spec.ParamKind != nil {
		hasParam = true
	}
	compilationResults := make([]CompilationResult, len(p.Spec.Validations))
	for i, validation := range p.Spec.Validations {
		compilationResults[i] = CompileValidatingPolicyExpression(validation.Expression, hasParam)
	}
	return &CELValidator{policy: p, compilationResults: compilationResults}
}

// CELValidator implements the Validator interface
type CELValidator struct {
	policy             *v1alpha1.ValidatingAdmissionPolicy
	compilationResults []CompilationResult
}

func convertObjectToUnstructured(obj interface{}) (*unstructured.Unstructured, error) {
	if obj == nil || reflect.ValueOf(obj).IsNil() {
		return &unstructured.Unstructured{Object: nil}, nil
	}
	ret, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: ret}, nil
}

func objectToResolveVal(r runtime.Object) (interface{}, error) {
	if r == nil {
		return nil, nil
	}
	v, err := convertObjectToUnstructured(r)
	if err != nil {
		return nil, err
	}
	return v.Object, nil
}

func policyDecisionKindForError(f v1alpha1.FailurePolicyType) policyDecisionKind {
	if f == v1alpha1.Ignore {
		return admit
	}
	return deny
}

// Validate validates all cel expressions in Validator and returns a PolicyDecision for each CEL expression or returns an error.
// An error will be returned if failed to convert the object/oldObject/params/request to unstructured.
// Each PolicyDecision will have a decision and a message.
// policyDecision.message will be empty if the decision is allowed and no error met.
func (v *CELValidator) Validate(a admission.Attributes, o admission.ObjectInterfaces, params runtime.Object) ([]policyDecision, error) {
	// TODO: replace unstructured with ref.Val for CEL variables when native type support is available

	decisions := make([]policyDecision, len(v.compilationResults))
	var err error
	oldObjectVal, err := objectToResolveVal(a.GetOldObject())
	if err != nil {
		return nil, err
	}
	objectVal, err := objectToResolveVal(a.GetObject())
	if err != nil {
		return nil, err
	}
	paramsVal, err := objectToResolveVal(params)
	if err != nil {
		return nil, err
	}
	request := createAdmissionRequest(a)
	requestVal, err := convertObjectToUnstructured(request)
	if err != nil {
		return nil, err
	}
	va := &validationActivation{
		object:    objectVal,
		oldObject: oldObjectVal,
		params:    paramsVal,
		request:   requestVal.Object,
	}

	var f v1alpha1.FailurePolicyType
	if v.policy.Spec.FailurePolicy == nil {
		f = v1alpha1.Fail
	} else {
		f = *v.policy.Spec.FailurePolicy
	}

	for i, compilationResult := range v.compilationResults {
		validation := v.policy.Spec.Validations[i]

		var policyDecision = &decisions[i]

		if compilationResult.Error != nil {
			policyDecision.kind = policyDecisionKindForError(f)
			policyDecision.message = fmt.Sprintf("compilation error: %v", compilationResult.Error)
			continue
		}
		if compilationResult.Program == nil {
			policyDecision.kind = policyDecisionKindForError(f)
			policyDecision.message = "unexpected internal error compiling expression"
			continue
		}
		evalResult, _, err := compilationResult.Program.Eval(va)
		if err != nil {
			policyDecision.kind = policyDecisionKindForError(f)
			policyDecision.message = fmt.Sprintf("expression '%v' resulted in error: %v", v.policy.Spec.Validations[i].Expression, err)
		} else if evalResult != celtypes.True {
			policyDecision.kind = deny
			if validation.Reason == nil {
				policyDecision.reason = metav1.StatusReasonInvalid
			} else {
				policyDecision.reason = *validation.Reason
			}
			if len(validation.Message) > 0 {
				policyDecision.message = strings.TrimSpace(validation.Message)
			} else {
				policyDecision.message = fmt.Sprintf("failed expression: %v", strings.TrimSpace(validation.Expression))
			}
		} else {
			policyDecision.kind = admit
		}
	}

	return decisions, nil
}

func createAdmissionRequest(attr admission.Attributes) *admissionv1.AdmissionRequest {
	gvk := attr.GetKind()
	gvr := attr.GetResource()
	subresource := attr.GetSubresource()

	// FIXME: how to get resource GVK, GVR and subresource?
	requestGVK := attr.GetKind()
	requestGVR := attr.GetResource()
	requestSubResource := attr.GetSubresource()

	aUserInfo := attr.GetUserInfo()
	var userInfo authenticationv1.UserInfo
	if aUserInfo != nil {
		userInfo = authenticationv1.UserInfo{
			Extra:    make(map[string]authenticationv1.ExtraValue),
			Groups:   aUserInfo.GetGroups(),
			UID:      aUserInfo.GetUID(),
			Username: aUserInfo.GetName(),
		}
		// Convert the extra information in the user object
		for key, val := range aUserInfo.GetExtra() {
			userInfo.Extra[key] = authenticationv1.ExtraValue(val)
		}
	}

	dryRun := attr.IsDryRun()

	return &admissionv1.AdmissionRequest{
		Kind: metav1.GroupVersionKind{
			Group:   gvk.Group,
			Kind:    gvk.Kind,
			Version: gvk.Version,
		},
		Resource: metav1.GroupVersionResource{
			Group:    gvr.Group,
			Resource: gvr.Resource,
			Version:  gvr.Version,
		},
		SubResource: subresource,
		RequestKind: &metav1.GroupVersionKind{
			Group:   requestGVK.Group,
			Kind:    requestGVK.Kind,
			Version: requestGVK.Version,
		},
		RequestResource: &metav1.GroupVersionResource{
			Group:    requestGVR.Group,
			Resource: requestGVR.Resource,
			Version:  requestGVR.Version,
		},
		RequestSubResource: requestSubResource,
		Name:               attr.GetName(),
		Namespace:          attr.GetNamespace(),
		Operation:          admissionv1.Operation(attr.GetOperation()),
		UserInfo:           userInfo,
		// Leave Object and OldObject unset since we don't provide access to them via request
		DryRun: &dryRun,
		Options: runtime.RawExtension{
			Object: attr.GetOperationOptions(),
		},
	}
}
