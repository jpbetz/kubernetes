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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Rule is a tuple of APIGroups, APIVersion, and Resources.It is recommended
// to make sure that all the tuple expansions are valid.
type Rule struct {
	// APIGroups is the API groups the resources belong to. '*' is all groups.
	// If '*' is present, the length of the slice must be one.
	// Required.
	APIGroups []string `json:"apiGroups,omitempty" protobuf:"bytes,1,rep,name=apiGroups"`

	// APIVersions is the API versions the resources belong to. '*' is all versions.
	// If '*' is present, the length of the slice must be one.
	// Required.
	APIVersions []string `json:"apiVersions,omitempty" protobuf:"bytes,2,rep,name=apiVersions"`

	// Resources is a list of resources this rule applies to.
	//
	// For example:
	// 'pods' means pods.
	// 'pods/log' means the log subresource of pods.
	// '*' means all resources, but not subresources.
	// 'pods/*' means all subresources of pods.
	// '*/scale' means all scale subresources.
	// '*/*' means all resources and their subresources.
	//
	// If wildcard is present, the validation rule will ensure resources do not
	// overlap with each other.
	//
	// Depending on the enclosing object, subresources might not be allowed.
	// Required.
	Resources []string `json:"resources,omitempty" protobuf:"bytes,3,rep,name=resources"`

	// scope specifies the scope of this rule.
	// Valid values are "Cluster", "Namespaced", and "*"
	// "Cluster" means that only cluster-scoped resources will match this rule.
	// Namespace API objects are cluster-scoped.
	// "Namespaced" means that only namespaced resources will match this rule.
	// "*" means that there are no scope restrictions.
	// Subresources match the scope of their parent resource.
	// Default is "*".
	//
	// +optional
	Scope *ScopeType `json:"scope,omitempty" protobuf:"bytes,4,rep,name=scope"`
}

// ScopeType specifies a scope for a Rule.
// +enum
type ScopeType string

const (
	// ClusterScope means that scope is limited to cluster-scoped objects.
	// Namespace objects are cluster-scoped.
	ClusterScope ScopeType = "Cluster"
	// NamespacedScope means that scope is limited to namespaced objects.
	NamespacedScope ScopeType = "Namespaced"
	// AllScopes means that all scopes are included.
	AllScopes ScopeType = "*"
)

// FailurePolicyType specifies a failure policy that defines how unrecognized errors from the admission endpoint are handled.
// +enum
type FailurePolicyType string

const (
	// Ignore means that an error calling the webhook is ignored.
	Ignore FailurePolicyType = "Ignore"
	// Fail means that an error calling the webhook causes the admission to fail.
	Fail FailurePolicyType = "Fail"
)

// MatchPolicyType specifies the type of match policy.
// +enum
type MatchPolicyType string

