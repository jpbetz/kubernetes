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
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	configYAML := `
Deployment:
  - metadata.name
  - metadata.namespace
  - spec.replicas
  - spec.template.spec.containers[].name
  - spec.template.spec.containers[].image
Pod:
  - metadata.name
  - metadata.namespace
  - spec.nodeName
  - status.phase
`

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if len(cfg) != 2 {
		t.Fatalf("expected 2 types, got %d", len(cfg))
	}

	deploymentFields := cfg["Deployment"]
	if len(deploymentFields) != 5 {
		t.Errorf("expected 5 fields for Deployment, got %d", len(deploymentFields))
	}

	podFields := cfg["Pod"]
	if len(podFields) != 4 {
		t.Errorf("expected 4 fields for Pod, got %d", len(podFields))
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "empty config",
			config:  Config{},
			wantErr: true,
		},
		{
			name:    "missing fields",
			config:  Config{"Deployment": {}},
			wantErr: true,
		},
		{
			name:    "valid",
			config:  Config{"Deployment": {"spec.replicas"}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
