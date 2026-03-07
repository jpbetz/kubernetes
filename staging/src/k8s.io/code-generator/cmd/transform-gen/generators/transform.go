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
	"io"
	"sort"

	"k8s.io/code-generator/cmd/transform-gen/config"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/namer"
	"k8s.io/gengo/v2/types"
)

// transformGenerator generates in-place transform functions that zero out
// fields not needed by the consumer. Transform functions modify the original
// API type in-place and return it. This makes them compatible with standard
// informers and listers as a cache.TransformFunc.
type transformGenerator struct {
	generator.GoGenerator

	resolved []*resolvedType
	localPkg string
	imports  namer.ImportTracker
}

var _ generator.Generator = &transformGenerator{}

func (g *transformGenerator) Filter(*generator.Context, *types.Type) bool {
	return false
}

func (g *transformGenerator) Namers(*generator.Context) namer.NameSystems {
	return namer.NameSystems{
		"raw": namer.NewRawNamer(g.localPkg, g.imports),
	}
}

func (g *transformGenerator) Imports(*generator.Context) []string {
	lines := g.imports.ImportLines()
	lines = append(lines, "\"fmt\"")
	return lines
}

func (g *transformGenerator) Init(c *generator.Context, w io.Writer) error {
	sorted := make([]*resolvedType, len(g.resolved))
	copy(sorted, g.resolved)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].sourceType.Name.Name < sorted[j].sourceType.Name.Name
	})

	for _, rt := range sorted {
		if err := g.generateTransform(w, rt); err != nil {
			return fmt.Errorf("generating transform for %s: %w", rt.sourceType.Name.Name, err)
		}
	}
	return nil
}

func (g *transformGenerator) generateTransform(w io.Writer, rt *resolvedType) error {
	typeName := rt.sourceType.Name.Name
	srcType := g.qualifiedTypeName(rt.sourceType)

	fmt.Fprintf(w, "// Transform%s zeros all fields of a *%s that are not needed,\n", typeName, srcType)
	fmt.Fprintf(w, "// modifying the object in-place. This is suitable for use as a\n")
	fmt.Fprintf(w, "// cache.TransformFunc with standard informers.\n")
	fmt.Fprintf(w, "func Transform%s(obj interface{}) (interface{}, error) {\n", typeName)
	fmt.Fprintf(w, "\to, ok := obj.(*%s)\n", srcType)
	fmt.Fprintf(w, "\tif !ok {\n")
	fmt.Fprintf(w, "\t\treturn obj, fmt.Errorf(\"unexpected type %%T\", obj)\n")
	fmt.Fprintf(w, "\t}\n")

	g.generateZeroFields(w, rt.sourceType, rt.fieldTree, "o", "\t")

	fmt.Fprintf(w, "\treturn o, nil\n")
	fmt.Fprintf(w, "}\n\n")

	return nil
}

// generateZeroFields generates statements that zero out excluded fields.
// For each non-embedded struct field, it either zeros the field (excluded),
// leaves it alone (fully included), or recurses into it (partially included).
func (g *transformGenerator) generateZeroFields(w io.Writer, t *types.Type, tree *config.FieldTree, varExpr string, indent string) {
	if tree.IncludeAll {
		return
	}

	t = dereferenceType(t)
	if t.Kind != types.Struct {
		return
	}

	for _, member := range t.Members {
		if member.Embedded {
			// Check if this embedded type has a partial tree.
			subtree := tree.HasField(member.Name)
			if subtree != nil && !subtree.IncludeAll {
				memberType := dereferenceType(member.Type)
				if memberType.Kind == types.Struct {
					g.generateZeroFields(w, memberType, subtree, varExpr+"."+member.Name, indent)
				}
			}
			continue
		}

		subtree := tree.HasField(member.Name)
		if subtree == nil {
			// Field not needed — zero it out.
			memberType := member.Type
			fmt.Fprintf(w, "%s%s.%s = %s\n", indent, varExpr, member.Name, g.zeroValue(memberType))
			continue
		}

		if subtree.IncludeAll {
			// Field entirely included — keep as-is.
			continue
		}

		// Partially included struct field — recurse.
		memberType := dereferenceType(member.Type)
		if member.Type.Kind == types.Pointer {
			// Pointer to struct — nil check then recurse.
			fmt.Fprintf(w, "%sif %s.%s != nil {\n", indent, varExpr, member.Name)
			g.generateZeroFields(w, memberType, subtree, varExpr+"."+member.Name, indent+"\t")
			fmt.Fprintf(w, "%s}\n", indent)
		} else if memberType.Kind == types.Struct {
			g.generateZeroFields(w, memberType, subtree, varExpr+"."+member.Name, indent)
		}
	}
}

// zeroValue returns the Go zero value expression for a type.
func (g *transformGenerator) zeroValue(t *types.Type) string {
	switch t.Kind {
	case types.Pointer, types.Slice, types.Map:
		return "nil"
	case types.Builtin:
		return builtinZeroValue(t.Name.Name)
	case types.Alias:
		if t.Underlying != nil {
			switch t.Underlying.Kind {
			case types.Builtin:
				return builtinZeroValue(t.Underlying.Name.Name)
			case types.Pointer, types.Slice, types.Map:
				return "nil"
			}
		}
		return g.qualifiedTypeName(t) + "{}"
	case types.Struct:
		return g.qualifiedTypeName(t) + "{}"
	default:
		return g.qualifiedTypeName(t) + "{}"
	}
}

func builtinZeroValue(name string) string {
	switch name {
	case "string":
		return `""`
	case "bool":
		return "false"
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64":
		return "0"
	default:
		return `""`
	}
}

// qualifiedTypeName returns the fully qualified reference to a type,
// adding an import if the type is from a different package.
func (g *transformGenerator) qualifiedTypeName(t *types.Type) string {
	if t.Name.Package == "" || t.Name.Package == g.localPkg {
		return t.Name.Name
	}
	g.imports.AddType(t)
	localName := g.imports.LocalNameOf(t.Name.Package)
	if localName != "" {
		return localName + "." + t.Name.Name
	}
	return t.Name.Name
}
