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
	"io"
	"path/filepath"
	"reflect"
	"strings"

	"k8s.io/gengo/args"
	"k8s.io/gengo/generator"
	"k8s.io/gengo/namer"
	"k8s.io/gengo/types"
	"k8s.io/klog/v2"

	//"k8s.io/code-generator/cmd/client-gen/generators/util"
	clientgentypes "k8s.io/code-generator/cmd/client-gen/types"
)

// NameSystems returns the name system used by the generators in this package.
func NameSystems(pluralExceptions map[string]string) namer.NameSystems {
	return namer.NameSystems{
		"public":             namer.NewPublicNamer(0),
		"private":            namer.NewPrivateNamer(0),
		"raw":                namer.NewRawNamer("", nil),
		"publicPlural":       namer.NewPublicPluralNamer(pluralExceptions), // TODO(jpbetz): Remove if not needed
		"privatePlural":      namer.NewPrivatePluralNamer(pluralExceptions), // TODO(jpbetz): Remove if not needed
	}
}

// DefaultNameSystem returns the default name system for ordering the types to be
// processed by the generators in this package.
func DefaultNameSystem() string {
	return "public"
}

// TODO(jpbetz): Get rid of all these entries. We need to find a general way of handling each of these cases.
var referencableTypes = []*types.Type{
	types.Ref("k8s.io/apimachinery/pkg/runtime", "RawExtension"), // should probably be atomic
	types.Ref("k8s.io/apimachinery/pkg/runtime", "Unknown"), // should probably be atomic
	types.Ref("k8s.io/apimachinery/pkg/api/resource", "Quantity"), // implements UnstructuredConverter
	types.Ref("k8s.io/apimachinery/pkg/util/intstr", "IntOrString"), // implements UnstructuredConverter
	types.Ref("k8s.io/api/core/v1", "ResourceList"), // typeref, we can add a general rule for these
}

type TypeInfos = map[string]*types.Type

func getFieldType(t TypeInfos, field *types.Type) *types.Type {
	switch field.Kind {
	case types.Struct:
		if info, ok := t[field.Name.String()]; ok && info != nil {
			return types.Ref(info.Name.Package, info.Name.Name + "Builder")
		}
		return field
	case types.Map:
		if info, ok := t[field.Elem.Name.String()]; ok && info != nil {
			return types.Ref(info.Name.Package, info.Name.Name + "Map")
		}
		return field
	case types.Slice:
		if info, ok := t[field.Elem.Name.String()]; ok && info != nil {
			return types.Ref(info.Name.Package, info.Name.Name + "List")
		}
		return field
	case types.Pointer:
		return getFieldType(t, field.Elem)
	default:
		return field
	}
}

// Packages makes the client package definition.
func Packages(context *generator.Context, arguments *args.GeneratorArgs) generator.Packages {
	boilerplate, err := arguments.LoadGoBoilerplate()
	if err != nil {
		klog.Fatalf("Failed loading boilerplate: %v", err)
	}

	var packageList generator.Packages
	typeInfos := TypeInfos{}

	for _, t := range referencableTypes {
		typeInfos[t.Name.String()] = nil
	}

	pkgTypes := map[string]*types.Package{}
	for _, inputDir := range arguments.InputDirs {
		p := context.Universe.Package(inputDir)
		_, internal, err := objectMetaForPackage(p)
		if err != nil {
			klog.Fatal(err)
		}
		if internal {
			klog.Warningf("Skipping internal package: %s", p.Path)
			continue
		}
		gv, groupPackageName := groupAndPackageName(p)
		pkg := filepath.Join(arguments.OutputPackagePath, groupPackageName, strings.ToLower(gv.Version.NonEmpty()))
		pkgTypes[pkg] = p
	}

	// first compute all the names of the builders to generate and keep track of them
	// so all the types are known when generating references between builders
	for pkg, p := range pkgTypes {
		for _, t := range p.Types {
			if t.Kind == types.Interface {
				continue
			}
			typeInfos[t.Name.String()] = types.Ref(pkg, t.Name.Name)
		}
	}

	for pkg, p := range pkgTypes {
		var typesToGenerate []*types.Type
		for _, t := range p.Types {
			if t.Kind == types.Interface {
				continue
			}
			typesToGenerate = append(typesToGenerate, t)
		}
		if len(p.Types) == 0 {
			klog.Warningf("Skipping package with no types: %s", p.Path)
			continue
		}
		orderer := namer.Orderer{Namer: namer.NewPrivateNamer(0)}
		typesToGenerate = orderer.OrderTypes(typesToGenerate)

		gv, _ := groupAndPackageName(p)
		packageName := types.Name{
			Name: strings.ToLower(gv.Version.NonEmpty()),
			Package: pkg,
		}
		packageList = append(packageList, &generator.DefaultPackage{
			PackageName: strings.ToLower(gv.Version.NonEmpty()),
			PackagePath: packageName.Package,
			HeaderText:  boilerplate,
			GeneratorFunc: func(c *generator.Context) (generators []generator.Generator) {
				for _, t := range typesToGenerate {
					generators = append(generators, &builderTypeGenerator{
						DefaultGen: generator.DefaultGen{
							OptionalName: strings.ToLower(t.Name.Name),
						},
						outputPackage:  arguments.OutputPackagePath,
						localPackage:   packageName,
						groupVersion:   gv,
						typeToGenerate: t,
						imports:        generator.NewImportTracker(),
						typeInfos:      typeInfos,
					})
				}
				return generators
			},
		})
	}

	return packageList
}

