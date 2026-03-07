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

package args

import (
	"fmt"

	"github.com/spf13/pflag"
	"k8s.io/code-generator/cmd/transform-gen/config"
)

// Args contains the arguments for transform-gen.
type Args struct {
	// OutputDir is the base directory under which to generate results.
	OutputDir string

	// OutputPkg is the Go import path of the generated results.
	OutputPkg string

	// ConfigFilePath is the path to the YAML config file declaring field views.
	ConfigFilePath string

	// GoHeaderFile is the path to a file containing boilerplate header text.
	GoHeaderFile string

	// InputBase is the Go import path prefix prepended to config keys
	// to form full package paths. For example, with InputBase "k8s.io/api"
	// and config key "apps/v1", the input package is "k8s.io/api/apps/v1".
	InputBase string

	// Config is the loaded configuration, populated after calling LoadConfig.
	Config config.Config
}

// New returns default arguments for the generator.
func New() *Args {
	return &Args{}
}

// AddFlags adds command-line flags to the given FlagSet.
func (a *Args) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&a.OutputDir, "output-dir", "",
		"the base directory under which to generate results")
	fs.StringVar(&a.OutputPkg, "output-pkg", "",
		"the Go import-path of the generated results")
	fs.StringVar(&a.ConfigFilePath, "config", "",
		"path to the YAML config file declaring field views per type")
	fs.StringVar(&a.GoHeaderFile, "go-header-file", "",
		"the path to a file containing boilerplate header text; the string \"YEAR\" will be replaced with the current 4-digit year")
	fs.StringVar(&a.InputBase, "input-base", "",
		"the Go import-path prefix for input packages; config keys are appended to this (e.g. \"k8s.io/api\")")
}

// Validate checks the given arguments.
func (a *Args) Validate() error {
	if len(a.OutputDir) == 0 {
		return fmt.Errorf("--output-dir must be specified")
	}
	if len(a.OutputPkg) == 0 {
		return fmt.Errorf("--output-pkg must be specified")
	}
	if len(a.ConfigFilePath) == 0 {
		return fmt.Errorf("--config must be specified")
	}
	if len(a.InputBase) == 0 {
		return fmt.Errorf("--input-base must be specified")
	}
	return nil
}

// LoadConfig reads and validates the config file, storing the result in Config.
func (a *Args) LoadConfig() error {
	cfg, err := config.LoadConfig(a.ConfigFilePath)
	if err != nil {
		return err
	}
	a.Config = cfg
	return nil
}
