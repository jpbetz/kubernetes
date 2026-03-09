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

package analysis

import (
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestAnalyzeFieldUsage(t *testing.T) {
	// Analyze the testconsumer package which accesses fields on the test input types.
	usage, err := AnalyzeFieldUsage(
		[]string{"k8s.io/code-generator/cmd/transform-gen/analysis/testconsumer"},
		"k8s.io/code-generator/cmd/transform-gen/analysis/testapi",
	)
	if err != nil {
		t.Fatalf("AnalyzeFieldUsage failed: %v", err)
	}

	// Normalize: sort field paths for comparison.
	for _, types := range usage {
		for typeName, paths := range types {
			sort.Strings(paths)
			types[typeName] = paths
		}
	}

	expected := FieldUsage{
		"v1": {
			"MyResource": []string{
				"spec.selector",
				"spec.selector.matchLabels",
			},
		},
	}

	if diff := cmp.Diff(expected, usage); diff != "" {
		t.Errorf("FieldUsage mismatch (-want +got):\n%s", diff)
	}
}
