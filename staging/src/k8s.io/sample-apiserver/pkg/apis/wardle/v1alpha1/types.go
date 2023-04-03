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

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// Flunder
// +validations=rule:"!has(self.metadata.name) || self.metadata.name.isFormat('dns1123subdomain')"
// +validations=rule:"!has(self.metadata.generateName) || self.metadata.generateName.isGenerateNameOfFormat('dns1123subdomain')"
type Flunder struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// +validations=rule:"has(self.specializations) || has(self.componentValues)"
	Spec   FlunderSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status FlunderStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

type FlunderSpec struct {
	// A name of another flunder or fischer, depending on the reference type.
	// +format=ipv4
	Reference string `json:"reference,omitempty" protobuf:"bytes,1,opt,name=reference"`
	// The reference type, defaults to "Flunder" if reference is set.
	ReferenceType *ReferenceType `json:"referenceType,omitempty" protobuf:"bytes,2,opt,name=referenceType"`

	// widgets
	// Required
	// +listType=set
	// +maxItems=5
	Widgets []string `json:"widgets" protobuf:"bytes,3,rep,name=widgets"`

	// primary
	// +optional
	Primary Color `json:"primary,omitempty" protobuf:"bytes,4,opt,name=primary"`

	// priority
	// +optional
	// +minimum=-100
	// +maximum=100
	// +validations=rule:"self > 75 || self < 25"
	Priority int64 `json:"priority,omitempty" protobuf:"bytes,5,opt,name=priority"`

	// weight
	// Required.
	Weight float64 `json:"weight,omitempty" protobuf:"bytes,6,opt,name=weight"`

	// componentValues
	// Required.
	// +listType=map
	// +listMapKey=name
	// +listMapKey=stage
	ComponentValues []ComponentTarget `json:"componentValues" protobuf:"bytes,7,rep,name=componentValues"`

	// provisions
	// Required.
	// +maxProperties=3
	Specializations map[string]string `json:"specializations" protobuf:"bytes,8,opt,name=specializations"`

	// reticulated
	// +default=true
	Reticulated bool `json:"reticulated,omitempty" protobuf:"bytes,8,opt,name=reticulated"`
}

type ComponentTarget struct {
	// name
	// Required.
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`

	// stage
	// Required.
	Stage string `json:"stage,omitempty" protobuf:"bytes,2,opt,name=stage"`

	// name
	// +optional
	Grade int32 `json:"grade,omitempty" protobuf:"bytes,3,opt,name=grade"`
}

// Color
// +enum
type Color string

const (
	Red   Color = "Red"
	Blue  Color = "Blue"
	Green Color = "Green"
)

// +enum
type ReferenceType string

const (
	FlunderReferenceType = ReferenceType("Flunder")
	FischerReferenceType = ReferenceType("Fischer")
)

type FlunderStatus struct {
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FlunderList is a list of Flunder objects.
type FlunderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Items []Flunder `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Fischer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// DisallowedFlunders holds a list of Flunder.Names that are disallowed.
	DisallowedFlunders []string `json:"disallowedFlunders,omitempty" protobuf:"bytes,2,rep,name=disallowedFlunders"`
}

// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FischerList is a list of Fischer objects.
type FischerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Items []Fischer `json:"items" protobuf:"bytes,2,rep,name=items"`
}
