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

	"k8s.io/code-generator/cmd/transform-gen/args"
	"k8s.io/code-generator/cmd/transform-gen/config"
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

// GetTargets builds the generation targets from the pre-loaded config.
func GetTargets(context *generator.Context, a *args.Args) []generator.Target {
	boilerplate, err := gengo.GoBoilerplate(a.GoHeaderFile, "", gengo.StdGeneratedBy)
	if err != nil {
		klog.Fatalf("Failed loading boilerplate: %v", err)
	}

	targets, err := buildTargets(context, a.Config, a.InputBase, a.OutputDir, a.OutputPkg, boilerplate)
	if err != nil {
		klog.Fatalf("Failed building targets: %v", err)
	}

	return targets
}

// resolvedType holds a source type and the field tree describing which fields to include.
type resolvedType struct {
	sourceType *types.Type
	fieldTree  *config.FieldTree
	// outputPkg is the Go import path where the transform function will be generated.
	outputPkg string
	// outputDir is the filesystem directory for the output.
	outputDir string
}

func buildTargets(context *generator.Context, cfg config.Config, inputBase, outputDir, outputPkg string, boilerplate []byte) ([]generator.Target, error) {
	// Group resolved types by output package.
	byPackage := map[string][]*resolvedType{}

	// Process each group/version key in the config.
	for gv, typeConfigs := range cfg {
		inputPkg := path.Join(inputBase, gv)
		p := context.Universe.Package(inputPkg)
		if p == nil {
			return nil, fmt.Errorf("package %q (from config key %q) not found in universe", inputPkg, gv)
		}

		for typeName, fields := range typeConfigs {
			sourceType, ok := p.Types[typeName]
			if !ok {
				return nil, fmt.Errorf("type %q not found in package %q", typeName, inputPkg)
			}

			fieldTree, err := config.ParseFieldPaths(fields)
			if err != nil {
				return nil, fmt.Errorf("type %s/%s: %w", gv, typeName, err)
			}

			resolvedTree, err := resolveFieldTree(sourceType, fieldTree)
			if err != nil {
				return nil, fmt.Errorf("type %s/%s: %w", gv, typeName, err)
			}

			// Auto-inject TypeMeta and ObjectMeta for top-level types.
			// TypeMeta is always fully included. ObjectMeta is included but
			// with ManagedFields stripped (they are never needed by consumers
			// and waste significant memory).
			injectMetaFields(sourceType, resolvedTree)

			outPkg, outDir := outputLocation(outputPkg, outputDir, gv)
			rt := &resolvedType{
				sourceType: sourceType,
				fieldTree:  resolvedTree,
				outputPkg:  outPkg,
				outputDir:  outDir,
			}
			byPackage[outPkg] = append(byPackage[outPkg], rt)
		}
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
					&transformGenerator{
						GoGenerator: generator.GoGenerator{
							OutputFilename: "zz_generated.transforms.go",
						},
						resolved: capturedRts,
						localPkg: capturedPkg,
						imports:  generator.NewImportTrackerForPackage(capturedPkg),
					},
				}
			},
		})
	}

	return targets, nil
}

// outputLocation computes the output package and directory for a config key.
// For example, config key "apps/v1" with outputPkg "example.io/transforms"
// produces "example.io/transforms/apps/v1".
func outputLocation(outputPkg, outputDir, gv string) (string, string) {
	return path.Join(outputPkg, gv), filepath.Join(outputDir, gv)
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

// injectMetaFields adds TypeMeta and ObjectMeta to the field tree if the source type has them.
// TypeMeta is always fully included. ObjectMeta is included with ManagedFields
// excluded, since ManagedFields are never needed by informer consumers and
// waste significant memory.
func injectMetaFields(sourceType *types.Type, tree *config.FieldTree) {
	if tree.IncludeAll {
		return
	}
	if tree.Fields == nil {
		tree.Fields = map[string]*config.FieldTree{}
	}
	for i := range sourceType.Members {
		m := &sourceType.Members[i]
		if !m.Embedded {
			continue
		}
		memberType := dereferenceType(m.Type)
		typeName := memberType.Name.Name
		if typeName == "TypeMeta" {
			if _, exists := tree.Fields[m.Name]; !exists {
				tree.Fields[m.Name] = &config.FieldTree{IncludeAll: true}
			}
		} else if typeName == "ObjectMeta" {
			if _, exists := tree.Fields[m.Name]; !exists {
				// Include ObjectMeta but strip ManagedFields.
				// Build a tree that includes all fields except ManagedFields.
				objectMetaTree := &config.FieldTree{
					Fields: map[string]*config.FieldTree{},
				}
				for j := range memberType.Members {
					field := &memberType.Members[j]
					if field.Name == "ManagedFields" {
						continue
					}
					objectMetaTree.Fields[field.Name] = &config.FieldTree{IncludeAll: true}
				}
				tree.Fields[m.Name] = objectMetaTree
			}
		}
	}
}
