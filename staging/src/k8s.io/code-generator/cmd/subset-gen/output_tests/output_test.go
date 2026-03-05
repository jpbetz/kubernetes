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

package output_tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/code-generator/cmd/subset-gen/args"
	"k8s.io/code-generator/cmd/subset-gen/generators"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/parser"
)

// TestSubsetGenOutput runs the subset generator on the test input types
// and compares the output against the golden files in generated/.
//
// To update golden files, run:
//
//	UPDATE_SUBSET_GEN_GOLDEN=true go test ./cmd/subset-gen/output_tests/...
func TestSubsetGenOutput(t *testing.T) {
	configPath := filepath.Join("testdata", "config.yaml")
	goldenDir := filepath.Join("generated")

	// Create a temp directory for generator output.
	tmpDir := t.TempDir()

	// Set up the generator args.
	a := &args.Args{
		ConfigFilePath: configPath,
		OutputDir:      tmpDir,
		OutputPkg:      "k8s.io/code-generator/cmd/subset-gen/output_tests/generated",
	}

	// Parse input packages.
	p := parser.New()
	if err := p.LoadPackages("k8s.io/code-generator/cmd/subset-gen/output_tests/input/v1"); err != nil {
		t.Fatalf("loading packages: %v", err)
	}

	c, err := generator.NewContext(p, generators.NameSystems(), generators.DefaultNameSystem())
	if err != nil {
		t.Fatalf("creating context: %v", err)
	}

	targets := generators.GetTargets(c, a)
	if err := c.ExecuteTargets(targets); err != nil {
		t.Fatalf("executing generator: %v", err)
	}

	// Compare generated files against golden files.
	update := os.Getenv("UPDATE_SUBSET_GEN_GOLDEN") == "true"

	err = filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		relPath, err := filepath.Rel(tmpDir, path)
		if err != nil {
			return err
		}

		actualBytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		actual := string(actualBytes)

		goldenPath := filepath.Join(goldenDir, relPath)

		if update {
			if err := os.MkdirAll(filepath.Dir(goldenPath), 0755); err != nil {
				return err
			}
			if err := os.WriteFile(goldenPath, actualBytes, 0644); err != nil {
				return err
			}
			t.Logf("Updated golden file: %s", goldenPath)
			return nil
		}

		goldenBytes, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Errorf("golden file %s not found (run with UPDATE_SUBSET_GEN_GOLDEN=true to create): %v", goldenPath, err)
			return nil
		}
		golden := string(goldenBytes)

		if actual != golden {
			t.Errorf("output mismatch for %s (run with UPDATE_SUBSET_GEN_GOLDEN=true to update)\n"+
				"=== ACTUAL ===\n%s\n=== GOLDEN ===\n%s", relPath, actual, golden)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking output dir: %v", err)
	}

	// Also check that no golden files exist that weren't generated.
	if !update {
		err = filepath.Walk(goldenDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			relPath, err := filepath.Rel(goldenDir, path)
			if err != nil {
				return err
			}
			generatedPath := filepath.Join(tmpDir, relPath)
			if _, err := os.Stat(generatedPath); os.IsNotExist(err) {
				t.Errorf("golden file %s exists but was not generated", relPath)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking golden dir: %v", err)
		}
	}
}
