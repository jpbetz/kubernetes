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

package options

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestParseWatchCacheShardSelectors(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		wantErr bool
		check   func(t *testing.T, out map[schema.GroupResource]interface{ String() string })
	}{
		{
			name: "core resource",
			input: []string{
				"pods#shardRange(object.metadata.uid, '0x0000000000000000', '0x8000000000000000')",
			},
		},
		{
			name: "grouped resource",
			input: []string{
				"events.events.k8s.io#shardRange(object.metadata.namespace, '0x0000000000000000', '0x4000000000000000')",
			},
		},
		{
			name: "OR-composed",
			input: []string{
				"pods#shardRange(object.metadata.uid, '0x0000000000000000', '0x4000000000000000') || shardRange(object.metadata.uid, '0xc000000000000000', '0x10000000000000000')",
			},
		},
		{
			name:    "missing hash separator",
			input:   []string{"pods-no-hash"},
			wantErr: true,
		},
		{
			name:    "malformed expression",
			input:   []string{"pods#not-a-shardRange-call"},
			wantErr: true,
		},
		{
			name: "duplicate resource",
			input: []string{
				"pods#shardRange(object.metadata.uid, '0x0000000000000000', '0x8000000000000000')",
				"pods#shardRange(object.metadata.uid, '0x8000000000000000', '0x10000000000000000')",
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ParseWatchCacheShardSelectors(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (out=%v)", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(out) != 1 {
				t.Errorf("expected 1 entry, got %d", len(out))
			}
		})
	}
}
