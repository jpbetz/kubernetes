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

package operation

import (
	"testing"
)

func TestResourcePatternMatches(t *testing.T) {
	testCases := []struct {
		name     string
		pattern  ResourcePattern
		request  Request
		expected bool
	}{
		{
			name:     "empty pattern never matches",
			pattern:  ResourcePattern{},
			request:  Request{Resource: "pods"},
			expected: false,
		},
		{
			name:     "exact match for root resource",
			pattern:  ResourcePattern{"pods"},
			request:  Request{Resource: "pods"},
			expected: true,
		},
		{
			name:     "non-match for root resource",
			pattern:  ResourcePattern{"pods"},
			request:  Request{Resource: "services"},
			expected: false,
		},
		{
			name:     "wildcard match for root resource",
			pattern:  ResourcePattern{"*"},
			request:  Request{Resource: "pods"},
			expected: true,
		},
		{
			name:     "exact match for subresource",
			pattern:  ResourcePattern{"pods", "status"},
			request:  Request{Resource: "pods", Subresources: []string{"status"}},
			expected: true,
		},
		{
			name:     "non-match for subresource",
			pattern:  ResourcePattern{"pods", "status"},
			request:  Request{Resource: "pods", Subresources: []string{"logs"}},
			expected: false,
		},
		{
			name:     "wildcard match for subresource",
			pattern:  ResourcePattern{"pods", "*"},
			request:  Request{Resource: "pods", Subresources: []string{"status"}},
			expected: true,
		},
		{
			name:     "wildcard match for resource and subresource",
			pattern:  ResourcePattern{"*", "*"},
			request:  Request{Resource: "pods", Subresources: []string{"status"}},
			expected: true,
		},
		{
			name:     "pattern length mismatch",
			pattern:  ResourcePattern{"pods", "status"},
			request:  Request{Resource: "pods"},
			expected: false,
		},
		{
			name:     "pattern length mismatch with subresources",
			pattern:  ResourcePattern{"pods"},
			request:  Request{Resource: "pods", Subresources: []string{"status"}},
			expected: false,
		},
		{
			name:     "multiple subresources exact match",
			pattern:  ResourcePattern{"pods", "x", "y", "z"},
			request:  Request{Resource: "pods", Subresources: []string{"x", "y", "z"}},
			expected: true,
		},
		{
			name:     "multiple subresources with wildcards",
			pattern:  ResourcePattern{"*", "x", "*", "z"},
			request:  Request{Resource: "pods", Subresources: []string{"x", "y", "z"}},
			expected: true,
		},
		{
			name:     "multiple subresources non-match",
			pattern:  ResourcePattern{"pods", "x", "y", "a"},
			request:  Request{Resource: "pods", Subresources: []string{"x", "y", "z"}},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.pattern.Matches(tc.request)
			if result != tc.expected {
				t.Errorf("Expected pattern %v to match request %v: %v, got: %v", tc.pattern, tc.request, tc.expected, result)
			}
		})
	}
}

func TestMustParsePattern(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected ResourcePattern
	}{
		{
			name:     "root resource",
			input:    "pods",
			expected: ResourcePattern{"pods"},
		},
		{
			name:     "resource with subresource",
			input:    "pods/status",
			expected: ResourcePattern{"pods", "status"},
		},
		{
			name:     "resource with multiple subresources",
			input:    "pods/x/y/z",
			expected: ResourcePattern{"pods", "x", "y", "z"},
		},
		{
			name:     "resource with wildcard",
			input:    "*/status",
			expected: ResourcePattern{"*", "status"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: ResourcePattern{""},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := MustParsePattern(tc.input)
			if len(result) != len(tc.expected) {
				t.Fatalf("Expected pattern length %d, got %d", len(tc.expected), len(result))
			}
			for i := range result {
				if result[i] != tc.expected[i] {
					t.Errorf("Expected pattern[%d] to be %q, got %q", i, tc.expected[i], result[i])
				}
			}
		})
	}
}

func TestRequestIn(t *testing.T) {
	testCases := []struct {
		name     string
		request  Request
		patterns []ResourcePattern
		expected bool
	}{
		{
			name:     "empty patterns",
			request:  Request{Resource: "pods"},
			patterns: []ResourcePattern{},
			expected: false,
		},
		{
			name:    "single matching pattern",
			request: Request{Resource: "pods"},
			patterns: []ResourcePattern{
				{"pods"},
			},
			expected: true,
		},
		{
			name:    "multiple patterns with one match",
			request: Request{Resource: "pods"},
			patterns: []ResourcePattern{
				{"services"},
				{"pods"},
				{"configmaps"},
			},
			expected: true,
		},
		{
			name:    "multiple patterns with no match",
			request: Request{Resource: "pods"},
			patterns: []ResourcePattern{
				{"services"},
				{"configmaps"},
			},
			expected: false,
		},
		{
			name:    "subresource match",
			request: Request{Resource: "pods", Subresources: []string{"status"}},
			patterns: []ResourcePattern{
				{"pods", "status"},
			},
			expected: true,
		},
		{
			name:    "wildcard patterns",
			request: Request{Resource: "pods", Subresources: []string{"status"}},
			patterns: []ResourcePattern{
				{"services", "status"},
				{"*", "*"},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.request.In(tc.patterns)
			if result != tc.expected {
				t.Errorf("Expected request %v to be in patterns %v: %v, got: %v", tc.request, tc.patterns, tc.expected, result)
			}
		})
	}
}