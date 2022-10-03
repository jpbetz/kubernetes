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

package validation

import (
	"fmt"
	"regexp"
	"strings"

	genericvalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/sets"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/util/webhook"
	"k8s.io/kubernetes/pkg/apis/admissionregistration"
	admissionregistrationv1 "k8s.io/kubernetes/pkg/apis/admissionregistration/v1"
	admissionregistrationv1alpha1 "k8s.io/kubernetes/pkg/apis/admissionregistration/v1alpha1"
	admissionregistrationv1beta1 "k8s.io/kubernetes/pkg/apis/admissionregistration/v1beta1"
)

func hasWildcard(slice []string) bool {
	for _, s := range slice {
		if s == "*" {
			return true
		}
	}
	return false
}

func validateResources(resources []string, fldPath *field.Path) field.ErrorList {
	var allErrors field.ErrorList
	if len(resources) == 0 {
		allErrors = append(allErrors, field.Required(fldPath, ""))
	}

	// x/*
	resourcesWithWildcardSubresoures := sets.String{}
	// */x
	subResourcesWithWildcardResource := sets.String{}
	// */*
	hasDoubleWildcard := false
	// *
	hasSingleWildcard := false
	// x
	hasResourceWithoutSubresource := false

	for i, resSub := range resources {
		if resSub == "" {
			allErrors = append(allErrors, field.Required(fldPath.Index(i), ""))
			continue
		}
		if resSub == "*/*" {
			hasDoubleWildcard = true
		}
		if resSub == "*" {
			hasSingleWildcard = true
		}
		parts := strings.SplitN(resSub, "/", 2)
		if len(parts) == 1 {
			hasResourceWithoutSubresource = resSub != "*"
			continue
		}
		res, sub := parts[0], parts[1]
		if _, ok := resourcesWithWildcardSubresoures[res]; ok {
			allErrors = append(allErrors, field.Invalid(fldPath.Index(i), resSub, fmt.Sprintf("if '%s/*' is present, must not specify %s", res, resSub)))
		}
		if _, ok := subResourcesWithWildcardResource[sub]; ok {
			allErrors = append(allErrors, field.Invalid(fldPath.Index(i), resSub, fmt.Sprintf("if '*/%s' is present, must not specify %s", sub, resSub)))
		}
		if sub == "*" {
			resourcesWithWildcardSubresoures[res] = struct{}{}
		}
		if res == "*" {
			subResourcesWithWildcardResource[sub] = struct{}{}
		}
	}
	if len(resources) > 1 && hasDoubleWildcard {
		allErrors = append(allErrors, field.Invalid(fldPath, resources, "if '*/*' is present, must not specify other resources"))
	}
	if hasSingleWildcard && hasResourceWithoutSubresource {
		allErrors = append(allErrors, field.Invalid(fldPath, resources, "if '*' is present, must not specify other resources without subresources"))
	}
	return allErrors
}

func validateResourcesNoSubResources(resources []string, fldPath *field.Path) field.ErrorList {
	var allErrors field.ErrorList
	if len(resources) == 0 {
		allErrors = append(allErrors, field.Required(fldPath, ""))
	}
	for i, resource := range resources {
		if resource == "" {
			allErrors = append(allErrors, field.Required(fldPath.Index(i), ""))
		}
		if strings.Contains(resource, "/") {
			allErrors = append(allErrors, field.Invalid(fldPath.Index(i), resource, "must not specify subresources"))
		}
	}
	if len(resources) > 1 && hasWildcard(resources) {
		allErrors = append(allErrors, field.Invalid(fldPath, resources, "if '*' is present, must not specify other resources"))
	}
	return allErrors
}

var validScopes = sets.NewString(
	string(admissionregistration.ClusterScope),
	string(admissionregistration.NamespacedScope),
	string(admissionregistration.AllScopes),
)

