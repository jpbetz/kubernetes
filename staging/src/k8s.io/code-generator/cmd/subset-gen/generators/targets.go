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

package generators

import (
	"fmt"
	"path"
	"path/filepath"
	"reflect"
	"strings"

	"k8s.io/code-generator/cmd/subset-gen/args"
	"k8s.io/code-generator/cmd/subset-gen/config"
	"k8s.io/gengo/v2"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/namer"
	"k8s.io/gengo/v2/types"
	"k8s.io/klog/v2"
)

// NameSystems returns the name system used by the generators in this package.
func NameSystems() namer.NameSystems {
	return namer.NameSystems{
		"public":  namer.NewPublicNamer(0),
		"private": namer.NewPrivateNamer(0),
		"raw":     namer.NewRawNamer("", nil),
	}
}

// DefaultNameSystem returns the default name system for ordering types.
func DefaultNameSystem() string {
	return "public"
}

// GetTargets builds the generation targets from the config file.
func GetTargets(context *generator.Context, a *args.Args) []generator.Target {
	boilerplate, err := gengo.GoBoilerplate(a.GoHeaderFile, "", gengo.StdGeneratedBy)
	if err != nil {
		klog.Fatalf("Failed loading boilerplate: %v", err)
	}

	cfg, err := config.LoadConfig(a.ConfigFilePath)
	if err != nil {
		klog.Fatalf("Failed loading config: %v", err)
	}

	targets, err := buildTargets(context, cfg, a.OutputDir, a.OutputPkg, boilerplate)
	if err != nil {
		klog.Fatalf("Failed building targets: %v", err)
	}

	return targets
}

// resolvedType holds a source type and the field tree describing which fields to include.
type resolvedType struct {
	sourceType *types.Type
	fieldTree  *config.FieldTree
	// outputPkg is the Go import path where this subset type will be generated.
	outputPkg string
	// outputDir is the filesystem directory for the output.
	outputDir string
}

// typeSubsetMap tracks which types need subset generation.
// Key is the source type's fully qualified name (package.Name).
type typeSubsetMap map[types.Name]*resolvedType

func buildTargets(context *generator.Context, cfg config.Config, outputDir, outputPkg string, boilerplate []byte) ([]generator.Target, error) {
	tsm := typeSubsetMap{}

	// Build a map of short type name -> source type from all input packages.
	typesByName := map[string]*types.Type{}
	for _, inputDir := range context.Inputs {
		p := context.Universe.Package(inputDir)
		for _, t := range p.Types {
			typesByName[t.Name.Name] = t
		}
	}

	// Resolve each configured type.
	for typeName, fields := range cfg {
		sourceType, ok := typesByName[typeName]
		if !ok {
			return nil, fmt.Errorf("type %q not found in input packages", typeName)
		}

		fieldTree, err := config.ParseFieldPaths(fields)
		if err != nil {
			return nil, fmt.Errorf("type %q: %w", typeName, err)
		}

		resolvedTree, err := resolveFieldTree(sourceType, fieldTree)
		if err != nil {
			return nil, fmt.Errorf("type %q: %w", typeName, err)
		}

		outPkg, outDir := outputLocation(outputPkg, outputDir, sourceType.Name.Package)
		tsm[sourceType.Name] = &resolvedType{
			sourceType: sourceType,
			fieldTree:  resolvedTree,
			outputPkg:  outPkg,
			outputDir:  outDir,
		}

		// Recursively discover nested types that also need subset generation.
		if err := discoverNestedSubsets(context, sourceType, resolvedTree, outputPkg, outputDir, tsm); err != nil {
			return nil, fmt.Errorf("type %q: discovering nested subsets: %w", typeName, err)
		}
	}

	// Group resolved types by output package.
	byPackage := map[string][]*resolvedType{}
	for _, rt := range tsm {
		byPackage[rt.outputPkg] = append(byPackage[rt.outputPkg], rt)
	}

	// Create a target for each output package.
	var targets []generator.Target
	for pkg, rts := range byPackage {
		if len(rts) == 0 {
			continue
		}
		dir := rts[0].outputDir
		pkgName := path.Base(pkg)

		// Capture for closure.
		capturedPkg := pkg
		capturedRts := rts

		targets = append(targets, &generator.SimpleTarget{
			PkgName:       pkgName,
			PkgPath:       capturedPkg,
			PkgDir:        dir,
			HeaderComment: boilerplate,
			GeneratorsFunc: func(c *generator.Context) []generator.Generator {
				return []generator.Generator{
					&subsetGenerator{
						GoGenerator: generator.GoGenerator{
							OutputFilename: "types.go",
						},
						resolved:   capturedRts,
						localPkg:   capturedPkg,
						imports:    generator.NewImportTrackerForPackage(capturedPkg),
						allSubsets: tsm,
					},
				}
			},
		})
	}

	return targets, nil
}

