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
	"fmt"
	"strings"
)

// FieldTree represents a tree of fields to include from a struct type.
// It is built by merging multiple field paths together.
type FieldTree struct {
	// IncludeAll means the path terminated at this node, so include the entire
	// type with all its fields. When true, Fields is ignored.
	IncludeAll bool

	// Fields maps Go field name -> subtree. For list traversals (containers[]),
	// the key is the field name without the "[]" suffix (e.g. "Containers"),
	// and the subtree represents the element type's fields.
	Fields map[string]*FieldTree
}

// fieldPathSegment represents a single segment of a parsed field path.
type fieldPathSegment struct {
	// FieldName is the name of the field (json name, will be resolved to Go name later).
	FieldName string
	// IsList indicates the segment uses [] syntax to traverse into list elements.
	IsList bool
}

// ParseFieldPaths parses a list of field path strings and merges them into a FieldTree.
// Field paths use dot-separated segments with optional [] for list traversal.
// The field names in paths are json names (lowercase), not Go field names.
func ParseFieldPaths(paths []string) (*FieldTree, error) {
	root := &FieldTree{
		Fields: map[string]*FieldTree{},
	}

	for _, p := range paths {
		segments, err := parseFieldPath(p)
		if err != nil {
			return nil, fmt.Errorf("parsing field path %q: %w", p, err)
		}
		if len(segments) == 0 {
			return nil, fmt.Errorf("empty field path")
		}
		root.merge(segments)
	}

	return root, nil
}

// parseFieldPath splits a field path string into segments.
// Examples:
//
//	"metadata.name" -> [{metadata, false}, {name, false}]
//	"spec.containers[].name" -> [{spec, false}, {containers, true}, {name, false}]
func parseFieldPath(path string) ([]fieldPathSegment, error) {
	if path == "" {
		return nil, fmt.Errorf("empty field path")
	}

	parts := strings.Split(path, ".")
	segments := make([]fieldPathSegment, 0, len(parts))

	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("empty segment in path %q", path)
		}

		seg := fieldPathSegment{}
		if strings.HasSuffix(part, "[]") {
			seg.FieldName = strings.TrimSuffix(part, "[]")
			seg.IsList = true
		} else {
			seg.FieldName = part
		}

		if seg.FieldName == "" {
			return nil, fmt.Errorf("empty field name in segment %q of path %q", part, path)
		}

		segments = append(segments, seg)
	}

	return segments, nil
}

// merge adds the given path segments into the tree.
func (t *FieldTree) merge(segments []fieldPathSegment) {
	if t.IncludeAll {
		// Already including everything, nothing to add.
		return
	}

	if len(segments) == 0 {
		// Path terminated here: include everything at this node.
		t.IncludeAll = true
		t.Fields = nil
		return
	}

	seg := segments[0]
	rest := segments[1:]

	if t.Fields == nil {
		t.Fields = map[string]*FieldTree{}
	}

	child, ok := t.Fields[seg.FieldName]
	if !ok {
		child = &FieldTree{
			Fields: map[string]*FieldTree{},
		}
		t.Fields[seg.FieldName] = child
	}

	// For list traversals, the child represents the element type.
	// The [] marker is used during path resolution to know we need to
	// dereference slice elements, but doesn't affect the tree structure.
	child.merge(rest)
}

// IsEmpty returns true if the tree has no fields selected.
func (t *FieldTree) IsEmpty() bool {
	return !t.IncludeAll && len(t.Fields) == 0
}

// HasField returns the subtree for the given field name, or nil if not selected.
func (t *FieldTree) HasField(name string) *FieldTree {
	if t.IncludeAll {
		return &FieldTree{IncludeAll: true}
	}
	if t.Fields == nil {
		return nil
	}
	return t.Fields[name]
}