func validateRule(rule *admissionregistration.Rule, fldPath *field.Path, allowSubResource bool) field.ErrorList {
	var allErrors field.ErrorList
	if len(rule.APIGroups) == 0 {
		allErrors = append(allErrors, field.Required(fldPath.Child("apiGroups"), ""))
	}
	if len(rule.APIGroups) > 1 && hasWildcard(rule.APIGroups) {
		allErrors = append(allErrors, field.Invalid(fldPath.Child("apiGroups"), rule.APIGroups, "if '*' is present, must not specify other API groups"))
	}
	// Note: group could be empty, e.g., the legacy "v1" API
	if len(rule.APIVersions) == 0 {
		allErrors = append(allErrors, field.Required(fldPath.Child("apiVersions"), ""))
	}
	if len(rule.APIVersions) > 1 && hasWildcard(rule.APIVersions) {
		allErrors = append(allErrors, field.Invalid(fldPath.Child("apiVersions"), rule.APIVersions, "if '*' is present, must not specify other API versions"))
	}
	for i, version := range rule.APIVersions {
		if version == "" {
			allErrors = append(allErrors, field.Required(fldPath.Child("apiVersions").Index(i), ""))
		}
	}
	if allowSubResource {
		allErrors = append(allErrors, validateResources(rule.Resources, fldPath.Child("resources"))...)
	} else {
		allErrors = append(allErrors, validateResourcesNoSubResources(rule.Resources, fldPath.Child("resources"))...)
	}
	if rule.Scope != nil && !validScopes.Has(string(*rule.Scope)) {
		allErrors = append(allErrors, field.NotSupported(fldPath.Child("scope"), *rule.Scope, validScopes.List()))
	}
	return allErrors
}

// AcceptedAdmissionReviewVersions contains the list of AdmissionReview versions the *prior* version of the API server understands.
// 1.15: server understands v1beta1; accepted versions are ["v1beta1"]
// 1.16: server understands v1, v1beta1; accepted versions are ["v1beta1"]
// 1.17+: server understands v1, v1beta1; accepted versions are ["v1","v1beta1"]
// 1.26: server understands v1alpha1; accepted version are ["v1", "v1beta1", "v1alpha1"]
var AcceptedAdmissionReviewVersions = []string{admissionregistrationv1.SchemeGroupVersion.Version, admissionregistrationv1beta1.SchemeGroupVersion.Version, admissionregistrationv1alpha1.SchemeGroupVersion.Version}

func isAcceptedAdmissionReviewVersion(v string) bool {
	for _, version := range AcceptedAdmissionReviewVersions {
		if v == version {
			return true
		}
	}
	return false
}

func validateAdmissionReviewVersions(versions []string, requireRecognizedAdmissionReviewVersion bool, fldPath *field.Path) field.ErrorList {
	allErrors := field.ErrorList{}

	// Currently only v1beta1 accepted in AdmissionReviewVersions
	if len(versions) < 1 {
		allErrors = append(allErrors, field.Required(fldPath, fmt.Sprintf("must specify one of %v", strings.Join(AcceptedAdmissionReviewVersions, ", "))))
	} else {
		seen := map[string]bool{}
		hasAcceptedVersion := false
		for i, v := range versions {
			if seen[v] {
				allErrors = append(allErrors, field.Invalid(fldPath.Index(i), v, "duplicate version"))
				continue
			}
			seen[v] = true
			for _, errString := range utilvalidation.IsDNS1035Label(v) {
				allErrors = append(allErrors, field.Invalid(fldPath.Index(i), v, errString))
			}
			if isAcceptedAdmissionReviewVersion(v) {
				hasAcceptedVersion = true
			}
		}
		if requireRecognizedAdmissionReviewVersion && !hasAcceptedVersion {
			allErrors = append(allErrors, field.Invalid(
				fldPath, versions,
				fmt.Sprintf("must include at least one of %v",
					strings.Join(AcceptedAdmissionReviewVersions, ", "))))
		}
	}
	return allErrors
}

// ValidateValidatingWebhookConfiguration validates a webhook before creation.
func ValidateValidatingWebhookConfiguration(e *admissionregistration.ValidatingWebhookConfiguration) field.ErrorList {
	return validateValidatingWebhookConfiguration(e, validationOptions{
		requireNoSideEffects:                    true,
		requireRecognizedAdmissionReviewVersion: true,
		requireUniqueWebhookNames:               true,
	})
}