// outputLocation computes the output package and directory for a source package.
// It maps the source package structure under the output base.
// For example, source "k8s.io/api/apps/v1" with outputPkg "example.io/subsets"
// produces "example.io/subsets/apps/v1".
func outputLocation(outputPkg, outputDir, sourcePkg string) (string, string) {
	// Extract the group/version suffix from the source package.
	parts := strings.Split(sourcePkg, "/")
	var suffix string
	if len(parts) >= 2 {
		suffix = strings.Join(parts[len(parts)-2:], "/")
	} else {
		suffix = parts[len(parts)-1]
	}

	return path.Join(outputPkg, suffix), filepath.Join(outputDir, suffix)
}

// resolveFieldTree converts json field names in the tree to Go field names
// by looking up the actual struct members.
func resolveFieldTree(t *types.Type, tree *config.FieldTree) (*config.FieldTree, error) {
	if tree.IncludeAll {
		return tree, nil
	}

	resolved := &config.FieldTree{
		Fields: map[string]*config.FieldTree{},
	}

	for jsonName, subtree := range tree.Fields {
		member, err := findMemberByJSONName(t, jsonName)
		if err != nil {
			return nil, fmt.Errorf("field %q in type %s: %w", jsonName, t.Name, err)
		}

		// Recursively resolve the subtree against the member's type.
		memberType := dereferenceType(member.Type)
		if memberType.Kind == types.Slice {
			memberType = dereferenceType(memberType.Elem)
		}
		if memberType.Kind == types.Map {
			memberType = dereferenceType(memberType.Elem)
		}

		var resolvedSubtree *config.FieldTree
		if subtree.IncludeAll || len(subtree.Fields) == 0 {
			resolvedSubtree = subtree
		} else if memberType.Kind == types.Struct {
			resolvedSubtree, err = resolveFieldTree(memberType, subtree)
			if err != nil {
				return nil, err
			}
		} else {
			resolvedSubtree = subtree
		}

		resolved.Fields[member.Name] = resolvedSubtree
	}

	return resolved, nil
}

