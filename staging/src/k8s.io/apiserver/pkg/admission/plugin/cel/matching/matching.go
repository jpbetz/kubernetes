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

package matching

import (
	"fmt"

	v1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/api/admissionregistration/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/client-go/kubernetes"
	listersv1 "k8s.io/client-go/listers/core/v1"

	"k8s.io/apiserver/pkg/admission/plugin/webhook/predicates/namespace"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/predicates/object"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/predicates/rules"
)

type MatchCriteria interface {
	namespace.NamespaceSelectorProvider
	object.ObjectSelectorProvider

	GetMatchResources() v1alpha1.MatchResources
}

// Matcher decides if a request matches against matchCriteria
type Matcher struct {
	namespaceMatcher *namespace.Matcher
	objectMatcher    *object.Matcher
}

// NewMatcher initialize the matcher with dependencies requires
func NewMatcher(
	namespaceLister listersv1.NamespaceLister,
	client kubernetes.Interface,
) *Matcher {
	return &Matcher{
		namespaceMatcher: &namespace.Matcher{
			NamespaceLister: namespaceLister,
			Client:          client,
		},
		objectMatcher: &object.Matcher{},
	}
}

// ValidateInitialization verify if the matcher is ready before use
func (m *Matcher) ValidateInitialization() error {
	if err := m.namespaceMatcher.Validate(); err != nil {
		return fmt.Errorf("namespaceMatcher is not properly setup: %v", err)
	}
	return nil
}

func (m *Matcher) Matches(attr admission.Attributes, o admission.ObjectInterfaces, criteria MatchCriteria, isBinding bool) (bool, error) {
	matches, err := m.namespaceMatcher.MatchNamespaceSelector(criteria, attr)
	if err != nil {
		return false, err
	}
	if !matches {
		return false, nil
	}

	matches, err = m.objectMatcher.MatchObjectSelector(criteria, attr)
	if err != nil {
		return false, err
	}
	if !matches {
		return false, nil
	}

	matchResources := criteria.GetMatchResources()
	matchPolicy := matchResources.MatchPolicy
	if matchesResourceRules(matchResources.ExcludeResourceRules, matchPolicy, attr, o) {
		return false, nil
	}

	if isBinding && matchResources.ResourceRules == nil {
		return true, nil
	}

	if !matchesResourceRules(matchResources.ResourceRules, matchPolicy, attr, o) {
		return false, nil
	}

	return true, nil
}

// TODO: for equivalent matching, this needs to return what GVk was matched
func matchesResourceRules(namedRules []v1alpha1.NamedRuleWithOperations, matchPolicy *v1alpha1.MatchPolicyType, attr admission.Attributes, o admission.ObjectInterfaces) bool {
	for _, namedRule := range namedRules {
		rule := v1.RuleWithOperations(namedRule.RuleWithOperations)
		ruleMatcher := rules.Matcher{
			Rule: rule,
			Attr: attr,
		}
		if !ruleMatcher.Matches() {
			continue
		}
		// an empty name list always matches
		if len(namedRule.ResourceNames) == 0 {
			return true
		}
		// TODO: GetName() can return an empty string if the user is relying on
		// the API server to generate the name... figure out what to do for this edge case
		name := attr.GetName()
		for _, matchedName := range namedRule.ResourceNames {
			if name == matchedName {
				return true
			}
		}
	}

	// if match policy is undefined or exact, don't perform fuzzy matching
	// note that defaulting to fuzzy matching is set by the API
	if matchPolicy == nil || *matchPolicy == v1alpha1.Exact {
		return false
	}

	attrWithOverride := &attrWithResourceOverride{Attributes: attr}
	equivalents := o.GetEquivalentResourceMapper().EquivalentResourcesFor(attr.GetResource(), attr.GetSubresource())
	for _, namedRule := range namedRules {
		for _, equivalent := range equivalents {
			if equivalent == attr.GetResource() {
				// we have already checked the original resource
				continue
			}
			attrWithOverride.resource = equivalent
			rule := v1.RuleWithOperations(namedRule.RuleWithOperations)
			m := rules.Matcher{
				Rule: rule,
				Attr: attrWithOverride,
			}
			if !m.Matches() {
				continue
			}
			// an empty name list always matches
			if len(namedRule.ResourceNames) == 0 {
				return true
			}
			// TODO: GetName() can return an empty string if the user is relying on
			// the API server to generate the name... figure out what to do for this edge case
			name := attr.GetName()
			for _, matchedName := range namedRule.ResourceNames {
				if name == matchedName {
					return true
				}
			}

		}
	}
	return false
}

type attrWithResourceOverride struct {
	admission.Attributes
	resource schema.GroupVersionResource
}

func (a *attrWithResourceOverride) GetResource() schema.GroupVersionResource { return a.resource }