func validateValidatingWebhookConfiguration(e *admissionregistration.ValidatingWebhookConfiguration, opts validationOptions) field.ErrorList {
	allErrors := genericvalidation.ValidateObjectMeta(&e.ObjectMeta, false, genericvalidation.NameIsDNSSubdomain, field.NewPath("metadata"))
	hookNames := sets.NewString()
	for i, hook := range e.Webhooks {
		allErrors = append(allErrors, validateValidatingWebhook(&hook, opts, field.NewPath("webhooks").Index(i))...)
		allErrors = append(allErrors, validateAdmissionReviewVersions(hook.AdmissionReviewVersions, opts.requireRecognizedAdmissionReviewVersion, field.NewPath("webhooks").Index(i).Child("admissionReviewVersions"))...)
		if opts.requireUniqueWebhookNames && len(hook.Name) > 0 {
			if hookNames.Has(hook.Name) {
				allErrors = append(allErrors, field.Duplicate(field.NewPath("webhooks").Index(i).Child("name"), hook.Name))
			} else {
				hookNames.Insert(hook.Name)
			}
		}
	}
	return allErrors
}

// ValidateMutatingWebhookConfiguration validates a webhook before creation.
func ValidateMutatingWebhookConfiguration(e *admissionregistration.MutatingWebhookConfiguration) field.ErrorList {
	return validateMutatingWebhookConfiguration(e, validationOptions{
		requireNoSideEffects:                    true,
		requireRecognizedAdmissionReviewVersion: true,
		requireUniqueWebhookNames:               true,
	})
}

type validationOptions struct {
	requireNoSideEffects                    bool
	requireRecognizedAdmissionReviewVersion bool
	requireUniqueWebhookNames               bool
}

func validateMutatingWebhookConfiguration(e *admissionregistration.MutatingWebhookConfiguration, opts validationOptions) field.ErrorList {
	allErrors := genericvalidation.ValidateObjectMeta(&e.ObjectMeta, false, genericvalidation.NameIsDNSSubdomain, field.NewPath("metadata"))
	hookNames := sets.NewString()
	for i, hook := range e.Webhooks {
		allErrors = append(allErrors, validateMutatingWebhook(&hook, opts, field.NewPath("webhooks").Index(i))...)
		allErrors = append(allErrors, validateAdmissionReviewVersions(hook.AdmissionReviewVersions, opts.requireRecognizedAdmissionReviewVersion, field.NewPath("webhooks").Index(i).Child("admissionReviewVersions"))...)
		if opts.requireUniqueWebhookNames && len(hook.Name) > 0 {
			if hookNames.Has(hook.Name) {
				allErrors = append(allErrors, field.Duplicate(field.NewPath("webhooks").Index(i).Child("name"), hook.Name))
			} else {
				hookNames.Insert(hook.Name)
			}
		}
	}
	return allErrors
}

