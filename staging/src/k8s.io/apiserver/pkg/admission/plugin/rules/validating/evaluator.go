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

package validating

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/klog/v2"

	v1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/admission/plugin/rules"
	"k8s.io/apiserver/pkg/admission/plugin/rules/generic"
)

type validatingEvaluator struct {
	plugin *Plugin
}

func newValidatingEvaluator(p *Plugin) func() generic.Evaluator {
	return func() generic.Evaluator {
		return &validatingEvaluator{p}
	}
}

var _ generic.Evaluator = &validatingEvaluator{}

func (d *validatingEvaluator) Evaluate(ctx context.Context, attr admission.Attributes, o admission.ObjectInterfaces, hooks []rules.RuleAccessor) error {
	var relevantHooks []*generic.RuleInvocation
	// Construct all the versions we need to call our webhooks
	versionedAttrs := map[schema.GroupVersionKind]*generic.VersionedAttributes{}
	for _, hook := range hooks {
		invocation, statusError := d.plugin.ShouldCallRule(hook, attr, o)
		if statusError != nil {
			return statusError
		}
		if invocation == nil {
			continue
		}
		relevantHooks = append(relevantHooks, invocation)
		// If we already have this version, continue
		if _, ok := versionedAttrs[invocation.Kind]; ok {
			continue
		}
		versionedAttr, err := generic.NewVersionedAttributes(attr, invocation.Kind, o)
		if err != nil {
			return apierrors.NewInternalError(err)
		}
		versionedAttrs[invocation.Kind] = versionedAttr
	}

	if len(relevantHooks) == 0 {
		// no matching rules
		return nil
	}

	wg := sync.WaitGroup{} // TODO: serialize?
	errCh := make(chan error, len(relevantHooks))
	wg.Add(len(relevantHooks))
	for i := range relevantHooks {
		go func(invocation *generic.RuleInvocation, idx int) {
			defer wg.Done()
			r, ok := invocation.Rule.GetValidatingRule()
			if !ok {
				utilruntime.HandleError(fmt.Errorf("validating webhook dispatch requires v1.ValidatingWebhook, but got %T", r))
				return
			}
			versionedAttr := versionedAttrs[invocation.Kind]
			err := d.evalRule(ctx, r, invocation, versionedAttr)
			if err != nil {
				klog.Warningf("rejected by rule %q: %#v", r.Name, err)
			}
			errCh <- err
		}(relevantHooks[i], i)
	}
	wg.Wait()
	close(errCh)

	var errs []error
	for e := range errCh {
		errs = append(errs, e)
	}
	if len(errs) == 0 {
		return nil
	}
	if len(errs) > 1 {
		for i := 1; i < len(errs); i++ {
			// TODO: merge status errors; until then, just return the first one.
			utilruntime.HandleError(errs[i])
		}
	}
	return errs[0]
}

func (d *validatingEvaluator) evalRule(ctx context.Context, h *v1.ValidatingRule, invocation *generic.RuleInvocation, attr *generic.VersionedAttributes) error {
	// TODO need to load the handler for the type?

	for _, validation := range h.Validations {
		//validation.Rule
		obj := attr.GetObject()
		switch obj.(type) {
		case *unstructured.Unstructured:
			// TODO: need the schema from the CRD
			//cel.Compile() // TODO: compile ahead of time
			//t.Object
			klog.Infof("validating rule %s for custom resource", validation.Rule)
		default:

			klog.Infof("validating rule %s", validation.Rule)
		}
	}
	return nil
}
