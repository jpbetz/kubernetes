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

// Config maps API group/version identifiers to type definitions.
// Each key is a group/version suffix (e.g. "apps/v1", "core/v1") that
// gets combined with the --input-base flag to form the full Go import path.
// Each type maps to a list of dot-separated field paths to retain.
// Example:
//
//	apps/v1:
//	  ReplicaSet:
//	    - spec.selector
//	core/v1:
//	  Service:
//	    - spec.selector
type Config map[string]map[string][]string

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
		return fmt.Errorf("at least one package must be defined")
	}
	for pkg, types := range c {
		if pkg == "" {
			return fmt.Errorf("empty package name")
		}
		if len(types) == 0 {
			return fmt.Errorf("package %q: at least one type must be specified", pkg)
		}
		for typeName, fields := range types {
			if typeName == "" {
				return fmt.Errorf("package %q: empty type name", pkg)
			}
			if len(fields) == 0 {
				return fmt.Errorf("package %q, type %q: at least one field must be specified", pkg, typeName)
			}
		}
	}
	return nil
}