func validateValidatingWebhook(hook *admissionregistration.ValidatingWebhook, opts validationOptions, fldPath *field.Path) field.ErrorList {
	var allErrors field.ErrorList
	// hook.Name must be fully qualified
	allErrors = append(allErrors, utilvalidation.IsFullyQualifiedName(fldPath.Child("name"), hook.Name)...)

	for i, rule := range hook.Rules {
		allErrors = append(allErrors, validateRuleWithOperations(&rule, fldPath.Child("rules").Index(i))...)
	}
	if hook.FailurePolicy != nil && !supportedFailurePolicies.Has(string(*hook.FailurePolicy)) {
		allErrors = append(allErrors, field.NotSupported(fldPath.Child("failurePolicy"), *hook.FailurePolicy, supportedFailurePolicies.List()))
	}
	if hook.MatchPolicy != nil && !supportedMatchPolicies.Has(string(*hook.MatchPolicy)) {
		allErrors = append(allErrors, field.NotSupported(fldPath.Child("matchPolicy"), *hook.MatchPolicy, supportedMatchPolicies.List()))
	}
	allowedSideEffects := supportedSideEffectClasses
	if opts.requireNoSideEffects {
		allowedSideEffects = noSideEffectClasses
	}
	if hook.SideEffects == nil {
		allErrors = append(allErrors, field.Required(fldPath.Child("sideEffects"), fmt.Sprintf("must specify one of %v", strings.Join(allowedSideEffects.List(), ", "))))
	}
	if hook.SideEffects != nil && !allowedSideEffects.Has(string(*hook.SideEffects)) {
		allErrors = append(allErrors, field.NotSupported(fldPath.Child("sideEffects"), *hook.SideEffects, allowedSideEffects.List()))
	}
	if hook.TimeoutSeconds != nil && (*hook.TimeoutSeconds > 30 || *hook.TimeoutSeconds < 1) {
		allErrors = append(allErrors, field.Invalid(fldPath.Child("timeoutSeconds"), *hook.TimeoutSeconds, "the timeout value must be between 1 and 30 seconds"))
	}

	if hook.NamespaceSelector != nil {
		allErrors = append(allErrors, metav1validation.ValidateLabelSelector(hook.NamespaceSelector, fldPath.Child("namespaceSelector"))...)
	}

	if hook.ObjectSelector != nil {
		allErrors = append(allErrors, metav1validation.ValidateLabelSelector(hook.ObjectSelector, fldPath.Child("objectSelector"))...)
	}

	cc := hook.ClientConfig
	switch {
	case (cc.URL == nil) == (cc.Service == nil):
		allErrors = append(allErrors, field.Required(fldPath.Child("clientConfig"), "exactly one of url or service is required"))
	case cc.URL != nil:
		allErrors = append(allErrors, webhook.ValidateWebhookURL(fldPath.Child("clientConfig").Child("url"), *cc.URL, true)...)
	case cc.Service != nil:
		allErrors = append(allErrors, webhook.ValidateWebhookService(fldPath.Child("clientConfig").Child("service"), cc.Service.Name, cc.Service.Namespace, cc.Service.Path, cc.Service.Port)...)
	}
	return allErrors
}

func validateMutatingWebhook(hook *admissionregistration.MutatingWebhook, opts validationOptions, fldPath *field.Path) field.ErrorList {
	var allErrors field.ErrorList
	// hook.Name must be fully qualified
	allErrors = append(allErrors, utilvalidation.IsFullyQualifiedName(fldPath.Child("name"), hook.Name)...)

	for i, rule := range hook.Rules {
		allErrors = append(allErrors, validateRuleWithOperations(&rule, fldPath.Child("rules").Index(i))...)
	}
	if hook.FailurePolicy != nil && !supportedFailurePolicies.Has(string(*hook.FailurePolicy)) {
		allErrors = append(allErrors, field.NotSupported(fldPath.Child("failurePolicy"), *hook.FailurePolicy, supportedFailurePolicies.List()))
	}
	if hook.MatchPolicy != nil && !supportedMatchPolicies.Has(string(*hook.MatchPolicy)) {
		allErrors = append(allErrors, field.NotSupported(fldPath.Child("matchPolicy"), *hook.MatchPolicy, supportedMatchPolicies.List()))
	}
	allowedSideEffects := supportedSideEffectClasses
	if opts.requireNoSideEffects {
		allowedSideEffects = noSideEffectClasses
	}
	if hook.SideEffects == nil {
		allErrors = append(allErrors, field.Required(fldPath.Child("sideEffects"), fmt.Sprintf("must specify one of %v", strings.Join(allowedSideEffects.List(), ", "))))
	}
	if hook.SideEffects != nil && !allowedSideEffects.Has(string(*hook.SideEffects)) {
		allErrors = append(allErrors, field.NotSupported(fldPath.Child("sideEffects"), *hook.SideEffects, allowedSideEffects.List()))
	}
	if hook.TimeoutSeconds != nil && (*hook.TimeoutSeconds > 30 || *hook.TimeoutSeconds < 1) {
		allErrors = append(allErrors, field.Invalid(fldPath.Child("timeoutSeconds"), *hook.TimeoutSeconds, "the timeout value must be between 1 and 30 seconds"))
	}

	if hook.NamespaceSelector != nil {
		allErrors = append(allErrors, metav1validation.ValidateLabelSelector(hook.NamespaceSelector, fldPath.Child("namespaceSelector"))...)
	}
	if hook.ObjectSelector != nil {
		allErrors = append(allErrors, metav1validation.ValidateLabelSelector(hook.ObjectSelector, fldPath.Child("objectSelector"))...)
	}
	if hook.ReinvocationPolicy != nil && !supportedReinvocationPolicies.Has(string(*hook.ReinvocationPolicy)) {
		allErrors = append(allErrors, field.NotSupported(fldPath.Child("reinvocationPolicy"), *hook.ReinvocationPolicy, supportedReinvocationPolicies.List()))
	}

	cc := hook.ClientConfig
	switch {
	case (cc.URL == nil) == (cc.Service == nil):
		allErrors = append(allErrors, field.Required(fldPath.Child("clientConfig"), "exactly one of url or service is required"))
	case cc.URL != nil:
		allErrors = append(allErrors, webhook.ValidateWebhookURL(fldPath.Child("clientConfig").Child("url"), *cc.URL, true)...)
	case cc.Service != nil:
		allErrors = append(allErrors, webhook.ValidateWebhookService(fldPath.Child("clientConfig").Child("service"), cc.Service.Name, cc.Service.Namespace, cc.Service.Path, cc.Service.Port)...)
	}
	return allErrors
}

