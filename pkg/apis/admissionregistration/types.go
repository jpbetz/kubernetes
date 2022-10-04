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

package admissionregistration

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Rule is a tuple of APIGroups, APIVersion, and Resources.It is recommended
// to make sure that all the tuple expansions are valid.
type Rule struct {
	// APIGroups is the API groups the resources belong to. '*' is all groups.
	// If '*' is present, the length of the slice must be one.
	// Required.
	APIGroups []string

	// APIVersions is the API versions the resources belong to. '*' is all versions.
	// If '*' is present, the length of the slice must be one.
	// Required.
	APIVersions []string

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
	Resources []string

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
	Scope *ScopeType
}

// ScopeType specifies the type of scope being used
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

// FailurePolicyType specifies the type of failure policy
type FailurePolicyType string

const (
	// Ignore means that an error calling the webhook is ignored.
	Ignore FailurePolicyType = "Ignore"
	// Fail means that an error calling the webhook causes the admission to fail.
	Fail FailurePolicyType = "Fail"
)

// MatchPolicyType specifies the type of match policy
type MatchPolicyType string

const (
	// Exact means requests should only be sent to the webhook if they exactly match a given rule
	Exact MatchPolicyType = "Exact"
	// Equivalent means requests should be sent to the webhook if they modify a resource listed in rules via another API group or version.
	Equivalent MatchPolicyType = "Equivalent"
)

// SideEffectClass denotes the type of side effects resulting from calling the webhook
type SideEffectClass string

