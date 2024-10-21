/*
Copyright 2024 The Kubernetes Authors.

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

// +k8s:validation-gen=TypeMeta
// +k8s:validation-gen-scheme-registry=k8s.io/code-generator/cmd/validation-gen/testscheme.Scheme

package label

import (
	"k8s.io/code-generator/cmd/validation-gen/testscheme"
)

var localSchemeBuilder = testscheme.New()

// From k8s.io/kubernetes/pkg/apis/apps/validation/validation.go
type ReplicaSetExample struct {
	TypeMeta int `json:"typeMeta"`

	// SelectorMatchLabel represents the expected value for template labels
	// +labelConsistency={"label": "app", "required": true}
	SelectorMatchLabel string `json:"selectorMatchLabel,omitempty"`

	Template *PodTemplate `json:"template,omitempty"`
	Metadata ObjectMeta   `json:"metadata,omitempty"`
}

// From k8s.io/kubernetes/pkg/apis/batch/validation/validation.go
type JobExample struct {
	TypeMeta int `json:"typeMeta"`

	// +labelConsistency={"label": "job-name", "required": true}
	Name string `json:"name,omitempty"`

	// +labelConsistency={"label": "controller-uid", "required": true}
	UID string `json:"uid,omitempty"`

	Template *PodTemplate `json:"template,omitempty"`
	Metadata ObjectMeta   `json:"metadata,omitempty"`
}

// From k8s.io/kubernetes/pkg/apis/core/validation/validation.go
type NodeExample struct {
	TypeMeta int `json:"typeMeta"`

	// +labelConsistency={"label": "node-restriction", "required": true}
	AllowedLabelValue string `json:"allowedLabelValue,omitempty"`

	Metadata ObjectMeta `json:"metadata,omitempty"`
}

// Supporting types
type PodTemplate struct {
	Metadata ObjectMeta `json:"metadata,omitempty"`
}

type ObjectMeta struct {
	Name   string            `json:"name,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}