var supportedFailurePolicies = sets.NewString(
	string(admissionregistration.Ignore),
	string(admissionregistration.Fail),
)

var supportedMatchPolicies = sets.NewString(
	string(admissionregistration.Exact),
	string(admissionregistration.Equivalent),
)

var supportedSideEffectClasses = sets.NewString(
	string(admissionregistration.SideEffectClassUnknown),
	string(admissionregistration.SideEffectClassNone),
	string(admissionregistration.SideEffectClassSome),
	string(admissionregistration.SideEffectClassNoneOnDryRun),
)

var noSideEffectClasses = sets.NewString(
	string(admissionregistration.SideEffectClassNone),
	string(admissionregistration.SideEffectClassNoneOnDryRun),
)

var supportedOperations = sets.NewString(
	string(admissionregistration.OperationAll),
	string(admissionregistration.Create),
	string(admissionregistration.Update),
	string(admissionregistration.Delete),
	string(admissionregistration.Connect),
)

var supportedReinvocationPolicies = sets.NewString(
	string(admissionregistration.NeverReinvocationPolicy),
	string(admissionregistration.IfNeededReinvocationPolicy),
)

func hasWildcardOperation(operations []admissionregistration.OperationType) bool {
	for _, o := range operations {
		if o == admissionregistration.OperationAll {
			return true
		}
	}
	return false
}

func validateRuleWithOperations(ruleWithOperations *admissionregistration.RuleWithOperations, fldPath *field.Path) field.ErrorList {
	var allErrors field.ErrorList
	if len(ruleWithOperations.Operations) == 0 {
		allErrors = append(allErrors, field.Required(fldPath.Child("operations"), ""))
	}
	if len(ruleWithOperations.Operations) > 1 && hasWildcardOperation(ruleWithOperations.Operations) {
		allErrors = append(allErrors, field.Invalid(fldPath.Child("operations"), ruleWithOperations.Operations, "if '*' is present, must not specify other operations"))
	}
	for i, operation := range ruleWithOperations.Operations {
		if !supportedOperations.Has(string(operation)) {
			allErrors = append(allErrors, field.NotSupported(fldPath.Child("operations").Index(i), operation, supportedOperations.List()))
		}
	}
	allowSubResource := true
	allErrors = append(allErrors, validateRule(&ruleWithOperations.Rule, fldPath, allowSubResource)...)
	return allErrors
}

// mutatingHasAcceptedAdmissionReviewVersions returns true if all webhooks have at least one
// admission review version this apiserver accepts.
func mutatingHasAcceptedAdmissionReviewVersions(webhooks []admissionregistration.MutatingWebhook) bool {
	for _, hook := range webhooks {
		hasRecognizedVersion := false
		for _, version := range hook.AdmissionReviewVersions {
			if isAcceptedAdmissionReviewVersion(version) {
				hasRecognizedVersion = true
				break
			}
		}
		if !hasRecognizedVersion && len(hook.AdmissionReviewVersions) > 0 {
			return false
		}
	}
	return true
}