const (
	// SideEffectClassUnknown means that no information is known about the side effects of calling the webhook.
	// If a request with the dry-run attribute would trigger a call to this webhook, the request will instead fail.
	SideEffectClassUnknown SideEffectClass = "Unknown"
	// SideEffectClassNone means that calling the webhook will have no side effects.
	SideEffectClassNone SideEffectClass = "None"
	// SideEffectClassSome means that calling the webhook will possibly have side effects.
	// If a request with the dry-run attribute would trigger a call to this webhook, the request will instead fail.
	SideEffectClassSome SideEffectClass = "Some"
	// SideEffectClassNoneOnDryRun means that calling the webhook will possibly have side effects, but if the
	// request being reviewed has the dry-run attribute, the side effects will be suppressed.
	SideEffectClassNoneOnDryRun SideEffectClass = "NoneOnDryRun"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ValidatingAdmissionPolicy describes the definition of an admission validation policy that accepts or rejects and object without changing it.
type ValidatingAdmissionPolicy struct {
	metav1.TypeMeta
	// Standard object metadata; More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata.
	// +optional
	metav1.ObjectMeta
	// Specification of the desired behavior of the AdmissionPolicy.
	// +optional
	Spec ValidatingAdmissionPolicySpec
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ValidatingAdmissionPolicyList is a list of ValidatingAdmissionPolicy.
type ValidatingAdmissionPolicyList struct {
	metav1.TypeMeta
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta
	// List of ValidatingAdmissionPolicy.
	Items []ValidatingAdmissionPolicy
}

// ValidatingAdmissionPolicySpec is the specification of the desired behavior of the AdmissionPolicy.
type ValidatingAdmissionPolicySpec struct {
	//ParamSource specifies the kind of resources used to parameterize this policy which has to be a CRD.
	// +optional
	ParamSource ParamSource

	// MatchConstraints specifies what resources this policy is designed to validate.
	// The AdmissionPolicy cares about a validation if it matches _any_ Constriant.
	// However, in order to prevent clusters from being put into a unustable state that cannot be recoverd from via the API
	// alidatingAdmissionPolicy cannot match ValidatingAdmissionPolicy/PolicyBinding/param resources.
	// ValidatingWebhookConfiguration cannot match MutatingWebhookConfiguration or ValidatingAdmissionPolicy/PolicyBinding/param resources.
	// +optional
	MatchConstraints MatchResources

	// Validations contain CEL expressions which is used to apply the validation.
	// Required.
	Validations []Validation

	// FailurePolicy defines how unrecognized errors from the admission endpoint are handled -
	// allowed values are Ignore or Fail. Defaults to Fail.
	// +optional
	FailurePolicy *FailurePolicyType
}

// ParamSource is a tuple of Group Kind and Version.
type ParamSource struct {
	// APIGroup is the API group the resources belong to.
	// Required.
	APIGroup string

	// APIVersion is the API version the resources belong to.
	// Required.
	APIVersion string

	// APIKind is the API kind the resources belong to.
	// Required.
	APIKind string
}

// Validation specified the CEL expression which is used to apply the validation.
type Validation struct {
	// The name of the validation rule.
	// Required.
	Name string
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
	Expression string
	// Message represents the message displayed when validation fails. The message is required if the Expression contains
	// line breaks. The message must not contain line breaks.
	// If unset, the message is "failed rule: {Rule}".
	// e.g. "must be a URL with the host matching spec.host"
	// If ExpressMessage is specified, Message will be ignored
	// If the Expression contains line breaks. Eith Message or ExpressMessage is required.
	// The message must not contain line breaks.
	// If unset, the message is "failed Expression: {Expression}".
	// +optional
	Message string
	// If the MessageExpression evaluates to an error, the Message field is used to provide the message.
	// The MessageExpression retured when failed the validation of specified.
	// If MessageExpression is specified, Message will be ignored
	// If the Expression contains line breaks. Either Message or MessageExpression is required.
	// The MessageExpression must not contain line breaks.
	// +optional
	MessageExpression string
	// Reason returned when failed the validation.
	// +optional
	Reason string
	// Code returned when failed the validation.
	Code string
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PolicyBinding binds the ValidatingAdmissionPolicy with paramerized resources.
// PolicyBinding and parameter CRDs together define how cluster administrators configure policies for clusters.
type PolicyBinding struct {
	metav1.TypeMeta
	// Standard object metadata; More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata.
	// +optional
	metav1.ObjectMeta
	// Specification of the desired behavior of the AdmissionPolicy.
	// +optional
	Spec PolicyBindingSpec
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PolicyBindingList is a list of PolicyBinding.
type PolicyBindingList struct {
	metav1.TypeMeta
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta
	// List of PolicyBinding.
	Items []PolicyBinding
}

// PolicyBindingSpec is the specification of the PolicyBinding.
type PolicyBindingSpec struct {
	// Policy references a ValidatingAdmissionPolicy which the policyBinding binds to.
	// Required.
	Policy string

	//Param specifies the parameter resource used to configure the admission control policy.
	// It should point to a Customer Resource which is created out of the CRD specified in ParamSource of ValidatingAdmissionPolicy it binded.
	// +optional
	Param string

	//MatchResources describe a list of match options which could be used to filter the resources.
	// +optional
	MatchResources MatchResources
}

// MatchResources decides whether to run the admission control policy on an object based
// on whether meet the match criteria.
type MatchResources struct {
	// Namespaces specifies the namespaces which the admission control policy should validate on.
	// +optional
	Namespaces []string
	// ExcludeNamespaces specifies the namespaces which the validating admission policy should not validate on.
	// +optional
	ExcludeNamespaces []string
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
	NamespaceSelector *metav1.LabelSelector
	// LabelSelector decides the match criteria on resource based on its labels.
	// See
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels
	// for examples of label selectors.
	//
	// Default to the empty LabelSelector, which matches everything.
	// +optional
	LabelSelector *metav1.LabelSelector
	// ResourceRules describes what operations on what resources/subresources the ValidatingAdmissionPolicy matches.
	// +optional
	ResourceRules []RuleWithOperations
	// ExcludeResourceRules describes what operations on what resources/subresources the ValidatingAdmissionPolicy should not care about.
	// +optional
	ExcludeResourceRules []RuleWithOperations
	// ResourceName specifies the resource name which the admission control policy should validate on.
	// +optional
	ResourceName []string
	// ExcludeResourceName specifies the resource name which the admission control policy should not validate on.
	// +optional
	ExcludeResourceName []string
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
	MatchPolicy *MatchPolicyType
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ValidatingWebhookConfiguration describes the configuration of an admission webhook that accepts or rejects and object without changing it.
type ValidatingWebhookConfiguration struct {
	metav1.TypeMeta
	// Standard object metadata; More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata.
	// +optional
	metav1.ObjectMeta
	// Webhooks is a list of webhooks and the affected resources and operations.
	// +optional
	Webhooks []ValidatingWebhook
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ValidatingWebhookConfigurationList is a list of ValidatingWebhookConfiguration.
type ValidatingWebhookConfigurationList struct {
	metav1.TypeMeta
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta
	// List of ValidatingWebhookConfigurations.
	Items []ValidatingWebhookConfiguration
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MutatingWebhookConfiguration describes the configuration of and admission webhook that accept or reject and may change the object.
type MutatingWebhookConfiguration struct {
	metav1.TypeMeta
	// Standard object metadata; More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata.
	// +optional
	metav1.ObjectMeta
	// Webhooks is a list of webhooks and the affected resources and operations.
	// +optional
	Webhooks []MutatingWebhook
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MutatingWebhookConfigurationList is a list of MutatingWebhookConfiguration.
type MutatingWebhookConfigurationList struct {
	metav1.TypeMeta
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta
	// List of MutatingWebhookConfiguration.
	Items []MutatingWebhookConfiguration
}

// ValidatingWebhook describes an admission webhook and the resources and operations it applies to.
type ValidatingWebhook struct {
	// The name of the admission webhook.
	// Name should be fully qualified, e.g., imagepolicy.kubernetes.io, where
	// "imagepolicy" is the name of the webhook, and kubernetes.io is the name
	// of the organization.
	// Required.
	Name string

	// ClientConfig defines how to communicate with the hook.
	// Required
	ClientConfig WebhookClientConfig

	// Rules describes what operations on what resources/subresources the webhook cares about.
	// The webhook cares about an operation if it matches _any_ Rule.
	Rules []RuleWithOperations

	// FailurePolicy defines how unrecognized errors from the admission endpoint are handled -
	// allowed values are Ignore or Fail. Defaults to Ignore.
	// +optional
	FailurePolicy *FailurePolicyType

	// matchPolicy defines how the "rules" list is used to match incoming requests.
	// Allowed values are "Exact" or "Equivalent".
	//
	// - Exact: match a request only if it exactly matches a specified rule.
	// For example, if deployments can be modified via apps/v1, apps/v1beta1, and extensions/v1beta1,
	// but "rules" only included `apiGroups:["apps"], apiVersions:["v1"], resources: ["deployments"]`,
	// a request to apps/v1beta1 or extensions/v1beta1 would not be sent to the webhook.
	//
	// - Equivalent: match a request if modifies a resource listed in rules, even via another API group or version.
	// For example, if deployments can be modified via apps/v1, apps/v1beta1, and extensions/v1beta1,
	// and "rules" only included `apiGroups:["apps"], apiVersions:["v1"], resources: ["deployments"]`,
	// a request to apps/v1beta1 or extensions/v1beta1 would be converted to apps/v1 and sent to the webhook.
	//
	// +optional
	MatchPolicy *MatchPolicyType

	// NamespaceSelector decides whether to run the webhook on an object based
	// on whether the namespace for that object matches the selector. If the
	// object itself is a namespace, the matching is performed on
	// object.metadata.labels. If the object is another cluster scoped resource,
	// it never skips the webhook.
	//
	// For example, to run the webhook on any objects whose namespace is not
	// associated with "runlevel" of "0" or "1";  you will set the selector as
	// follows:
	// "namespaceSelector": {
	//   "matchExpressions": [
	//     {
	//       "key": "runlevel",
	//       "operator": "NotIn",
	//       "values": [
	//         "0",
	//         "1"
	//       ]
	//     }
	//   ]
	// }
	//
	// If instead you want to only run the webhook on any objects whose
	// namespace is associated with the "environment" of "prod" or "staging";
	// you will set the selector as follows:
	// "namespaceSelector": {
	//   "matchExpressions": [
	//     {
	//       "key": "environment",
	//       "operator": "In",
	//       "values": [
	//         "prod",
	//         "staging"
	//       ]
	//     }
	//   ]
	// }
	//
	// See
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/
	// for more examples of label selectors.
	//
	// Default to the empty LabelSelector, which matches everything.
	// +optional
	NamespaceSelector *metav1.LabelSelector

	// ObjectSelector decides whether to run the webhook based on if the
	// object has matching labels. objectSelector is evaluated against both
	// the oldObject and newObject that would be sent to the webhook, and
	// is considered to match if either object matches the selector. A null
	// object (oldObject in the case of create, or newObject in the case of
	// delete) or an object that cannot have labels (like a
	// DeploymentRollback or a PodProxyOptions object) is not considered to
	// match.
	// Use the object selector only if the webhook is opt-in, because end
	// users may skip the admission webhook by setting the labels.
	// Default to the empty LabelSelector, which matches everything.
	// +optional
	ObjectSelector *metav1.LabelSelector

	// SideEffects states whether this webhook has side effects.
	// Acceptable values are: Unknown, None, Some, NoneOnDryRun
	// Webhooks with side effects MUST implement a reconciliation system, since a request may be
	// rejected by a future step in the admission chain and the side effects therefore need to be undone.
	// Requests with the dryRun attribute will be auto-rejected if they match a webhook with
	// sideEffects == Unknown or Some. Defaults to Unknown.
	// +optional
	SideEffects *SideEffectClass

	// TimeoutSeconds specifies the timeout for this webhook. After the timeout passes,
	// the webhook call will be ignored or the API call will fail based on the
	// failure policy.
	// The timeout value must be between 1 and 30 seconds.
	// +optional
	TimeoutSeconds *int32

	// AdmissionReviewVersions is an ordered list of preferred `AdmissionReview`
	// versions the Webhook expects. API server will try to use first version in
	// the list which it supports. If none of the versions specified in this list
	// supported by API server, validation will fail for this object.
	// If the webhook configuration has already been persisted with a version apiserver
	// does not understand, calls to the webhook will fail and be subject to the failure policy.
	// +optional
	AdmissionReviewVersions []string
}

// MutatingWebhook describes an admission webhook and the resources and operations it applies to.
type MutatingWebhook struct {
	// The name of the admission webhook.
	// Name should be fully qualified, e.g., imagepolicy.kubernetes.io, where
	// "imagepolicy" is the name of the webhook, and kubernetes.io is the name
	// of the organization.
	// Required.
	Name string

	// ClientConfig defines how to communicate with the hook.
	// Required
	ClientConfig WebhookClientConfig

	// Rules describes what operations on what resources/subresources the webhook cares about.
	// The webhook cares about an operation if it matches _any_ Rule.
	Rules []RuleWithOperations

	// FailurePolicy defines how unrecognized errors from the admission endpoint are handled -
	// allowed values are Ignore or Fail. Defaults to Ignore.
	// +optional
	FailurePolicy *FailurePolicyType

	// matchPolicy defines how the "rules" list is used to match incoming requests.
	// Allowed values are "Exact" or "Equivalent".
	//
	// - Exact: match a request only if it exactly matches a specified rule.
	// For example, if deployments can be modified via apps/v1, apps/v1beta1, and extensions/v1beta1,
	// but "rules" only included `apiGroups:["apps"], apiVersions:["v1"], resources: ["deployments"]`,
	// a request to apps/v1beta1 or extensions/v1beta1 would not be sent to the webhook.
	//
	// - Equivalent: match a request if modifies a resource listed in rules, even via another API group or version.
	// For example, if deployments can be modified via apps/v1, apps/v1beta1, and extensions/v1beta1,
	// and "rules" only included `apiGroups:["apps"], apiVersions:["v1"], resources: ["deployments"]`,
	// a request to apps/v1beta1 or extensions/v1beta1 would be converted to apps/v1 and sent to the webhook.
	//
	// +optional
	MatchPolicy *MatchPolicyType

	// NamespaceSelector decides whether to run the webhook on an object based
	// on whether the namespace for that object matches the selector. If the
	// object itself is a namespace, the matching is performed on
	// object.metadata.labels. If the object is another cluster scoped resource,
	// it never skips the webhook.
	//
	// For example, to run the webhook on any objects whose namespace is not
	// associated with "runlevel" of "0" or "1";  you will set the selector as
	// follows:
	// "namespaceSelector": {
	//   "matchExpressions": [
	//     {
	//       "key": "runlevel",
	//       "operator": "NotIn",
	//       "values": [
	//         "0",
	//         "1"
	//       ]
	//     }
	//   ]
	// }
	//
	// If instead you want to only run the webhook on any objects whose
	// namespace is associated with the "environment" of "prod" or "staging";
	// you will set the selector as follows:
	// "namespaceSelector": {
	//   "matchExpressions": [
	//     {
	//       "key": "environment",
	//       "operator": "In",
	//       "values": [
	//         "prod",
	//         "staging"
	//       ]
	//     }
	//   ]
	// }
	//
	// See
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/
	// for more examples of label selectors.
	//
	// Default to the empty LabelSelector, which matches everything.
	// +optional
	NamespaceSelector *metav1.LabelSelector

	// ObjectSelector decides whether to run the webhook based on if the
	// object has matching labels. objectSelector is evaluated against both
	// the oldObject and newObject that would be sent to the webhook, and
	// is considered to match if either object matches the selector. A null
	// object (oldObject in the case of create, or newObject in the case of
	// delete) or an object that cannot have labels (like a
	// DeploymentRollback or a PodProxyOptions object) is not considered to
	// match.
	// Use the object selector only if the webhook is opt-in, because end
	// users may skip the admission webhook by setting the labels.
	// Default to the empty LabelSelector, which matches everything.
	// +optional
	ObjectSelector *metav1.LabelSelector

	// SideEffects states whether this webhook has side effects.
	// Acceptable values are: Unknown, None, Some, NoneOnDryRun
	// Webhooks with side effects MUST implement a reconciliation system, since a request may be
	// rejected by a future step in the admission chain and the side effects therefore need to be undone.
	// Requests with the dryRun attribute will be auto-rejected if they match a webhook with
	// sideEffects == Unknown or Some. Defaults to Unknown.
	// +optional
	SideEffects *SideEffectClass

	// TimeoutSeconds specifies the timeout for this webhook. After the timeout passes,
	// the webhook call will be ignored or the API call will fail based on the
	// failure policy.
	// The timeout value must be between 1 and 30 seconds.
	// +optional
	TimeoutSeconds *int32

	// AdmissionReviewVersions is an ordered list of preferred `AdmissionReview`
	// versions the Webhook expects. API server will try to use first version in
	// the list which it supports. If none of the versions specified in this list
	// supported by API server, validation will fail for this object.
	// If the webhook configuration has already been persisted with a version apiserver
	// does not understand, calls to the webhook will fail and be subject to the failure policy.
	// +optional
	AdmissionReviewVersions []string

	// reinvocationPolicy indicates whether this webhook should be called multiple times as part of a single admission evaluation.
	// Allowed values are "Never" and "IfNeeded".
	//
	// Never: the webhook will not be called more than once in a single admission evaluation.
	//
	// IfNeeded: the webhook will be called at least one additional time as part of the admission evaluation
	// if the object being admitted is modified by other admission plugins after the initial webhook call.
	// Webhooks that specify this option *must* be idempotent, and hence able to process objects they previously admitted.
	// Note:
	// * the number of additional invocations is not guaranteed to be exactly one.
	// * if additional invocations result in further modifications to the object, webhooks are not guaranteed to be invoked again.
	// * webhooks that use this option may be reordered to minimize the number of additional invocations.
	// * to validate an object after all mutations are guaranteed complete, use a validating admission webhook instead.
	//
	// Defaults to "Never".
	// +optional
	ReinvocationPolicy *ReinvocationPolicyType
}

// ReinvocationPolicyType specifies what type of policy the admission hook uses.
type ReinvocationPolicyType string

var (
	// NeverReinvocationPolicy indicates that the webhook must not be called more than once in a
	// single admission evaluation.
	NeverReinvocationPolicy ReinvocationPolicyType = "Never"
	// IfNeededReinvocationPolicy indicates that the webhook may be called at least one
	// additional time as part of the admission evaluation if the object being admitted is
	// modified by other admission plugins after the initial webhook call.
	IfNeededReinvocationPolicy ReinvocationPolicyType = "IfNeeded"
)

// RuleWithOperations is a tuple of Operations and Resources. It is recommended to make
// sure that all the tuple expansions are valid.
type RuleWithOperations struct {
	// Operations is the operations the admission hook cares about - CREATE, UPDATE, or *
	// for all operations.
	// If '*' is present, the length of the slice must be one.
	// Required.
	Operations []OperationType
	// Rule is embedded, it describes other criteria of the rule, like
	// APIGroups, APIVersions, Resources, etc.
	Rule
}

// OperationType specifies what type of operation the admission hook cares about.
type OperationType string

// The constants should be kept in sync with those defined in k8s.io/kubernetes/pkg/admission/interface.go.
const (
	OperationAll OperationType = "*"
	Create       OperationType = "CREATE"
	Update       OperationType = "UPDATE"
	Delete       OperationType = "DELETE"
	Connect      OperationType = "CONNECT"
)

// WebhookClientConfig contains the information to make a TLS
// connection with the webhook
type WebhookClientConfig struct {
	// `url` gives the location of the webhook, in standard URL form
	// (`scheme://host:port/path`). Exactly one of `url` or `service`
	// must be specified.
	//
	// The `host` should not refer to a service running in the cluster; use
	// the `service` field instead. The host might be resolved via external
	// DNS in some apiservers (e.g., `kube-apiserver` cannot resolve
	// in-cluster DNS as that would be a layering violation). `host` may
	// also be an IP address.
	//
	// Please note that using `localhost` or `127.0.0.1` as a `host` is
	// risky unless you take great care to run this webhook on all hosts
	// which run an apiserver which might need to make calls to this
	// webhook. Such installs are likely to be non-portable, i.e., not easy
	// to turn up in a new cluster.
	//
	// The scheme must be "https"; the URL must begin with "https://".
	//
	// A path is optional, and if present may be any string permissible in
	// a URL. You may use the path to pass an arbitrary string to the
	// webhook, for example, a cluster identifier.
	//
	// Attempting to use a user or basic auth e.g. "user:password@" is not
	// allowed. Fragments ("#...") and query parameters ("?...") are not
	// allowed, either.
	//
	// +optional
	URL *string

	// `service` is a reference to the service for this webhook. Either
	// `service` or `url` must be specified.
	//
	// If the webhook is running within the cluster, then you should use `service`.
	//
	// +optional
	Service *ServiceReference

	// `caBundle` is a PEM encoded CA bundle which will be used to validate the webhook's server certificate.
	// If unspecified, system trust roots on the apiserver are used.
	// +optional
	CABundle []byte
}

// ServiceReference holds a reference to Service.legacy.k8s.io
type ServiceReference struct {
	// `namespace` is the namespace of the service.
	// Required
	Namespace string
	// `name` is the name of the service.
	// Required
	Name string

	// `path` is an optional URL path which will be sent in any request to
	// this service.
	// +optional
	Path *string

	// If specified, the port on the service that hosting webhook.
	// `port` should be a valid port number (1-65535, inclusive).
	// +optional
	Port int32
}
