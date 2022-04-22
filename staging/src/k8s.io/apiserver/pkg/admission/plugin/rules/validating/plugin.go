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
	"io"

	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/admission/configuration"
	generic "k8s.io/apiserver/pkg/admission/plugin/rules/generic"
)

const (
	// PluginName indicates the name of admission plug-in
	PluginName = "ValidatingRules"
)

// Register registers a plugin
func Register(plugins *admission.Plugins) {
	plugins.Register(PluginName, func(configFile io.Reader) (admission.Interface, error) {
		plugin, err := NewValidatingAdmissionRules(configFile) // TODO
		if err != nil {
			return nil, err
		}

		return plugin, nil
	})
}

// Plugin is an implementation of admission.Interface.
type Plugin struct {
	*generic.Rules
}

func (a *Plugin) Handles(operation admission.Operation) bool {
	//TODO implement me
	panic("implement me")
}

var _ admission.ValidationInterface = &Plugin{}

// NewValidatingAdmissionRules returns a generic admission webhook plugin.
func NewValidatingAdmissionRules(configFile io.Reader) (*Plugin, error) {
	handler := admission.NewHandler(admission.Connect, admission.Create, admission.Delete, admission.Update)
	p := &Plugin{}
	var err error
	p.Rules, err = generic.NewRules(handler, configFile, configuration.NewValidatingRulesConfigurationManager, newValidatingEvaluator(p))
	if err != nil {
		return nil, err
	}
	return p, nil
}

// Validate makes an admission decision based on the request attributes.
func (a *Plugin) Validate(ctx context.Context, attr admission.Attributes, o admission.ObjectInterfaces) error {
	return a.Rules.Evaluate(ctx, attr, o)
}
