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

package expressions

import (
	"k8s.io/apimachinery/pkg/runtime"
)

type Selector interface {
	// Matches returns true if this selector matches the given object.
	Matches(runtime.Object) bool

	// Empty returns true if this selector does not restrict the selection space.
	Empty() bool

	// String returns a human-readable string that represents this selector.
	String() string

	DeepCopySelector() Selector
}

func Everything() Selector {
	return emptyRule{}
}

func RuleSelector(rule string) Selector {
	if rule == "" {
		return emptyRule{}
	}
	return rawSelector{Rule: rule}
}

func ParseSelector(rule string) (Selector, error) {
	return RuleSelector(rule), nil
}

type emptyRule struct{}

func (e emptyRule) Matches(object runtime.Object) bool { return true }
func (e emptyRule) Empty() bool                        { return true }
func (e emptyRule) String() string                     { return "" }
func (e emptyRule) DeepCopySelector() Selector         { return e }

type rawSelector struct {
	Rule string
}

func (r rawSelector) Matches(object runtime.Object) bool {
	return false // TODO: should be an error
}

func (r rawSelector) Empty() bool {
	return false
}

func (r rawSelector) String() string {
	return r.Rule
}

func (r rawSelector) DeepCopySelector() Selector {
	return rawSelector{Rule: r.Rule}
}
