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
	"reflect"
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
		"privatePlural":      namer.NewPrivatePluralNamer(pluralExceptions),
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
		if internal {
			continue
		}

		var gv clientgentypes.GroupVersion
		parts := strings.Split(p.Path, "/")
		gv.Group = clientgentypes.Group(parts[len(parts)-2])
		gv.Version = clientgentypes.Version(parts[len(parts)-1])
		groupPackageName := strings.ToLower(gv.Group.NonEmpty())

		// If there's a comment of the form "// +groupName=somegroup" or
		// "// +groupName=somegroup.foo.bar.io", use the first field (somegroup) as the name of the
		// group when generating.
		if override := types.ExtractCommentTags("+", p.Comments)["groupName"]; override != nil {
			gv.Group = clientgentypes.Group(strings.SplitN(override[0], ".", 2)[0])
		}

		var typesToGenerate []*types.Type
		for _, t := range p.Types {
			typesToGenerate = append(typesToGenerate, t)
		}
		if len(p.Types) == 0 {
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
						typeToGenerate: t,
						imports:        generator.NewImportTracker(),
						objectMeta:     objectMeta,
					})
				}
				return generators
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

	//tags, err := util.ParseClientGenTags(append(t.SecondClosestCommentLines, t.CommentLines...))
	//if err != nil {
	//	return err
	//}

	// TODO(jpbetz): Handle list types explicitly?
	if strings.HasSuffix(t.Name.Name, "List") {
		return nil
	}

	sw.Do(typeBuilderStruct, m)
	sw.Do(typeBuilderConstructor, m)

	for _, member := range t.Members {
		jsonName, ok := jsonName(member.Tags)
		if !ok {
			continue
		}
		m := map[string]interface{}{
			"Resource":   c.Universe.Function(types.Name{Package: t.Name.Package, Name: "Resource"}),
			"type":       t,
			"member":     member, // TODO(jpbetz): Need to get the member json name out of the tags
			"objectMeta": g.objectMeta,
			"memberJsonName":   jsonName,
		}

		if member.Type.IsPrimitive() || (member.Type.Elem != nil && member.Type.Elem.IsPrimitive()) {
			// TODO(jpbetz): This does not handle converting maps and lists to unstructured correctly yet
			// e.g. it should construct map[string]interface{} instead of map[string]string
			sw.Do(memberBuilderFunc_Set_primitive, m)
		} else if member.Type.Kind == types.Map {
			sw.Do(memberBuilderFunc_Set_map, m)
		} else if member.Type.Kind == types.Slice {
			sw.Do(memberBuilderFunc_Set_slice, m)
		} else {
			sw.Do(memberBuilderFunc_Set, m)
		}
	}

	sw.Do(typeBuilderList, m)
	sw.Do(typeBuilderMap, m)

	return sw.Error()
}

func jsonName(tags string) (string, bool) {
	jsonTag := reflect.StructTag(tags).Get("json")
	index := strings.Index(jsonTag, ",")
	if index == -1 {
		index = len(jsonTag)
	}
	if index == 0 {
		return "", false
	}
	return jsonTag[:index], true
}

// TODO: generate an interface

var typeBuilderStruct = `
type $.type|public$Builder struct {
  unstructured map[string]interface{}
}
`

var typeBuilderConstructor = `
func $.type|public$() $.type|public$Builder {
  return $.type|public$Builder{unstructured: map[string]interface{}{}}
}
`

//var memberBuilderFunc_Get = `
//func (b $.type|public$Builder) Get$.member.Name$() $.member.Type|public$Builder {
//	return b.obj.$.member.Name$
//}
//`

var memberBuilderFunc_Set = `
func (b $.type|public$Builder) Set$.member.Name$(value $.member.Type|public$Builder) $.type|public$Builder {
	b.unstructured["$.memberJsonName$"] = value.unstructured
	return b
}
`

//var memberBuilderFunc_Get_primitive = `
//
//func (b $.type|public$Builder) Get$.member.Name$() $.member.Type|raw$ {
//	return  b.unstructured["$.memberJsonName$"].($.member.Type|raw$)
//}
//`

var memberBuilderFunc_Set_primitive = `
func (b $.type|public$Builder) Set$.member.Name$(value $.member.Type|raw$) $.type|raw$ {
	b.unstructured["$.memberJsonName$"] = value
	return b
}
`

var memberBuilderFunc_Set_map = `
func (b $.type|public$Builder) Set$.member.Name$(values $.member.Type.Elem|public$Map) $.type|raw$ {
	u := make(map[string]interface{}, len(values))
	for key, value := range values {
		u[key] = value
	}
	b.unstructured["$.memberJsonName$"] = u
	return b
}
`

var memberBuilderFunc_Set_slice = `
func (b $.type|public$Builder) Set$.member.Name$(values $.member.Type.Elem|public$List) $.type|raw$ {
	u := make([]interface{}, len(values))
	for i, value := range values {
		u[i] = value
	}
	b.unstructured["$.memberJsonName$"] = u
	return b
}
`

// TODO(jpbetz): Names collide with Kubernetes List types (i.e. types that have ListMeta)
var typeBuilderList = `
type $.type|public$List = []$.type|public$Builder
`

var typeBuilderMap = `
type $.type|public$Map = map[string]$.type|public$Builder
`
