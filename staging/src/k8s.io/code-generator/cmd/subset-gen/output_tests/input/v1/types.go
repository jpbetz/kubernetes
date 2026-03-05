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

// Package v1 contains test input types for subset-gen output tests.
package v1

// TypeMeta describes an individual object in an API response or request
// with strings representing the type of the object and its API schema version.
type TypeMeta struct {
	Kind       string `json:"kind,omitempty" protobuf:"bytes,1,opt,name=kind"`
	APIVersion string `json:"apiVersion,omitempty" protobuf:"bytes,2,opt,name=apiVersion"`
}

// ObjectMeta is metadata that all persisted resources must have.
type ObjectMeta struct {
	Name         string            `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	Namespace    string            `json:"namespace,omitempty" protobuf:"bytes,3,opt,name=namespace"`
	Labels       map[string]string `json:"labels,omitempty" protobuf:"bytes,11,rep,name=labels"`
	Annotations  map[string]string `json:"annotations,omitempty" protobuf:"bytes,12,rep,name=annotations"`
	GenerateName string            `json:"generateName,omitempty" protobuf:"bytes,2,opt,name=generateName"`
}

// +genclient
// +genclient:method=GetScale,verb=get,subresource=scale
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Deployment enables declarative updates for Pods and ReplicaSets.
type Deployment struct {
	TypeMeta   `json:",inline"`
	ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   DeploymentSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status DeploymentStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DeploymentList is a list of Deployments.
type DeploymentList struct {
	TypeMeta `json:",inline"`
	ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Items []Deployment `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// ListMeta describes metadata for a list.
type ListMeta struct {
	ResourceVersion string `json:"resourceVersion,omitempty" protobuf:"bytes,2,opt,name=resourceVersion"`
	Continue        string `json:"continue,omitempty" protobuf:"bytes,3,opt,name=continue"`
}

// DeploymentSpec is the specification of the desired behavior of the Deployment.
type DeploymentSpec struct {
	Replicas *int32            `json:"replicas,omitempty" protobuf:"varint,1,opt,name=replicas"`
	Selector *LabelSelector    `json:"selector" protobuf:"bytes,2,opt,name=selector"`
	Template PodTemplateSpec   `json:"template" protobuf:"bytes,3,opt,name=template"`
	Strategy DeploymentStrategy `json:"strategy,omitempty" protobuf:"bytes,4,opt,name=strategy"`
	Paused   bool              `json:"paused,omitempty" protobuf:"varint,7,opt,name=paused"`
}

// DeploymentStatus is the most recently observed status of the Deployment.
type DeploymentStatus struct {
	Replicas          int32 `json:"replicas,omitempty" protobuf:"varint,1,opt,name=replicas"`
	UpdatedReplicas   int32 `json:"updatedReplicas,omitempty" protobuf:"varint,3,opt,name=updatedReplicas"`
	AvailableReplicas int32 `json:"availableReplicas,omitempty" protobuf:"varint,4,opt,name=availableReplicas"`
}

// DeploymentStrategy describes how to replace existing pods with new ones.
type DeploymentStrategy struct {
	Type string `json:"type,omitempty" protobuf:"bytes,1,opt,name=type"`
}

// LabelSelector is a label query over a set of resources.
type LabelSelector struct {
	MatchLabels      map[string]string          `json:"matchLabels,omitempty" protobuf:"bytes,1,rep,name=matchLabels"`
	MatchExpressions []LabelSelectorRequirement `json:"matchExpressions,omitempty" protobuf:"bytes,2,rep,name=matchExpressions"`
}

// LabelSelectorRequirement is a selector that contains values, a key, and an operator.
type LabelSelectorRequirement struct {
	Key      string   `json:"key" protobuf:"bytes,1,opt,name=key"`
	Operator string   `json:"operator" protobuf:"bytes,2,opt,name=operator"`
	Values   []string `json:"values,omitempty" protobuf:"bytes,3,rep,name=values"`
}

// PodTemplateSpec describes the data a pod should have when created from a template.
type PodTemplateSpec struct {
	ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec PodSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
}

// PodSpec is a description of a pod.
type PodSpec struct {
	Containers     []Container `json:"containers" protobuf:"bytes,2,rep,name=containers"`
	InitContainers []Container `json:"initContainers,omitempty" protobuf:"bytes,20,rep,name=initContainers"`
	NodeName       string      `json:"nodeName,omitempty" protobuf:"bytes,10,opt,name=nodeName"`
	HostNetwork    bool        `json:"hostNetwork,omitempty" protobuf:"varint,11,opt,name=hostNetwork"`
}

// Container represents a single container in a pod.
type Container struct {
	Name    string   `json:"name" protobuf:"bytes,1,opt,name=name"`
	Image   string   `json:"image,omitempty" protobuf:"bytes,2,opt,name=image"`
	Command []string `json:"command,omitempty" protobuf:"bytes,3,rep,name=command"`
	Args    []string `json:"args,omitempty" protobuf:"bytes,4,rep,name=args"`
	Ports   []ContainerPort `json:"ports,omitempty" protobuf:"bytes,6,rep,name=ports"`
}

// ContainerPort represents a network port in a single container.
type ContainerPort struct {
	Name          string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	ContainerPort int32  `json:"containerPort" protobuf:"varint,3,opt,name=containerPort"`
	Protocol      string `json:"protocol,omitempty" protobuf:"bytes,4,opt,name=protocol"`
}
