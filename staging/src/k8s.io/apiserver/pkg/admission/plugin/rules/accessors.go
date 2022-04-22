/*
Copyright 2019 The Kubernetes Authors.

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

package rules

import (
	"sync"

	"k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/rest"
)

type RuleAccessor interface { // TODO: organize to share common definition with RuleAccessor
	// GetUID gets a string that uniquely identifies the webhook.
	GetUID() string

	// GetConfigurationName gets the name of the webhook configuration that owns this webhook.
	GetConfigurationName() string

	// GetParsedNamespaceSelector gets the webhook NamespaceSelector field.
	GetParsedNamespaceSelector() (labels.Selector, error)
	// GetParsedObjectSelector gets the webhook ObjectSelector field.
	GetParsedObjectSelector() (labels.Selector, error)

	// GetName gets the webhook Name field. Note that the name is scoped to the webhook
	// configuration and does not provide a globally unique identity, if a unique identity is
	// needed, use GetUID.
	GetName() string
	// GetMatchRules gets the webhook Rules field.
	GetMatchRules() []v1.RuleWithOperations
	// GetMatchPolicy gets the webhook MatchPolicy field.
	GetMatchPolicy() *v1.MatchPolicyType
	// GetNamespaceSelector gets the webhook NamespaceSelector field.
	GetNamespaceSelector() *metav1.LabelSelector
	// GetObjectSelector gets the webhook ObjectSelector field.
	GetObjectSelector() *metav1.LabelSelector

	// GetValidatingRule if the accessor contains a ValidatingRule, returns it and true, else returns false.
	GetValidatingRule() (*v1.ValidatingRule, bool)
}

// NewValidatingRuleAccessor creates an accessor for a ValidatingRule.
func NewValidatingRuleAccessor(uid, configurationName string, h *v1.ValidatingRule) RuleAccessor {
	return &ValidatingRuleAccessor{uid: uid, configurationName: configurationName, ValidatingRule: h}
}

type ValidatingRuleAccessor struct {
	*v1.ValidatingRule
	uid               string
	configurationName string

	initObjectSelector sync.Once
	objectSelector     labels.Selector
	objectSelectorErr  error

	initNamespaceSelector sync.Once
	namespaceSelector     labels.Selector
	namespaceSelectorErr  error

	initClient sync.Once
	client     *rest.RESTClient
	clientErr  error
}

func (m *ValidatingRuleAccessor) GetUID() string {
	return m.uid
}

func (m *ValidatingRuleAccessor) GetConfigurationName() string {
	return m.configurationName
}

func (m *ValidatingRuleAccessor) GetParsedNamespaceSelector() (labels.Selector, error) {
	m.initNamespaceSelector.Do(func() {
		m.namespaceSelector, m.namespaceSelectorErr = metav1.LabelSelectorAsSelector(m.NamespaceSelector)
	})
	return m.namespaceSelector, m.namespaceSelectorErr
}

func (m *ValidatingRuleAccessor) GetParsedObjectSelector() (labels.Selector, error) {
	m.initObjectSelector.Do(func() {
		m.objectSelector, m.objectSelectorErr = metav1.LabelSelectorAsSelector(m.ObjectSelector)
	})
	return m.objectSelector, m.objectSelectorErr
}

func (m *ValidatingRuleAccessor) GetName() string {
	return m.Name
}

func (m *ValidatingRuleAccessor) GetMatchRules() []v1.RuleWithOperations {
	return m.MatchRules
}

func (m *ValidatingRuleAccessor) GetMatchPolicy() *v1.MatchPolicyType {
	return m.MatchPolicy
}

func (m *ValidatingRuleAccessor) GetNamespaceSelector() *metav1.LabelSelector {
	return m.NamespaceSelector
}

func (m *ValidatingRuleAccessor) GetObjectSelector() *metav1.LabelSelector {
	return m.ObjectSelector
}

func (m *ValidatingRuleAccessor) GetValidatingRule() (*v1.ValidatingRule, bool) {
	return m.ValidatingRule, true
}

func (m *ValidatingRuleAccessor) GetValidatingWebhook() (*v1.ValidatingWebhook, bool) {
	return nil, false
}