// findMemberByJSONName finds the struct member that has the given JSON tag name.
// It also checks embedded types.
func findMemberByJSONName(t *types.Type, jsonName string) (*types.Member, error) {
	t = dereferenceType(t)
	if t.Kind != types.Struct {
		return nil, fmt.Errorf("type %s is not a struct", t.Name)
	}

	for i := range t.Members {
		m := &t.Members[i]

		// Check if this member matches the json name.
		memberJSONName := jsonTagName(m)
		if memberJSONName == jsonName {
			return m, nil
		}

		// For inline embedded types, recurse into them.
		if m.Embedded && isInline(m) {
			embeddedType := dereferenceType(m.Type)
			if embeddedType.Kind == types.Struct {
				if found, err := findMemberByJSONName(embeddedType, jsonName); err == nil {
					return found, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("no field with json name %q in type %s", jsonName, t.Name)
}

// jsonTagName returns the JSON name for a struct member.
func jsonTagName(m *types.Member) string {
	tag := reflect.StructTag(m.Tags).Get("json")
	if tag == "" || tag == "-" {
		return ""
	}
	name, _ := parseTag(tag)
	if name == "" {
		return m.Name
	}
	return name
}

func parseTag(tag string) (string, string) {
	if idx := strings.Index(tag, ","); idx != -1 {
		return tag[:idx], tag[idx+1:]
	}
	return tag, ""
}

// isInline returns true if the member has a json:",inline" tag.
func isInline(m *types.Member) bool {
	tag := reflect.StructTag(m.Tags).Get("json")
	_, opts := parseTag(tag)
	return strings.Contains(opts, "inline")
}

// dereferenceType follows pointers to get the underlying type.
func dereferenceType(t *types.Type) *types.Type {
	for t.Kind == types.Pointer {
		t = t.Elem
	}
	return t
}

// discoverNestedSubsets walks a type's field tree and discovers nested struct types
// that need subset generation (i.e., types that are traversed through, not included in full).
func discoverNestedSubsets(context *generator.Context, t *types.Type, tree *config.FieldTree, outputPkg, outputDir string, tsm typeSubsetMap) error {
	if tree.IncludeAll {
		return nil
	}

	t = dereferenceType(t)
	if t.Kind != types.Struct {
		return nil
	}

	for goFieldName, subtree := range tree.Fields {
		member := findMemberByGoName(t, goFieldName)
		if member == nil {
			return fmt.Errorf("field %q not found in type %s", goFieldName, t.Name)
		}

		memberType := dereferenceType(member.Type)
		if memberType.Kind == types.Slice {
			memberType = dereferenceType(memberType.Elem)
		}
		if memberType.Kind == types.Map {
			memberType = dereferenceType(memberType.Elem)
		}

		if memberType.Kind != types.Struct {
			continue
		}

		if subtree.IncludeAll {
			// Path terminates at this struct - use original type, no subset needed.
			continue
		}

		// This struct is traversed through - it needs subset generation.
		outPkg, outDir := outputLocation(outputPkg, outputDir, memberType.Name.Package)

		existing, ok := tsm[memberType.Name]
		if ok {
			// Merge field trees if this type is referenced from multiple paths.
			mergeFieldTrees(existing.fieldTree, subtree)
		} else {
			tsm[memberType.Name] = &resolvedType{
				sourceType: memberType,
				fieldTree:  subtree,
				outputPkg:  outPkg,
				outputDir:  outDir,
			}
		}

		// Continue recursing.
		if err := discoverNestedSubsets(context, memberType, subtree, outputPkg, outputDir, tsm); err != nil {
			return err
		}
	}

	return nil
}

// findMemberByGoName finds a member by its Go field name, including in embedded types.
func findMemberByGoName(t *types.Type, name string) *types.Member {
	for i := range t.Members {
		m := &t.Members[i]
		if m.Name == name {
			return m
		}
		// Check inline embedded types.
		if m.Embedded && isInline(m) {
			embeddedType := dereferenceType(m.Type)
			if embeddedType.Kind == types.Struct {
				if found := findMemberByGoName(embeddedType, name); found != nil {
					return found
				}
			}
		}
	}
	return nil
}

// mergeFieldTrees merges src into dst.
func mergeFieldTrees(dst, src *config.FieldTree) {
	if src.IncludeAll {
		dst.IncludeAll = true
		dst.Fields = nil
		return
	}
	if dst.IncludeAll {
		return
	}
	if dst.Fields == nil {
		dst.Fields = map[string]*config.FieldTree{}
	}
	for name, srcChild := range src.Fields {
		if dstChild, ok := dst.Fields[name]; ok {
			mergeFieldTrees(dstChild, srcChild)
		} else {
			dst.Fields[name] = srcChild
		}
	}
}
