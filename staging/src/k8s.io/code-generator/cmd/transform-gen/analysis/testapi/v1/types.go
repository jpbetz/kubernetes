/*
Copyright 2025 The Kubernetes Authors.

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

// Package v1 contains test API types for the field usage analyzer tests.
package v1

// TypeMeta describes an individual object in an API response or request.
type TypeMeta struct {
	Kind       string `json:"kind,omitempty"`
	APIVersion string `json:"apiVersion,omitempty"`
}

// ObjectMeta is metadata that all persisted resources must have.
type ObjectMeta struct {
	Name         string            `json:"name,omitempty"`
	Namespace    string            `json:"namespace,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	ManagedFields []byte           `json:"managedFields,omitempty"`
}

// MyResource is a test API type.
type MyResource struct {
	TypeMeta   `json:",inline"`
	ObjectMeta `json:"metadata,omitempty"`

	Spec   MyResourceSpec   `json:"spec,omitempty"`
	Status MyResourceStatus `json:"status,omitempty"`
}

// MyResourceSpec is the spec of MyResource.
type MyResourceSpec struct {
	Replicas *int32         `json:"replicas,omitempty"`
	Selector *LabelSelector `json:"selector,omitempty"`
	Template string         `json:"template,omitempty"`
}

// MyResourceStatus is the status of MyResource.
type MyResourceStatus struct {
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`
}

// LabelSelector is a label query over a set of resources.
type LabelSelector struct {
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}
