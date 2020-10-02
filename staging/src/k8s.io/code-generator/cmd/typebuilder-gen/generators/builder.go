/*
Copyright 2015 The Kubernetes Authors.

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

package generators

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"k8s.io/gengo/args"
	"k8s.io/gengo/generator"
	"k8s.io/gengo/namer"
	"k8s.io/gengo/types"
	"k8s.io/klog/v2"

	"k8s.io/code-generator/cmd/client-gen/generators/util"
	clientgentypes "k8s.io/code-generator/cmd/client-gen/types"
)

// NameSystems returns the name system used by the generators in this package.
func NameSystems(pluralExceptions map[string]string) namer.NameSystems {
	return namer.NameSystems{
		"public":             namer.NewPublicNamer(0),
		"private":            namer.NewPrivateNamer(0),
		"raw":                namer.NewRawNamer("", nil),
		"publicPlural":       namer.NewPublicPluralNamer(pluralExceptions),
		"allLowercasePlural": namer.NewAllLowercasePluralNamer(pluralExceptions),
		"lowercaseSingular":  &lowercaseSingularNamer{},
	}
}

// lowercaseSingularNamer implements Namer
type lowercaseSingularNamer struct{}

// Name returns t's name in all lowercase.
func (n *lowercaseSingularNamer) Name(t *types.Type) string {
	return strings.ToLower(t.Name.Name)
}

// DefaultNameSystem returns the default name system for ordering the types to be
// processed by the generators in this package.
func DefaultNameSystem() string {
	return "public"
}

// Packages makes the client package definition.
func Packages(context *generator.Context, arguments *args.GeneratorArgs) generator.Packages {
	boilerplate, err := arguments.LoadGoBoilerplate()
	if err != nil {
		klog.Fatalf("Failed loading boilerplate: %v", err)
	}

	var packageList generator.Packages
	for _, inputDir := range arguments.InputDirs {
		p := context.Universe.Package(inputDir)

		objectMeta, internal, err := objectMetaForPackage(p)
		if err != nil {
			klog.Fatal(err)
		}
		if objectMeta == nil {
			// no types in this package had genclient
			continue
		}

		var gv clientgentypes.GroupVersion
		var internalGVPkg string

		if internal {
			lastSlash := strings.LastIndex(p.Path, "/")
			if lastSlash == -1 {
				klog.Fatalf("error constructing internal group version for package %q", p.Path)
			}
			gv.Group = clientgentypes.Group(p.Path[lastSlash+1:])
			internalGVPkg = p.Path
		} else {
			parts := strings.Split(p.Path, "/")
			gv.Group = clientgentypes.Group(parts[len(parts)-2])
			gv.Version = clientgentypes.Version(parts[len(parts)-1])

			internalGVPkg = strings.Join(parts[0:len(parts)-1], "/")
		}
		groupPackageName := strings.ToLower(gv.Group.NonEmpty())

		// If there's a comment of the form "// +groupName=somegroup" or
		// "// +groupName=somegroup.foo.bar.io", use the first field (somegroup) as the name of the
		// group when generating.
		if override := types.ExtractCommentTags("+", p.Comments)["groupName"]; override != nil {
			gv.Group = clientgentypes.Group(strings.SplitN(override[0], ".", 2)[0])
		}

		// TODO(jpbetz): This needs to be the entire list of types defined in group, not just top level ones
		var typesToGenerate []*types.Type
		for _, t := range p.Types {
			tags := util.MustParseClientGenTags(append(t.SecondClosestCommentLines, t.CommentLines...))
			if !tags.GenerateClient { // TODO(jpbetz): filter to just APIs that have Update or Apply?
				continue
			}
			typesToGenerate = append(typesToGenerate, t)
		}
		if len(typesToGenerate) == 0 {
			continue
		}
		orderer := namer.Orderer{Namer: namer.NewPrivateNamer(0)}
		typesToGenerate = orderer.OrderTypes(typesToGenerate)

		packagePath := filepath.Join(arguments.OutputPackagePath, groupPackageName, strings.ToLower(gv.Version.NonEmpty()))
		packageList = append(packageList, &generator.DefaultPackage{
			PackageName: strings.ToLower(gv.Version.NonEmpty()),
			PackagePath: packagePath,
			HeaderText:  boilerplate,
			GeneratorFunc: func(c *generator.Context) (generators []generator.Generator) {
				for _, t := range typesToGenerate {
					generators = append(generators, &builderTypeGenerator{
						DefaultGen: generator.DefaultGen{
							OptionalName: strings.ToLower(t.Name.Name),
						},
						outputPackage:  arguments.OutputPackagePath,
						groupVersion:   gv,
						internalGVPkg:  internalGVPkg,
						typeToGenerate: t,
						imports:        generator.NewImportTracker(),
						objectMeta:     objectMeta,
					})
				}
				return generators
			},
			FilterFunc: func(c *generator.Context, t *types.Type) bool {
				tags := util.MustParseClientGenTags(append(t.SecondClosestCommentLines, t.CommentLines...))
				return tags.GenerateClient // TODO(jpbetz): filter to just APIs that have Update or Apply?
			},
		})
	}

	return packageList
}

// objectMetaForPackage returns the type of ObjectMeta used by package p.
func objectMetaForPackage(p *types.Package) (*types.Type, bool, error) {
	generatingForPackage := false
	for _, t := range p.Types {
		// filter out types which dont have genclient.
		if !util.MustParseClientGenTags(append(t.SecondClosestCommentLines, t.CommentLines...)).GenerateClient {
			continue
		}
		generatingForPackage = true
		for _, member := range t.Members {
			if member.Name == "ObjectMeta" {
				return member.Type, isInternal(member), nil
			}
		}
	}
	if generatingForPackage {
		return nil, false, fmt.Errorf("unable to find ObjectMeta for any types in package %s", p.Path)
	}
	return nil, false, nil
}

// isInternal returns true if the tags for a member do not contain a json tag
func isInternal(m types.Member) bool {
	return !strings.Contains(m.Tags, "json")
}

// builderTypeGenerator produces a file of builders for a given GroupVersion and
// type.
type builderTypeGenerator struct {
	generator.DefaultGen
	outputPackage  string
	groupVersion   clientgentypes.GroupVersion
	internalGVPkg  string
	typeToGenerate *types.Type
	imports        namer.ImportTracker
	objectMeta     *types.Type
}

var _ generator.Generator = &builderTypeGenerator{}

func (g *builderTypeGenerator) Filter(c *generator.Context, t *types.Type) bool {
	return t == g.typeToGenerate
}

func (g *builderTypeGenerator) Namers(c *generator.Context) namer.NameSystems {
	return namer.NameSystems{
		"raw": namer.NewRawNamer(g.outputPackage, g.imports),
	}
}

func (g *builderTypeGenerator) Imports(c *generator.Context) (imports []string) {
	imports = append(imports, g.imports.ImportLines()...)
	//imports = append(imports, "k8s.io/apimachinery/pkg/api/errors")
	//imports = append(imports, "k8s.io/apimachinery/pkg/labels")
	return
}

func (g *builderTypeGenerator) GenerateType(c *generator.Context, t *types.Type, w io.Writer) error {
	sw := generator.NewSnippetWriter(w, c, "$", "$")

	klog.V(5).Infof("processing type %v", t)
	m := map[string]interface{}{
		"Resource":   c.Universe.Function(types.Name{Package: t.Name.Package, Name: "Resource"}),
		"type":       t,
		"objectMeta": g.objectMeta,
	}

	tags, err := util.ParseClientGenTags(append(t.SecondClosestCommentLines, t.CommentLines...))
	if err != nil {
		return err
	}

	if tags.NonNamespaced {
		// TODO(jpbetz): Handle non-namespaced types
		//sw.Do(typeListerInterface_NonNamespaced, m)
	} else {
		sw.Do(typeBuilderStruct, m)
	}

	for _, member := range t.Members {
		m := map[string]interface{}{
			"Resource":   c.Universe.Function(types.Name{Package: t.Name.Package, Name: "Resource"}),
			"type":       t,
			"member":     member,
			"objectMeta": g.objectMeta,
		}

		if tags.NonNamespaced {
			// TODO(jpbetz): Handle non-namespaced types
			//sw.Do(typeListerInterface_NonNamespaced, m)
		} else {
			sw.Do(memberBuilderFunc_Get, m)
			sw.Do(memberBuilderFunc_Set, m)
		}
	}

	return sw.Error()
}

// TODO: generate an interface

var typeBuilderStruct = `
type $.type|private$Builder struct {}
`

var memberBuilderFunc_Get = `
func (b $.type|private$Builder) Get() {
}
`

var memberBuilderFunc_Set = `
func (b $.type|private$Builder) Set() {
}
`
