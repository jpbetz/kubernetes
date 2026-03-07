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

package config

import (
	"testing"
)

func TestParseFieldPaths(t *testing.T) {
	tests := []struct {
		name    string
		paths   []string
		wantErr bool
		check   func(t *testing.T, tree *FieldTree)
	}{
		{
			name:  "single scalar field",
			paths: []string{"metadata.name"},
			check: func(t *testing.T, tree *FieldTree) {
				metadata := tree.HasField("metadata")
				if metadata == nil {
					t.Fatal("expected metadata field")
				}
				name := metadata.HasField("name")
				if name == nil {
					t.Fatal("expected metadata.name field")
				}
				if !name.IncludeAll {
					t.Error("expected name to be IncludeAll")
				}
			},
		},
		{
			name:  "include entire struct",
			paths: []string{"metadata"},
			check: func(t *testing.T, tree *FieldTree) {
				metadata := tree.HasField("metadata")
				if metadata == nil {
					t.Fatal("expected metadata field")
				}
				if !metadata.IncludeAll {
					t.Error("expected metadata to be IncludeAll")
				}
			},
		},
		{
			name:  "list traversal",
			paths: []string{"spec.containers[].name"},
			check: func(t *testing.T, tree *FieldTree) {
				spec := tree.HasField("spec")
				if spec == nil {
					t.Fatal("expected spec field")
				}
				containers := spec.HasField("containers")
				if containers == nil {
					t.Fatal("expected spec.containers field")
				}
				name := containers.HasField("name")
				if name == nil {
					t.Fatal("expected spec.containers[].name field")
				}
				if !name.IncludeAll {
					t.Error("expected name to be IncludeAll")
				}
			},
		},
		{
			name:  "merging paths",
			paths: []string{"spec.replicas", "spec.selector", "metadata.name"},
			check: func(t *testing.T, tree *FieldTree) {
				spec := tree.HasField("spec")
				if spec == nil {
					t.Fatal("expected spec field")
				}
				if spec.HasField("replicas") == nil {
					t.Error("expected spec.replicas")
				}
				if spec.HasField("selector") == nil {
					t.Error("expected spec.selector")
				}
				metadata := tree.HasField("metadata")
				if metadata == nil {
					t.Fatal("expected metadata field")
				}
				if metadata.HasField("name") == nil {
					t.Error("expected metadata.name")
				}
			},
		},
		{
			name:  "includeAll subsumes children",
			paths: []string{"spec.template.spec.containers[].name", "spec.template"},
			check: func(t *testing.T, tree *FieldTree) {
				spec := tree.HasField("spec")
				if spec == nil {
					t.Fatal("expected spec field")
				}
				template := spec.HasField("template")
				if template == nil {
					t.Fatal("expected spec.template field")
				}
				// "spec.template" path should make it IncludeAll, subsuming the containers path.
				if !template.IncludeAll {
					t.Error("expected template to be IncludeAll after merging")
				}
			},
		},
		{
			name:    "empty path",
			paths:   []string{""},
			wantErr: true,
		},
		{
			name:    "double dot",
			paths:   []string{"spec..name"},
			wantErr: true,
		},
		{
			name:    "trailing dot",
			paths:   []string{"spec."},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := ParseFieldPaths(tt.paths)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, tree)
			}
		})
	}
}

func TestFieldTreeIsEmpty(t *testing.T) {
	empty := &FieldTree{}
	if !empty.IsEmpty() {
		t.Error("expected empty tree")
	}

	nonEmpty := &FieldTree{IncludeAll: true}
	if nonEmpty.IsEmpty() {
		t.Error("expected non-empty tree")
	}
}
