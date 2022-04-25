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

package generic

import (
	"context"
	"fmt"
	"io"

	admissionv1 "k8s.io/api/admission/v1"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	"k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	rules2 "k8s.io/apiserver/pkg/admission/plugin/rules"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/config"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/namespace"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/object"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/rules"
	webhookutil "k8s.io/apiserver/pkg/util/webhook"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
)

// Rules is an abstract admission plugin with all the infrastructure to define Admit or Validate on-top.
type Rules struct {
	*admission.Handler

	sourceFactory sourceFactory

	hookSource       Source
	namespaceMatcher *namespace.Matcher
	objectMatcher    *object.Matcher
	evaluator        Evaluator
}

var (
	_ admission.Interface = &Rules{}
)

type sourceFactory func(f informers.SharedInformerFactory) Source
type evaluatorFactory func() Evaluator

// NewRules creates a new generic admission webhook.
func NewRules(handler *admission.Handler, configFile io.Reader, sourceFactory sourceFactory, evaluatorFactory evaluatorFactory) (*Rules, error) {
	kubeconfigFile, err := config.LoadConfig(configFile)
	if err != nil {
		return nil, err
	}

	cm, err := webhookutil.NewClientManager(
		[]schema.GroupVersion{
			admissionv1beta1.SchemeGroupVersion,
			admissionv1.SchemeGroupVersion,
		},
		admissionv1beta1.AddToScheme,
		admissionv1.AddToScheme,
	)
	if err != nil {
		return nil, err
	}
	authInfoResolver, err := webhookutil.NewDefaultAuthenticationInfoResolver(kubeconfigFile)
	if err != nil {
		return nil, err
	}
	// Set defaults which may be overridden later.
	cm.SetAuthenticationInfoResolver(authInfoResolver)
	cm.SetServiceResolver(webhookutil.NewDefaultServiceResolver())

	return &Rules{
		Handler:          handler,
		sourceFactory:    sourceFactory,
		namespaceMatcher: &namespace.Matcher{},
		objectMatcher:    &object.Matcher{},
		evaluator:        evaluatorFactory(),
	}, nil
}

// SetExternalKubeClientSet implements the WantsExternalKubeInformerFactory interface.
// It sets external ClientSet for admission plugins that need it
func (a *Rules) SetExternalKubeClientSet(client clientset.Interface) {
	a.namespaceMatcher.Client = client
}

// SetExternalKubeInformerFactory implements the WantsExternalKubeInformerFactory interface.
func (a *Rules) SetExternalKubeInformerFactory(f informers.SharedInformerFactory) {
	namespaceInformer := f.Core().V1().Namespaces()
	a.namespaceMatcher.NamespaceLister = namespaceInformer.Lister()
	a.hookSource = a.sourceFactory(f)
	a.SetReadyFunc(func() bool {
		return namespaceInformer.Informer().HasSynced() && a.hookSource.HasSynced()
	})
}

// ValidateInitialization implements the InitializationValidator interface.
func (a *Rules) ValidateInitialization() error {
	if a.hookSource == nil {
		return fmt.Errorf("kubernetes client is not properly setup")
	}
	if err := a.namespaceMatcher.Validate(); err != nil {
		return fmt.Errorf("namespaceMatcher is not properly setup: %v", err)
	}
	return nil
}

// ShouldCallRule returns invocation details if the webhook should be called, nil if the webhook should not be called,
// or an error if an error was encountered during evaluation.
func (a *Rules) ShouldCallRule(h rules2.RuleAccessor, attr admission.Attributes, o admission.ObjectInterfaces) (*RuleInvocation, *apierrors.StatusError) {
	matches, matchNsErr := a.namespaceMatcher.MatchNamespaceSelectorForRule(h, attr)
	// Should not return an error here for webhooks which do not apply to the request, even if err is an unexpected scenario.
	if !matches && matchNsErr == nil {
		return nil, nil
	}

	// Should not return an error here for webhooks which do not apply to the request, even if err is an unexpected scenario.
	matches, matchObjErr := a.objectMatcher.MatchObjectSelectorForRule(h, attr)
	if !matches && matchObjErr == nil {
		return nil, nil
	}

	var invocation *RuleInvocation
	for _, r := range h.GetMatchRules() {
		m := rules.Matcher{Rule: r, Attr: attr}
		if m.Matches() {
			invocation = &RuleInvocation{
				Rule:        h,
				Resource:    attr.GetResource(),
				Subresource: attr.GetSubresource(),
				Kind:        attr.GetKind(),
			}
			break
		}
	}
	if invocation == nil && h.GetMatchPolicy() != nil && *h.GetMatchPolicy() == v1.Equivalent {
		attrWithOverride := &attrWithResourceOverride{Attributes: attr}
		equivalents := o.GetEquivalentResourceMapper().EquivalentResourcesFor(attr.GetResource(), attr.GetSubresource())
		// honor earlier rules first
	OuterLoop:
		for _, r := range h.GetMatchRules() {
			// see if the rule matches any of the equivalent resources
			for _, equivalent := range equivalents {
				if equivalent == attr.GetResource() {
					// exclude attr.GetResource(), which we already checked
					continue
				}
				attrWithOverride.resource = equivalent
				m := rules.Matcher{Rule: r, Attr: attrWithOverride}
				if m.Matches() {
					kind := o.GetEquivalentResourceMapper().KindFor(equivalent, attr.GetSubresource())
					if kind.Empty() {
						return nil, apierrors.NewInternalError(fmt.Errorf("unable to convert to %v: unknown kind", equivalent))
					}
					invocation = &RuleInvocation{
						Rule:        h,
						Resource:    equivalent,
						Subresource: attr.GetSubresource(),
						Kind:        kind,
					}
					break OuterLoop
				}
			}
		}
	}

	if invocation == nil {
		return nil, nil
	}
	if matchNsErr != nil {
		return nil, matchNsErr
	}
	if matchObjErr != nil {
		return nil, matchObjErr
	}

	return invocation, nil
}

type attrWithResourceOverride struct {
	admission.Attributes
	resource schema.GroupVersionResource
}

func (a *attrWithResourceOverride) GetResource() schema.GroupVersionResource { return a.resource }

// Evaluate is called by the downstream Validate or Admit methods.
func (a *Rules) Evaluate(ctx context.Context, attr admission.Attributes, o admission.ObjectInterfaces) error {
	if rules.IsWebhookConfigurationResource(attr) {
		return nil
	}
	if !a.WaitForReady() {
		return admission.NewForbidden(attr, fmt.Errorf("not yet ready to handle request"))
	}
	rules := a.hookSource.Rules()
	return a.evaluator.Evaluate(ctx, attr, o, rules)
}
