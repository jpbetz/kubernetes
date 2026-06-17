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

package main

import (
	"strings"

	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/namer"
	"k8s.io/gengo/v2/types"
)

// genBase holds the fields and helpers shared by the validation-gen generators
// (genValidations and genFeatureGate), which both walk the shared type discovery
// and emit a file per package.
type genBase struct {
	generator.GoGenerator
	outputPackage  string
	rootTypes      []*types.Type
	discovered     *typeDiscoverer
	imports        namer.ImportTracker
	schemeRegistry types.Name
}

func newGenBase(outputFilename, outputPackage string, rootTypes []*types.Type, discovered *typeDiscoverer, schemeRegistry types.Name) genBase {
	return genBase{
		GoGenerator:    generator.GoGenerator{OutputFilename: outputFilename},
		outputPackage:  outputPackage,
		rootTypes:      rootTypes,
		discovered:     discovered,
		imports:        generator.NewImportTrackerForPackage(outputPackage),
		schemeRegistry: schemeRegistry,
	}
}

func (g *genBase) Namers(_ *generator.Context) namer.NameSystems {
	return namer.NameSystems{
		"raw": namer.NewRawNamer(g.outputPackage, g.imports),
	}
}

func (g *genBase) Imports(_ *generator.Context) (imports []string) {
	for _, line := range g.imports.ImportLines() {
		if line != g.outputPackage && !strings.HasSuffix(line, `"`+g.outputPackage+`"`) {
			imports = append(imports, line)
		}
	}
	return imports
}