// validatingHasAcceptedAdmissionReviewVersions returns true if all webhooks have at least one
// admission review version this apiserver accepts.
func validatingHasAcceptedAdmissionReviewVersions(webhooks []admissionregistration.ValidatingWebhook) bool {
	for _, hook := range webhooks {
		hasRecognizedVersion := false
		for _, version := range hook.AdmissionReviewVersions {
			if isAcceptedAdmissionReviewVersion(version) {
				hasRecognizedVersion = true
				break
			}
		}
		if !hasRecognizedVersion && len(hook.AdmissionReviewVersions) > 0 {
			return false
		}
	}
	return true
}

// mutatingHasUniqueWebhookNames returns true if all webhooks have unique names
func mutatingHasUniqueWebhookNames(webhooks []admissionregistration.MutatingWebhook) bool {
	names := sets.NewString()
	for _, hook := range webhooks {
		if names.Has(hook.Name) {
			return false
		}
		names.Insert(hook.Name)
	}
	return true
}

// validatingHasUniqueWebhookNames returns true if all webhooks have unique names
func validatingHasUniqueWebhookNames(webhooks []admissionregistration.ValidatingWebhook) bool {
	names := sets.NewString()
	for _, hook := range webhooks {
		if names.Has(hook.Name) {
			return false
		}
		names.Insert(hook.Name)
	}
	return true
}

// mutatingHasNoSideEffects returns true if all webhooks have no side effects
func mutatingHasNoSideEffects(webhooks []admissionregistration.MutatingWebhook) bool {
	for _, hook := range webhooks {
		if hook.SideEffects == nil || !noSideEffectClasses.Has(string(*hook.SideEffects)) {
			return false
		}
	}
	return true
}

// validatingHasNoSideEffects returns true if all webhooks have no side effects
func validatingHasNoSideEffects(webhooks []admissionregistration.ValidatingWebhook) bool {
	for _, hook := range webhooks {
		if hook.SideEffects == nil || !noSideEffectClasses.Has(string(*hook.SideEffects)) {
			return false
		}
	}
	return true
}

// ValidateValidatingWebhookConfigurationUpdate validates update of validating webhook configuration
func ValidateValidatingWebhookConfigurationUpdate(newC, oldC *admissionregistration.ValidatingWebhookConfiguration) field.ErrorList {
	return validateValidatingWebhookConfiguration(newC, validationOptions{
		requireNoSideEffects:                    validatingHasNoSideEffects(oldC.Webhooks),
		requireRecognizedAdmissionReviewVersion: validatingHasAcceptedAdmissionReviewVersions(oldC.Webhooks),
		requireUniqueWebhookNames:               validatingHasUniqueWebhookNames(oldC.Webhooks),
	})
}

// ValidateMutatingWebhookConfigurationUpdate validates update of mutating webhook configuration
func ValidateMutatingWebhookConfigurationUpdate(newC, oldC *admissionregistration.MutatingWebhookConfiguration) field.ErrorList {
	return validateMutatingWebhookConfiguration(newC, validationOptions{
		requireNoSideEffects:                    mutatingHasNoSideEffects(oldC.Webhooks),
		requireRecognizedAdmissionReviewVersion: mutatingHasAcceptedAdmissionReviewVersions(oldC.Webhooks),
		requireUniqueWebhookNames:               mutatingHasUniqueWebhookNames(oldC.Webhooks),
	})
}

// ValidateValidatingAdmissionPolicy validates a ValidatingAdmissionPolicy before creation.
func ValidateValidatingAdmissionPolicy(p *admissionregistration.ValidatingAdmissionPolicy) field.ErrorList {
	return validateValidatingAdmissionPolicy(p)
}

func validateValidatingAdmissionPolicy(p *admissionregistration.ValidatingAdmissionPolicy) field.ErrorList {
	allErrors := genericvalidation.ValidateObjectMeta(&p.ObjectMeta, false, genericvalidation.NameIsDNSSubdomain, field.NewPath("metadata"))
	allErrors = append(allErrors, validateValidatingAdmissionPolicySpec(&p.Spec, field.NewPath("spec"))...)

	return allErrors
}