func groupAndPackageName(p *types.Package) (clientgentypes.GroupVersion, string) {
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
	return gv, groupPackageName
}

// objectMetaForPackage returns the type of ObjectMeta used by package p.
func objectMetaForPackage(p *types.Package) (*types.Type, bool, error) {
	for _, t := range p.Types {
		for _, member := range t.Members {
			if member.Name == "ObjectMeta" {
				return member.Type, isInternal(member), nil
			}
		}
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
	localPackage   types.Name
	groupVersion   clientgentypes.GroupVersion
	typeToGenerate *types.Type
	imports        namer.ImportTracker
	typeInfos      TypeInfos
}

var _ generator.Generator = &builderTypeGenerator{}

func (g *builderTypeGenerator) Filter(c *generator.Context, t *types.Type) bool {
	return t == g.typeToGenerate
}

func (g *builderTypeGenerator) Namers(c *generator.Context) namer.NameSystems {
	return namer.NameSystems{
		"raw": namer.NewRawNamer(g.localPackage.Package, g.imports),
	}
}

func (g *builderTypeGenerator) Imports(c *generator.Context) (imports []string) {
	imports = append(imports, g.imports.ImportLines()...)
	return
}

func (g *builderTypeGenerator) GenerateType(c *generator.Context, t *types.Type, w io.Writer) error {
	sw := generator.NewSnippetWriter(w, c, "$", "$")

	klog.V(5).Infof("processing type %v", t)
	m := map[string]interface{}{
		"Resource":   c.Universe.Function(types.Name{Package: t.Name.Package, Name: "Resource"}),
		"type":       t,
		"unstructuredConverter": unstructuredConverter,
	}

	// TODO(jpbetz): Check for ListMeta instead
	if strings.HasSuffix(t.Name.Name, "List") {
		return nil
	}

	sw.Do("type $.type|public$Builder struct {\n", m)
	for _, member := range t.Members {
		m := map[string]interface{}{
			"Resource":       c.Universe.Function(types.Name{Package: t.Name.Package, Name: "Resource"}),
			"type":           t,
			"member":         member,
			"memberType":     getFieldType(g.typeInfos, member.Type),
			"memberTags":      member.Tags,
		}
		_, _,inline, _ := lookupJsonTags(member)
		if member.Embedded && inline { // TODO(jpbetz): Add error checking for when a field is either embedded or inlined, but not both
			// TODO: properly embed embedded types
			sw.Do("$.member.Name$ $.memberType|raw$ `$.memberTags$`\n", m)
		} else {
			sw.Do("$.member.Name$ *$.memberType|raw$ `$.memberTags$`\n", m)
		}
	}
	sw.Do("}\n", m)
	sw.Do(typeBuilderConstructor, m)

	for _, member := range t.Members {
		jsonName, _, inline, _ := lookupJsonTags(member)
		if jsonName == "" {
			continue
		}
		m := map[string]interface{}{
			"Resource":       c.Universe.Function(types.Name{Package: t.Name.Package, Name: "Resource"}),
			"type":           t,
			"member":         member,
			"memberType":     getFieldType(g.typeInfos, member.Type),
			"memberJsonName": jsonName,
		}
		if member.Embedded && inline {
			sw.Do(memberSetterEmbedded, m)
		} else {
			sw.Do(memberSetter, m)
		}
	}

	sw.Do(typeBuilderBuild, m)
	sw.Do(typeBuilderList, m)
	sw.Do(typeBuilderMap, m)

	return sw.Error()
}

func lookupJsonTags(m types.Member) (name string, omit bool, inline bool, omitempty bool) {
	tag := reflect.StructTag(m.Tags).Get("json")
	if tag == "-" {
		return "", true, false, false
	}
	name, opts := parseTag(tag)
	if name == "" {
		name = m.Name
	}
	return name, false, opts.Contains("inline"), opts.Contains("omitempty")
}


type tagOptions string

// parseTag splits a struct field's json tag into its name and
// comma-separated options.
func parseTag(tag string) (string, tagOptions) {
	if idx := strings.Index(tag, ","); idx != -1 {
		return tag[:idx], tagOptions(tag[idx+1:])
	}
	return tag, tagOptions("")
}

// Contains reports whether a comma-separated list of options
// contains a particular substr flag. substr must be surrounded by a
// string boundary or commas.
func (o tagOptions) Contains(optionName string) bool {
	if len(o) == 0 {
		return false
	}
	s := string(o)
	for s != "" {
		var next string
		i := strings.Index(s, ",")
		if i >= 0 {
			s, next = s[:i], s[i+1:]
		}
		if s == optionName {
			return true
		}
		s = next
	}
	return false
}


var typeBuilderConstructor = `
func $.type|public$() $.type|public$Builder {
  return $.type|public$Builder{}
}
`

var typeBuilderBuild = `
func (b $.type|public$Builder) ToUnstructured() map[string]interface{} {
  u, err := $.unstructuredConverter|raw$.ToUnstructured(&b)
  if err != nil {
    panic(err)
  }
  return u
}
`

var memberSetter = `
func (b $.type|public$Builder) Set$.member.Name$(value $.memberType|raw$) $.type|public$Builder {
	b.$.member.Name$ = &value
	return b
}
`

var memberSetterEmbedded = `
func (b $.type|public$Builder) Set$.member.Name$(value $.memberType|raw$) $.type|public$Builder {
	b.$.member.Name$ = value
	return b
}
`

var typeBuilderList = `
type $.type|public$List = []$.type|public$Builder
`

var typeBuilderMap = `
type $.type|public$Map = map[string]$.type|public$Builder
`
