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

package testing

import (
	"testing"
)

func TestFieldPathToJSONPointer(t *testing.T) {
	tests := []struct {
		name        string
		fieldPath   string
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name:      "simple path",
			fieldPath: "spec.containers.name",
			want:      "/spec/containers/name",
		},
		{
			name:      "path with array index",
			fieldPath: "spec.containers[0].name",
			want:      "/spec/containers/0/name",
		},
		{
			name:      "path with multiple array indices",
			fieldPath: "spec.containers[0].ports[1].containerPort",
			want:      "/spec/containers/0/ports/1/containerPort",
		},
		{
			name:      "metadata path",
			fieldPath: "metadata.name",
			want:      "/metadata/name",
		},
		{
			name:      "single component",
			fieldPath: "version",
			want:      "/version",
		},
		{
			name:        "empty path",
			fieldPath:   "",
			wantErr:     true,
			errContains: "field path cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FieldPathToJSONPointer(tt.fieldPath)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %v", tt.errContains, err)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("FieldPathToJSONPointer() = %v, want %v", got, tt.want)
			}
		})
	}
}