func validateValidatingAdmissionPolicySpec(spec *admissionregistration.ValidatingAdmissionPolicySpec, fldPath *field.Path) field.ErrorList {
	var allErrors field.ErrorList
	if spec.FailurePolicy != nil && !supportedFailurePolicies.Has(string(*spec.FailurePolicy)) {
		allErrors = append(allErrors, field.NotSupported(fldPath.Child("failurePolicy"), *spec.FailurePolicy, supportedFailurePolicies.List()))
	}
	allErrors = append(allErrors, validateParamSource(&spec.ParamSource, fldPath.Child("paramSource"))...)
	allErrors = append(allErrors, validateMatchResources(&spec.MatchConstraints, fldPath.Child("matchConstraints"))...)
	if len(spec.Validations) == 0 {
		allErrors = append(allErrors, field.Required(fldPath.Child("validations"), ""))
	}
	for i, validation := range spec.Validations {
		allErrors = append(allErrors, validateValidation(&validation, fldPath.Child("validations").Index(i))...)
	}
	return allErrors
}

func validateParamSource(ps *admissionregistration.ParamSource, fldPath *field.Path) field.ErrorList {
	var allErrors field.ErrorList
	if len(ps.APIGroup) == 0 {
		allErrors = append(allErrors, field.Required(fldPath.Child("apiGroup"), ""))
	} else if errs := utilvalidation.IsDNS1123Subdomain(ps.APIGroup); len(errs) > 0 {
		allErrors = append(allErrors, field.Invalid(fldPath.Child("apiGroup"), ps.APIGroup, strings.Join(errs, ",")))
	} else if len(strings.Split(ps.APIGroup, ".")) < 2 {
		allErrors = append(allErrors, field.Invalid(fldPath.Child("apiGroup"), ps.APIGroup, "should be a domain with at least one dot"))
	}
	if len(ps.APIVersion) == 0 {
		allErrors = append(allErrors, field.Required(fldPath.Child("apiVersion"), ""))
	}
	if len(ps.APIKind) == 0 {
		allErrors = append(allErrors, field.Required(fldPath.Child("apiKind"), ""))
	}
	if errs := utilvalidation.IsDNS1035Label(ps.APIVersion); len(errs) > 0 {
		allErrors = append(allErrors, field.Invalid(fldPath.Child("apiVersion"), ps.APIVersion, strings.Join(errs, ",")))
	}
	return allErrors
}

func validateMatchResources(mc *admissionregistration.MatchResources, fldPath *field.Path) field.ErrorList {
	var allErrors field.ErrorList
	if mc.MatchPolicy != nil && !supportedMatchPolicies.Has(string(*mc.MatchPolicy)) {
		allErrors = append(allErrors, field.NotSupported(fldPath.Child("matchPolicy"), *mc.MatchPolicy, supportedMatchPolicies.List()))
	}
	for i, namespace := range mc.Namespaces {
		for _, msg := range genericvalidation.ValidateNamespaceName(namespace, false) {
			allErrors = append(allErrors, field.Invalid(fldPath.Child("namespaces").Index(i), namespace, msg))
		}
	}
	for i, namespace := range mc.ExcludeNamespaces {
		for _, msg := range genericvalidation.ValidateNamespaceName(namespace, false) {
			allErrors = append(allErrors, field.Invalid(fldPath.Child("excludeNamespaces").Index(i), namespace, msg))
		}
	}
	if mc.NamespaceSelector != nil {
		allErrors = append(allErrors, metav1validation.ValidateLabelSelector(mc.NamespaceSelector, fldPath.Child("namespaceSelector"))...)
	}

	if mc.LabelSelector != nil {
		allErrors = append(allErrors, metav1validation.ValidateLabelSelector(mc.LabelSelector, fldPath.Child("labelSelector"))...)
	}

	for i, rule := range mc.ResourceRules {
		allErrors = append(allErrors, validateRuleWithOperations(&rule, fldPath.Child("resourceRules").Index(i))...)
	}

	for i, rule := range mc.ExcludeResourceRules {
		allErrors = append(allErrors, validateRuleWithOperations(&rule, fldPath.Child("excludeResourceRules").Index(i))...)
	}

	return allErrors
}

