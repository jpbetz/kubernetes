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
	"os"

	"go.yaml.in/yaml/v2"
)

// Config maps type names to lists of field paths to include.
// Example:
//
//	Deployment:
//	  - metadata.name
//	  - spec.replicas
//	Pod:
//	  - metadata.name
//	  - status.phase
type Config map[string][]string

// LoadConfig reads and parses a YAML config file.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}

	cfg := Config{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config file %s: %w", path, err)
	}

	return cfg, nil
}

// Validate checks the config for required fields.
func (c Config) Validate() error {
	if len(c) == 0 {
		return fmt.Errorf("at least one type must be defined")
	}
	for typeName, fields := range c {
		if typeName == "" {
			return fmt.Errorf("empty type name")
		}
		if len(fields) == 0 {
			return fmt.Errorf("type %q: at least one field must be specified", typeName)
		}
	}
	return nil
}
