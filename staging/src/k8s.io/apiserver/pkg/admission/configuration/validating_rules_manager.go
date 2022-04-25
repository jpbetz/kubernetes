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

package configuration

import (
	"fmt"
	"sort"
	"sync/atomic"

	"k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apiserver/pkg/admission/plugin/rules"
	"k8s.io/apiserver/pkg/admission/plugin/rules/generic"
	"k8s.io/client-go/informers"
	admissionregistrationlisters "k8s.io/client-go/listers/admissionregistration/v1"
	"k8s.io/client-go/tools/cache"
)

// validatingRulesConfigurationManager collects the validating Rules objects so that they can be called.
type validatingRulesConfigurationManager struct {
	configuration *atomic.Value
	lister        admissionregistrationlisters.ValidatingRuleConfigurationLister
	hasSynced     func() bool
	// initialConfigurationSynced stores a boolean value, which tracks if
	// the existing Rules configs have been synced (honored) by the
	// manager at startup-- the informer has synced and either has no items
	// or has finished executing updateConfiguration() once.
	initialConfigurationSynced *atomic.Value
}

var _ generic.Source = &validatingRulesConfigurationManager{}

func NewValidatingRulesConfigurationManager(f informers.SharedInformerFactory) generic.Source {
	informer := f.Admissionregistration().V1().ValidatingRuleConfigurations()
	manager := &validatingRulesConfigurationManager{
		configuration:              &atomic.Value{},
		lister:                     informer.Lister(),
		hasSynced:                  informer.Informer().HasSynced,
		initialConfigurationSynced: &atomic.Value{},
	}

	// Start with an empty list
	manager.configuration.Store([]rules.RuleAccessor{})
	manager.initialConfigurationSynced.Store(false)

	// On any change, rebuild the config
	informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { manager.updateConfiguration() },
		UpdateFunc: func(_, _ interface{}) { manager.updateConfiguration() },
		DeleteFunc: func(_ interface{}) { manager.updateConfiguration() },
	})

	return manager
}

// Rules returns the merged ValidatingRulesConfiguration.
func (v *validatingRulesConfigurationManager) Rules() []rules.RuleAccessor {
	return v.configuration.Load().([]rules.RuleAccessor)
}

// HasSynced returns true when the manager is synced with existing Rulesconfig
// objects at startup-- which means the informer is synced and either has no items
// or updateConfiguration() has completed.
func (v *validatingRulesConfigurationManager) HasSynced() bool {
	if !v.hasSynced() {
		return false
	}
	if v.initialConfigurationSynced.Load().(bool) {
		// the informer has synced and configuration has been updated
		return true
	}
	if configurations, err := v.lister.List(labels.Everything()); err == nil && len(configurations) == 0 {
		// the empty list we initially stored is valid to use.
		// Setting initialConfigurationSynced to true, so subsequent checks
		// would be able to take the fast path on the atomic boolean in a
		// cluster without any admission Ruless configured.
		v.initialConfigurationSynced.Store(true)
		// the informer has synced and we don't have any items
		return true
	}
	return false

}

func (v *validatingRulesConfigurationManager) updateConfiguration() {
	configurations, err := v.lister.List(labels.Everything())
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error updating configuration: %v", err))
		return
	}
	v.configuration.Store(mergeValidatingRulesConfigurations(configurations))
	v.initialConfigurationSynced.Store(true)
}

func mergeValidatingRulesConfigurations(configurations []*v1.ValidatingRuleConfiguration) []rules.RuleAccessor {
	sort.SliceStable(configurations, ValidatingRulesConfigurationSorter(configurations).ByName)
	var accessors []rules.RuleAccessor
	for _, c := range configurations {
		// Rules names are not validated for uniqueness, so we check for duplicates and
		// add a int suffix to distinguish between them
		names := map[string]int{}
		for i := range c.ValidatingRules {
			n := c.ValidatingRules[i].Name
			uid := fmt.Sprintf("%s/%s/%d", c.Name, n, names[n])
			names[n]++
			accessors = append(accessors, rules.NewValidatingRuleAccessor(uid, c.Name, &c.ValidatingRules[i]))
		}
	}
	return accessors
}

type ValidatingRulesConfigurationSorter []*v1.ValidatingRuleConfiguration

func (a ValidatingRulesConfigurationSorter) ByName(i, j int) bool {
	return a[i].Name < a[j].Name
}