func validateValidation(v *admissionregistration.Validation, fldPath *field.Path) field.ErrorList {
	var allErrors field.ErrorList
	if v.Name == "" {
		allErrors = append(allErrors, field.Required(fldPath.Child("name"), ""))
	}
	for _, msg := range genericvalidation.ValidateNamespaceName(v.Name, false) {
		allErrors = append(allErrors, field.Invalid(fldPath.Child("name"), v.Name, msg))
	}
	trimmedExpression := strings.TrimSpace(v.Expression)
	trimmedMsg := strings.TrimSpace(v.Message)
	trimmedExpressMsg := strings.TrimSpace(v.MessageExpression)
	if len(trimmedExpression) == 0 {
		allErrors = append(allErrors, field.Required(fldPath.Child("expression"), "expression is not specified"))
	} else if len(v.MessageExpression) > 0 && len(trimmedExpressMsg) == 0 {
		allErrors = append(allErrors, field.Invalid(fldPath.Child("expressMessage"), v.MessageExpression, "ExpressMessage must be non-empty if specified"))
	} else if len(v.Message) > 0 && len(trimmedMsg) == 0 {
		allErrors = append(allErrors, field.Invalid(fldPath.Child("message"), v.Message, "message must be non-empty if specified"))
	} else if hasNewlines(trimmedExpressMsg) {
		allErrors = append(allErrors, field.Invalid(fldPath.Child("expressMessage"), v.MessageExpression, "ExpressMessage must not contain line breaks"))
	} else if hasNewlines(trimmedMsg) {
		allErrors = append(allErrors, field.Invalid(fldPath.Child("message"), v.Message, "message must not contain line breaks"))
	} else if hasNewlines(trimmedMsg) && trimmedMsg == "" && trimmedExpressMsg == "" {
		allErrors = append(allErrors, field.Required(fldPath.Child("message"), "message or expressMessage must be specified if expression contains line breaks"))
	}
	return allErrors
}

var newlineMatcher = regexp.MustCompile(`[\n\r]+`) // valid newline chars in CEL grammar
func hasNewlines(s string) bool {
	return newlineMatcher.MatchString(s)
}

// ValidatePolicyBinding validates a PolicyBinding before create.
func ValidatePolicyBinding(pb *admissionregistration.PolicyBinding) field.ErrorList {
	return validatePolicyBinding(pb)
}

func validatePolicyBinding(pb *admissionregistration.PolicyBinding) field.ErrorList {
	allErrors := genericvalidation.ValidateObjectMeta(&pb.ObjectMeta, false, genericvalidation.NameIsDNSSubdomain, field.NewPath("metadata"))
	allErrors = append(allErrors, validatePolicyBindingSpec(&pb.Spec, field.NewPath("spec"))...)

	return allErrors
}

func validatePolicyBindingSpec(spec *admissionregistration.PolicyBindingSpec, fldPath *field.Path) field.ErrorList {
	var allErrors field.ErrorList

	if len(spec.Policy) == 0 {
		allErrors = append(allErrors, field.Required(fldPath.Child("policy"), ""))
	}
	for _, msg := range genericvalidation.NameIsDNSSubdomain(spec.Policy, false) {
		allErrors = append(allErrors, field.Invalid(fldPath.Child("policy"), spec.Policy, msg))
	}
	if len(spec.Param) > 0 {
		for _, msg := range genericvalidation.NameIsDNSSubdomain(spec.Param, false) {
			allErrors = append(allErrors, field.Invalid(fldPath.Child("Param"), spec.Param, msg))
		}
	}
	allErrors = append(allErrors, validateMatchResources(&spec.MatchResources, fldPath.Child("matchResouces"))...)

	return allErrors
}

// ValidateValidatingAdmissionPolicyUpdate validates update of validating admission policy
func ValidateValidatingAdmissionPolicyUpdate(newC, oldC *admissionregistration.ValidatingAdmissionPolicy) field.ErrorList {
	return validateValidatingAdmissionPolicy(newC)
}

// ValidatePolicyBindingUpdate validates update of validating admission policy
func ValidatePolicyBindingUpdate(newC, oldC *admissionregistration.PolicyBinding) field.ErrorList {
	return validatePolicyBinding(newC)
}
