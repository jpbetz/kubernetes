/*
Copyright 2026 The Kubernetes Authors.

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

package fielddrops

// Root is the strategy entry type. DropDisabledFields_Root (spec scope) and
// DropDisabledStatusFields_Root (status scope) are generated for it.
type Root struct {
	TypeMeta int
	Spec     RootSpec
	Status   RootStatus
}

// RootStatus exercises status-scope dropping: its gated leaf must land in
// DropDisabledStatusFields_Root, not the spec entry.
type RootStatus struct {
	// +k8s:featureGate(drop: true)=StatusGate
	// +k8s:optional
	Allocation *Allocation `json:"allocation,omitempty"`
}

// Allocation is a status sub-object.
type Allocation struct {
	Device string `json:"device"`
}

// RootSpec exercises a direct pointer drop leaf and nested recursion.
type RootSpec struct {
	// Priority is a direct pointer drop leaf.
	// +k8s:featureGate(drop: true)=PointerLeafGate
	// +k8s:optional
	Priority *bool `json:"priority,omitempty"`

	// Mode is a value-typed (string) drop leaf, cleared to "" when dropped.
	// +k8s:featureGate(drop: true)=ValueLeafGate
	// +k8s:optional
	Mode string `json:"mode,omitempty"`

	// Requests reaches the same gate (MultiLeafGate) through two sub-types,
	// exercising multi-leaf in-use.
	// +k8s:optional
	// +k8s:listType=atomic
	Requests []Request `json:"requests,omitempty"`
}

// Request holds one drop leaf directly and one via a pointer-to-struct.
type Request struct {
	Name string `json:"name"`

	// Tolerations is a slice drop leaf reached via a slice-of-struct.
	// +k8s:featureGate(drop: true)=MultiLeafGate
	// +k8s:optional
	// +k8s:listType=atomic
	Tolerations []Toleration `json:"tolerations,omitempty"`

	// Exactly is a pointer-to-struct that itself holds a drop leaf.
	// +k8s:optional
	Exactly *Exactly `json:"exactly,omitempty"`
}

// Exactly holds a drop leaf reached via a pointer-to-struct.
type Exactly struct {
	// +k8s:featureGate(drop: true)=MultiLeafGate
	// +k8s:optional
	// +k8s:listType=atomic
	Tolerations []Toleration `json:"tolerations,omitempty"`
}

// Toleration is a leaf element type with no gated fields.
type Toleration struct {
	Key string `json:"key"`
}
