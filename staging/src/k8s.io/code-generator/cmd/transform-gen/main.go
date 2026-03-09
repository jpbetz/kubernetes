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

// transform-gen generates transform functions for Kubernetes API types that zero
// out fields not needed by a component, suitable for use as informer
// cache.TransformFunc to reduce memory usage.
package main

import (
	"flag"
	"path"
	"sort"

	"github.com/spf13/pflag"
	"k8s.io/code-generator/cmd/transform-gen/analysis"
	"k8s.io/code-generator/cmd/transform-gen/args"
	"k8s.io/code-generator/cmd/transform-gen/config"
	"k8s.io/code-generator/cmd/transform-gen/generators"
	"k8s.io/gengo/v2"
	"k8s.io/gengo/v2/generator"
	"k8s.io/klog/v2"
)

func main() {
	klog.InitFlags(nil)
	a := args.New()
	a.AddFlags(pflag.CommandLine)
	if err := flag.Set("logtostderr", "true"); err != nil {
		klog.Fatalf("Error: %v", err)
	}
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	if err := a.Validate(); err != nil {
		klog.Fatalf("Error: %v", err)
	}

	if len(a.ScanPackages) > 0 {
		// Auto-discover field usage from source code.
		usage, err := analysis.AnalyzeFieldUsage(a.ScanPackages, a.InputBase)
		if err != nil {
			klog.Fatalf("Error analyzing field usage: %v", err)
		}
		a.Config = config.Config(usage)
	} else {
		// Load from config file.
		if err := a.LoadConfig(); err != nil {
			klog.Fatalf("Error loading config: %v", err)
		}
	}

	if err := a.Config.Validate(); err != nil {
		klog.Fatalf("Error validating config: %v", err)
	}

	// Build input package paths by combining --input-base with config keys.
	inputPkgs := make([]string, 0, len(a.Config))
	for key := range a.Config {
		inputPkgs = append(inputPkgs, path.Join(a.InputBase, key))
	}
	sort.Strings(inputPkgs)

	myTargets := func(context *generator.Context) []generator.Target {
		return generators.GetTargets(context, a)
	}

	if err := gengo.Execute(
		generators.NameSystems(),
		generators.DefaultNameSystem(),
		myTargets,
		gengo.StdBuildTag,
		inputPkgs,
	); err != nil {
		klog.Fatalf("Error: %v", err)
	}
	klog.V(2).Info("Completed successfully.")
}