const (
	// Exact means requests should only be sent to the webhook if they exactly match a given rule.
	Exact MatchPolicyType = "Exact"
	// Equivalent means requests should be sent to the webhook if they modify a resource listed in rules via another API group or version.
	Equivalent MatchPolicyType = "Equivalent"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ValidatingAdmissionPolicy describes the definition of an admission validation policy that accepts or rejects and object without changing it.
type ValidatingAdmissionPolicy struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object metadata; More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	// Specification of the desired behavior of the AdmissionPolicy.
	// +optional
	Spec ValidatingAdmissionPolicySpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ValidatingAdmissionPolicyList is a list of ValidatingAdmissionPolicy.
type ValidatingAdmissionPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	// List of ValidatingAdmissionPolicy.
	Items []ValidatingAdmissionPolicy `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// ValidatingAdmissionPolicySpec is the specification of the desired behavior of the AdmissionPolicy.
type ValidatingAdmissionPolicySpec struct {
	//ParamSource specifies the kind of resources used to parameterize this policy which has to be a CRD.
	// +optional
	ParamSource ParamSource `json:"paramSource,omitempty" protobuf:"bytes,1,rep,name=paramSource"`

	// MatchConstraints specifies what resources this policy is designed to validate.
	// The AdmissionPolicy cares about a validation if it matches _any_ Constriant.
	// However, in order to prevent clusters from being put into a unustable state that cannot be recoverd from via the API
	// alidatingAdmissionPolicy cannot match ValidatingAdmissionPolicy/PolicyBinding/param resources.
	// ValidatingWebhookConfiguration cannot match MutatingWebhookConfiguration or ValidatingAdmissionPolicy/PolicyBinding/param resources.
	// +optional
	MatchConstraints MatchResources `json:"matchConstraints,omitempty" protobuf:"bytes,2,rep,name=matchConstraints"`

	// Validations contain CEL expressions which is used to apply the validation.
	// Required.
	Validations []Validation `json:"validations,omitempty" protobuf:"bytes,3,rep,name=validations"`

	// FailurePolicy defines how unrecognized errors from the admission endpoint are handled -
	// allowed values are Ignore or Fail. Defaults to Fail.
	// +optional
	FailurePolicy *FailurePolicyType `json:"failurePolicy,omitempty" protobuf:"bytes,4,opt,name=failurePolicy,casttype=FailurePolicyType"`
}

// ParamSource is a tuple of Group Kind and Version.
type ParamSource struct {
	// APIGroup is the API group the resources belong to.
	// Required.
	APIGroup string `json:"apiGroup,omitempty" protobuf:"bytes,1,rep,name=apiGroup"`

	// APIVersion is the API version the resources belong to.
	// Required.
	APIVersion string `json:"apiVersion,omitempty" protobuf:"bytes,2,rep,name=apiVersion"`

	// APIKind is the API kind the resources belong to.
	// Required.
	APIKind string `json:"apiKind,omitempty" protobuf:"bytes,3,rep,name=apiKind"`
}

// Validation specified the CEL expression which is used to apply the validation.
type Validation struct {
	// The name of the validation rule.
	// Required.
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`
	// Expression represents the expression which will be evaluated by CEL.
	// ref: https://github.com/google/cel-spec
	// CEL expressions have access to the contents of the AdmissionReview type, organized into CEL variables as well as some other useful variables:
	//
	//'object'
	//'oldObject'
	//'review'
	//'requestResource' (GVR)
	//'resource' (GVR)
	//'name'
	//'namespace'
	//'operation'
	//'userInfo'
	//'dryRun'
	//'options'
	//'config' - configuration data of the policy configuration being validated
	// The `object` variable in the expression is bound to the resource this policy is designed to validate.
	//
	// The `apiVersion`, `kind`, `metadata.name` and `metadata.generateName` are always accessible from the root of the
	// object. No other metadata properties are accessible.
	//
	// Only property names of the form `[a-zA-Z_.-/][a-zA-Z0-9_.-/]*` are accessible.
	// Accessible property names are escaped according to the following rules when accessed in the expression:
	// - '__' escapes to '__underscores__'
	// - '.' escapes to '__dot__'
	// - '-' escapes to '__dash__'
	// - '/' escapes to '__slash__'
	// - Property names that exactly match a CEL RESERVED keyword escape to '__{keyword}__'. The keywords are:
	//	  "true", "false", "null", "in", "as", "break", "const", "continue", "else", "for", "function", "if",
	//	  "import", "let", "loop", "package", "namespace", "return".
	// Examples:
	//   - Expression accessing a property named "namespace": {"Expression": "object.__namespace__ > 0"}
	//   - Expression accessing a property named "x-prop": {"Expression": "object.x__dash__prop > 0"}
	//   - Expression accessing a property named "redact__d": {"Expression": "object.redact__underscores__d > 0"}
	//
	// Equality on arrays with list type of 'set' or 'map' ignores element order, i.e. [1, 2] == [2, 1].
	// Concatenation on arrays with x-kubernetes-list-type use the semantics of the list type:
	//   - 'set': `X + Y` performs a union where the array positions of all elements in `X` are preserved and
	//     non-intersecting elements in `Y` are appended, retaining their partial order.
	//   - 'map': `X + Y` performs a merge where the array positions of all keys in `X` are preserved but the values
	//     are overwritten by values in `Y` when the key sets of `X` and `Y` intersect. Elements in `Y` with
	//     non-intersecting keys are appended, retaining their partial order.
	// Required.
	Expression string `json:"expression" protobuf:"bytes,2,opt,name=Expression"`
	// Message represents the message displayed when validation fails. The message is required if the Expression contains
	// line breaks. The message must not contain line breaks.
	// If unset, the message is "failed rule: {Rule}".
	// e.g. "must be a URL with the host matching spec.host"
	// If ExpressMessage is specified, Message will be ignored
	// If the Expression contains line breaks. Eith Message or ExpressMessage is required.
	// The message must not contain line breaks.
	// If unset, the message is "failed Expression: {Expression}".
	// +optional
	Message string `json:"message" protobuf:"bytes,3,opt,name=message"`
	// If the MessageExpression evaluates to an error, the Message field is used to provide the message.
	// The MessageExpression retured when failed the validation of specified.
	// If MessageExpression is specified, Message will be ignored
	// If the Expression contains line breaks. Either Message or MessageExpression is required.
	// The MessageExpression must not contain line breaks.
	// +optional
	MessageExpression string `json:"expressMessage" protobuf:"bytes,4,opt,name=expressMessage"`
	// Reason returned when failed the validation.
	// +optional
	Reason string `json:"reason" protobuf:"bytes,5,opt,name=reason"`
	// Code returned when failed the validation.
	Code string `json:"code" protobuf:"bytes,6,opt,name=code"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PolicyBinding binds the ValidatingAdmissionPolicy with paramerized resources.
// PolicyBinding and parameter CRDs together define how cluster administrators configure policies for clusters.
type PolicyBinding struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object metadata; More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	// Specification of the desired behavior of the AdmissionPolicy.
	// +optional
	Spec PolicyBindingSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PolicyBindingList is a list of PolicyBinding.
type PolicyBindingList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	// List of PolicyBinding.
	Items []PolicyBinding `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// PolicyBindingSpec is the specification of the PolicyBinding.
type PolicyBindingSpec struct {
	// Policy references a ValidatingAdmissionPolicy which the policyBinding binds to.
	// Required.
	Policy string `json:"policy,omitempty" protobuf:"bytes,1,rep,name=policy"`

	//Param specifies the parameter resource used to configure the admission control policy.
	// It should point to a Customer Resource which is created out of the CRD specified in ParamSource of ValidatingAdmissionPolicy it binded.
	// +optional
	Param string `json:"param,omitempty" protobuf:"bytes,2,rep,name=param"`

	//MatchResources describe a list of match options which could be used to filter the resources.
	// +optional
	MatchResources MatchResources `json:"matchResources,omitempty" protobuf:"bytes,3,rep,name=matchResources"`
}

// MatchResources decides whether to run the admission control policy on an object based
// on whether meet the match criteria.
type MatchResources struct {
	// Namespaces specifies the namespaces which the admission control policy should validate on.
	// +optional
	Namespaces []string `json:"namespaces,omitempty" protobuf:"bytes,1,opt,name=namespaces"`
	// ExcludeNamespaces specifies the namespaces which the validating admission policy should not validate on.
	// +optional
	ExcludeNamespaces []string `json:"excludeNamespaces,omitempty" protobuf:"bytes,2,opt,name=excludeNamespaces"`
	// NamespaceSelector decides whether to run the admission control policy on an object based
	// on whether the namespace for that object matches the selector. If the
	// object itself is a namespace, the matching is performed on
	// object.metadata.labels. If the object is another cluster scoped resource,
	// it never skips the admission control policy.
	//
	// See
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels
	// for examples of label selectors.
	//
	// Default to the empty LabelSelector, which matches everything.
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty" protobuf:"bytes,3,opt,name=namespaceSelector"`
	// LabelSelector decides the match criteria on resource based on its labels.
	// See
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels
	// for examples of label selectors.
	//
	// Default to the empty LabelSelector, which matches everything.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty" protobuf:"bytes,4,opt,name=labelSelector"`
	// ResourceRules describes what operations on what resources/subresources the ValidatingAdmissionPolicy matches.
	// +optional
	ResourceRules []RuleWithOperations `json:"resourceRules,omitempty" protobuf:"bytes,5,rep,name=resourceRules"`
	// ExcludeResourceRules describes what operations on what resources/subresources the ValidatingAdmissionPolicy should not care about.
	// +optional
	ExcludeResourceRules []RuleWithOperations `json:"excluderResourceRules,omitempty" protobuf:"bytes,6,rep,name=excludeResourceRules"`
	// ResourceName specifies the resource name which the admission control policy should validate on.
	// +optional
	ResourceName []string `json:"resourceName,omitempty" protobuf:"bytes,7,rep,name=resourceName"`
	// ExcludeResourceName specifies the resource name which the admission control policy should not validate on.
	// +optional
	ExcludeResourceName []string `json:"excludeResourceName,omitempty" protobuf:"bytes,8,rep,name=excludeResourceName"`
	// matchPolicy defines how the "MatchResources" list is used to match incoming requests.
	// Allowed values are "Exact" or "Equivalent".
	//
	// - Exact: match a request only if it exactly matches a specified rule.
	// For example, if deployments can be modified via apps/v1, apps/v1beta1, and extensions/v1beta1,
	// but "rules" only included `apiGroups:["apps"], apiVersions:["v1"], resources: ["deployments"]`,
	// a request to apps/v1beta1 or extensions/v1beta1 would not be sent to the ValidatingAdmissionPolicy.
	//
	// - Equivalent: match a request if modifies a resource listed in rules, even via another API group or version.
	// For example, if deployments can be modified via apps/v1, apps/v1beta1, and extensions/v1beta1,
	// and "rules" only included `apiGroups:["apps"], apiVersions:["v1"], resources: ["deployments"]`,
	// a request to apps/v1beta1 or extensions/v1beta1 would be converted to apps/v1 and sent to the ValidatingAdmissionPolicy.
	//
	// Defaults to "Equivalent"
	// +optional
	MatchPolicy *MatchPolicyType `json:"matchPolicy,omitempty" protobuf:"bytes,9,opt,name=matchPolicy,casttype=MatchPolicyType"`
}

// RuleWithOperations is a tuple of Operations and Resources. It is recommended to make
// sure that all the tuple expansions are valid.
type RuleWithOperations struct {
	// Operations is the operations the admission hook cares about - CREATE, UPDATE, DELETE, CONNECT or *
	// for all of those operations and any future admission operations that are added.
	// If '*' is present, the length of the slice must be one.
	// Required.
	Operations []OperationType `json:"operations,omitempty" protobuf:"bytes,1,rep,name=operations,casttype=OperationType"`
	// Rule is embedded, it describes other criteria of the rule, like
	// APIGroups, APIVersions, Resources, etc.
	Rule `json:",inline" protobuf:"bytes,2,opt,name=rule"`
}

// OperationType specifies an operation for a request.
// +enum
type OperationType string

// The constants should be kept in sync with those defined in k8s.io/kubernetes/pkg/admission/interface.go.
const (
	OperationAll OperationType = "*"
	Create       OperationType = "CREATE"
	Update       OperationType = "UPDATE"
	Delete       OperationType = "DELETE"
	Connect      OperationType = "CONNECT"
)
