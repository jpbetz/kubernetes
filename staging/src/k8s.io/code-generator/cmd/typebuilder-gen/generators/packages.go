/*
Copyright 2020 The Kubernetes Authors.

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
	"path"
	"path/filepath"
	"sort"
	"strings"

	"k8s.io/gengo/args"
	"k8s.io/gengo/generator"
	"k8s.io/gengo/namer"
	"k8s.io/gengo/types"
	"k8s.io/klog/v2"

	clientgentypes "k8s.io/code-generator/cmd/client-gen/types"
)

// NameSystems returns the name system used by the generators in this package.
func NameSystems() namer.NameSystems {
	return namer.NameSystems{
		"public":  namer.NewPublicNamer(0),
		"private": namer.NewPrivateNamer(0),
		"raw":     namer.NewRawNamer("", nil),
	}
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

	pkgTypes := packageTypesForInputDirs(context, arguments.InputDirs, arguments.OutputPackagePath)
	builderRefs := refGraphForReachableTypes(pkgTypes)

	groupVersions := make(map[string]clientgentypes.GroupVersions)
	groupGoNames := make(map[string]string)
	buildersForGroupVersion := make(map[clientgentypes.GroupVersion][]builder)

	var packageList generator.Packages
	for pkg, p := range pkgTypes {
		gv, pkgName := groupAndPackageName(p)
		pkgType := types.Name{Name: pkgName, Package: pkg}

		var toGenerate []builder
		for _, t := range p.Types {
			if typePkg, ok := builderRefs[t.Name]; ok {
				toGenerate = append(toGenerate, builder{
					Type:    t,
					Builder: types.Ref(typePkg, t.Name.Name+"Builder"),
				})
			}
		}
		if len(toGenerate) == 0 {
			continue // Don't generate empty packages
		}
		sort.Sort(builderSort(toGenerate))

		// generate the apply builders
		packageList = append(packageList, generatorForBuilderPackage(arguments.OutputPackagePath, boilerplate, pkgType, gv, toGenerate, builderRefs))

		// group all the generated builders by gv so ForKind() can be generated
		groupPackageName := gv.Group.NonEmpty()
		groupVersionsEntry, ok := groupVersions[groupPackageName]
		if !ok {
			groupVersionsEntry = clientgentypes.GroupVersions{
				PackageName: groupPackageName,
				Group:       gv.Group,
			}
		}
		groupVersionsEntry.Versions = append(groupVersionsEntry.Versions, clientgentypes.PackageVersion{
			Version: gv.Version,
			Package: path.Clean(p.Path),
		})

		groupGoNames[groupPackageName] = goName(gv, p)
		buildersForGroupVersion[gv] = toGenerate
		groupVersions[groupPackageName] = groupVersionsEntry
	}

	// generate interface and ForKind() function
	packageList = append(packageList, generatorForInterface(arguments.OutputPackagePath, boilerplate, groupVersions, buildersForGroupVersion, groupGoNames))

	return packageList
}

func generatorForBuilderPackage(outputPackagePath string, boilerplate []byte, packageName types.Name, gv clientgentypes.GroupVersion, typesToGenerate []builder, builderRefs refGraph) *generator.DefaultPackage {
	return &generator.DefaultPackage{
		PackageName: strings.ToLower(gv.Version.NonEmpty()),
		PackagePath: packageName.Package,
		HeaderText:  boilerplate,
		GeneratorFunc: func(c *generator.Context) (generators []generator.Generator) {
			for _, t := range typesToGenerate {
				generators = append(generators, &builderTypeGenerator{
					DefaultGen: generator.DefaultGen{
						OptionalName: strings.ToLower(t.Type.Name.Name),
					},
					outputPackage:  outputPackagePath,
					localPackage:   packageName,
					groupVersion:   gv,
					typeToGenerate: t.Type,
					imports:        generator.NewImportTracker(),
					builderRefs:    builderRefs,
				})
			}
			return generators
		},
	}
}

func generatorForInterface(outPackagePath string, boilerplate []byte, groupVersions map[string]clientgentypes.GroupVersions, buildersForGroupVersion map[clientgentypes.GroupVersion][]builder, groupGoNames map[string]string) *generator.DefaultPackage {
	return &generator.DefaultPackage{
		PackageName: filepath.Base(outPackagePath),
		PackagePath: outPackagePath,
		HeaderText:  boilerplate,
		GeneratorFunc: func(c *generator.Context) (generators []generator.Generator) {
			generators = append(generators, &interfaceGenerator{
				DefaultGen: generator.DefaultGen{
					OptionalName: "interface",
				},
				outputPackage:        outPackagePath,
				imports:              generator.NewImportTracker(),
				groupVersions:        groupVersions,
				typesForGroupVersion: buildersForGroupVersion,
				groupGoNames:         groupGoNames,
			})
			return generators
		},
	}
}

func goName(gv clientgentypes.GroupVersion, p *types.Package) string {
	goName := namer.IC(strings.Split(gv.Group.NonEmpty(), ".")[0])
	if override := types.ExtractCommentTags("+", p.Comments)["groupGoName"]; override != nil {
		goName = namer.IC(override[0])
	}
	return goName
}

func packageTypesForInputDirs(context *generator.Context, inputDirs []string, outputPath string) map[string]*types.Package {
	pkgTypes := map[string]*types.Package{}
	for _, inputDir := range inputDirs {
		p := context.Universe.Package(inputDir)
		internal := isInternalPackage(p)
		if internal {
			klog.Warningf("Skipping internal package: %s", p.Path)
			continue
		}
		gv, groupPackageName := groupAndPackageName(p)
		pkg := filepath.Join(outputPath, groupPackageName, strings.ToLower(gv.Version.NonEmpty()))
		pkgTypes[pkg] = p
	}
	return pkgTypes
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

// isInternalPackage returns true if the package is an internal package
func isInternalPackage(p *types.Package) bool {
	for _, t := range p.Types {
		for _, member := range t.Members {
			if member.Name == "ObjectMeta" {
				return isInternal(member)
			}
		}
	}
	return false
}

// isInternal returns true if the tags for a member do not contain a json tag
func isInternal(m types.Member) bool {
	_, ok := lookupJsonTags(m)
	return !ok
}
