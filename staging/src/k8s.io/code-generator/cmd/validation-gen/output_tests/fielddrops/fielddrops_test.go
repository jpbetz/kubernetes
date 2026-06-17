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

import (
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/api/operation"
	"k8s.io/utils/ptr"
)

// op builds a create operation whose enabled options are the given gate names.
// The generated drop keeps a gate's fields when its option is present and clears
// them otherwise; the apiserver populates these options as gate-on-or-in-use.
func op(options ...string) operation.Operation {
	return operation.Operation{Type: operation.Create, Options: options}
}

// fullRoot returns a Root with every droppable field set, including MultiLeafGate
// tolerations reached through both a slice-of-struct (Requests[*].Tolerations)
// and a pointer-to-struct (Requests[*].Exactly.Tolerations).
func fullRoot() *Root {
	return &Root{
		Spec: RootSpec{
			Priority: ptr.To(true),
			Mode:     "fast",
			Requests: []Request{
				{
					Name:        "r0",
					Tolerations: []Toleration{{Key: "a"}},
					Exactly:     &Exactly{Tolerations: []Toleration{{Key: "b"}}},
				},
			},
		},
		Status: RootStatus{Allocation: &Allocation{Device: "d"}},
	}
}

func multiLeafSet(r *Root) bool {
	for _, req := range r.Spec.Requests {
		if len(req.Tolerations) > 0 {
			return true
		}
		if req.Exactly != nil && len(req.Exactly.Tolerations) > 0 {
			return true
		}
	}
	return false
}

// TestDropMultiLeaf covers the per-type delegating drop for a gate reached
// through two sub-types: the tolerations are kept when the option is present and
// cleared when it is absent.
func TestDropMultiLeaf(t *testing.T) {
	cases := []struct {
		name    string
		options []string
		wantSet bool
	}{
		{name: "option present keeps", options: []string{"MultiLeafGate"}, wantSet: true},
		{name: "option absent drops", options: nil, wantSet: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj := fullRoot()
			DropDisabledFields_Root(op(tc.options...), obj)
			if got := multiLeafSet(obj); got != tc.wantSet {
				t.Fatalf("tolerations present=%v, want %v", got, tc.wantSet)
			}
		})
	}
}

// TestInUseMultiLeaf confirms the single in-use detector reports MultiLeafGate
// across both leaves: tolerations only in Exactly still count as in use.
func TestInUseMultiLeaf(t *testing.T) {
	old := &Root{Spec: RootSpec{Requests: []Request{{Exactly: &Exactly{Tolerations: []Toleration{{Key: "x"}}}}}}}
	if inUse := inUse_Root(old, nil); !slices.Contains(inUse, "MultiLeafGate") {
		t.Fatalf("expected MultiLeafGate in use via the Exactly leaf, got %v", inUse)
	}
	if inUse := inUse_Root(&Root{}, nil); len(inUse) != 0 {
		t.Fatalf("expected no gates in use for an empty Root, got %v", inUse)
	}
}

// TestDropValueLeaf exercises a value-typed (string) leaf, cleared to "".
func TestDropValueLeaf(t *testing.T) {
	obj := fullRoot()
	DropDisabledFields_Root(op(), obj)
	if obj.Spec.Mode != "" {
		t.Fatalf("Mode=%q, want cleared", obj.Spec.Mode)
	}
	kept := fullRoot()
	DropDisabledFields_Root(op("ValueLeafGate"), kept)
	if kept.Spec.Mode != "fast" {
		t.Fatalf("Mode=%q, want kept", kept.Spec.Mode)
	}
}

// TestDropPointerLeaf exercises a direct pointer leaf.
func TestDropPointerLeaf(t *testing.T) {
	obj := fullRoot()
	DropDisabledFields_Root(op(), obj)
	if obj.Spec.Priority != nil {
		t.Fatal("expected Priority dropped when its option is absent")
	}
	kept := fullRoot()
	DropDisabledFields_Root(op("PointerLeafGate"), kept)
	if kept.Spec.Priority == nil {
		t.Fatal("expected Priority kept when its option is present")
	}
}

// TestDropStatusScope confirms status-scope partitioning: the status leaf is
// dropped by DropDisabledStatusFields_Root, and the spec entry never touches it.
func TestDropStatusScope(t *testing.T) {
	dropped := fullRoot()
	DropDisabledStatusFields_Root(op(), dropped)
	if dropped.Status.Allocation != nil {
		t.Fatal("status entry should drop Allocation when its option is absent")
	}

	kept := fullRoot()
	DropDisabledStatusFields_Root(op("StatusGate"), kept)
	if kept.Status.Allocation == nil {
		t.Fatal("status entry should keep Allocation when its option is present")
	}

	specOnly := fullRoot()
	DropDisabledFields_Root(op(), specOnly)
	if specOnly.Status.Allocation == nil {
		t.Fatal("spec entry must not touch status fields")
	}
}
