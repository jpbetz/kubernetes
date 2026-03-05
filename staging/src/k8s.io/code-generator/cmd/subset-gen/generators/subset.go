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
	"strings"

	"k8s.io/code-generator/cmd/subset-gen/config"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/namer"
	"k8s.io/gengo/v2/types"
	"k8s.io/klog/v2"
)

// subsetGenerator generates subset structs for all resolved types in a single package,
// writing them to a single output file (types.go).
type subsetGenerator struct {
	generator.GoGenerator

	resolved   []*resolvedType
	localPkg   string
	imports    namer.ImportTracker
	subsetBase string         // base output package for this subset definition
	allSubsets typeSubsetMap   // all types that have subsets in this run
	listInfos  []*listTypeInfo // list types to generate in this package
}

var _ generator.Generator = &subsetGenerator{}

func (g *subsetGenerator) Filter(_ *generator.Context, t *types.Type) bool {
	for _, rt := range g.resolved {
		if t == rt.sourceType {
			return true
		}
	}
	return false
}

func (g *subsetGenerator) Namers(*generator.Context) namer.NameSystems {
	return namer.NameSystems{
		"raw": namer.NewRawNamer(g.localPkg, g.imports),
	}
}

func (g *subsetGenerator) Imports(*generator.Context) []string {
	return g.imports.ImportLines()
}

func (g *subsetGenerator) GenerateType(c *generator.Context, t *types.Type, w io.Writer) error {
	// Find the resolvedType for this type.
	for _, rt := range g.resolved {
		if t == rt.sourceType {
			klog.V(5).Infof("generating subset for type %v", t)
			return g.generateStruct(w, t, rt)
		}
	}
	return fmt.Errorf("type %v not found in resolved types", t)
}

func (g *subsetGenerator) generateStruct(w io.Writer, t *types.Type, rt *resolvedType) error {
	tree := rt.fieldTree

	// Write marker comments for top-level types and list types.
	isListType := strings.HasSuffix(t.Name.Name, "List")
	if rt.topLevel {
		// Top-level resource types get +genclient and deepcopy-gen marker.
		fmt.Fprint(w, "// +genclient\n")
		fmt.Fprint(w, "// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object\n")
	} else if isListType {
		// List types only get deepcopy-gen marker.
		fmt.Fprint(w, "// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object\n")
	}

	// Write doc comments (stripped of source markers).
	comments := filterCommentMarkers(append(t.SecondClosestCommentLines, t.CommentLines...))
	if len(comments) > 0 {
		fmt.Fprint(w, comments)
	}

	fmt.Fprintf(w, "type %s struct {\n", t.Name.Name)

	for _, member := range t.Members {
		if !g.shouldIncludeMember(t, &member, tree) {
			continue
		}

		// Write member comments.
		memberComments := filterCommentMarkers(member.CommentLines)
		if len(memberComments) > 0 {
			fmt.Fprint(w, memberComments)
		}

		fieldType := g.resolveFieldType(&member, tree)

		if member.Embedded {
			if member.Tags != "" {
				fmt.Fprintf(w, "\t%s `%s`\n", fieldType, member.Tags)
			} else {
				fmt.Fprintf(w, "\t%s\n", fieldType)
			}
		} else {
			if member.Tags != "" {
				fmt.Fprintf(w, "\t%s %s `%s`\n", member.Name, fieldType, member.Tags)
			} else {
				fmt.Fprintf(w, "\t%s %s\n", member.Name, fieldType)
			}
		}
	}

	fmt.Fprint(w, "}\n\n")
	return nil
}

// shouldIncludeMember returns true if the member should be included in the subset.
func (g *subsetGenerator) shouldIncludeMember(t *types.Type, member *types.Member, tree *config.FieldTree) bool {
	if tree.IncludeAll {
		return true
	}

	// For embedded types, check if any of the embedded type's fields are selected.
	if member.Embedded {
		// Inline embedded types: check if any field selected matches a field in this embedded type,
		// or if the tree explicitly includes this embedded type by name (e.g. auto-injected TypeMeta).
		if isInline(member) {
			if tree.HasField(member.Name) != nil {
				return true
			}
			embeddedType := dereferenceType(member.Type)
			if embeddedType.Kind == types.Struct {
				for fieldName := range tree.Fields {
					if findMemberByGoName(embeddedType, fieldName) != nil {
						return true
					}
				}
			}
			return false
		}
		// Non-inline embedded: check if the embedded type name matches a selected field.
		jsonName := jsonTagName(member)
		if jsonName != "" {
			return tree.HasField(member.Name) != nil
		}
		return false
	}

	return tree.HasField(member.Name) != nil
}

// resolveFieldType determines the Go type string for a member's field type.
// If the field's type has a subset generated for it, use the subset type.
// Otherwise, use the original type.
func (g *subsetGenerator) resolveFieldType(member *types.Member, tree *config.FieldTree) string {
	subtree := tree.HasField(member.Name)

	// For embedded types with inline tag, pass through the tree as-is
	// since the embedded type's fields are directly in the parent's tree.
	if member.Embedded && isInline(member) {
		subtree = tree
	}

	return g.typeString(member.Type, subtree)
}

// typeString produces the Go source representation of a type,
// substituting subset types where appropriate.
func (g *subsetGenerator) typeString(t *types.Type, subtree *config.FieldTree) string {
	switch t.Kind {
	case types.Pointer:
		return "*" + g.typeString(t.Elem, subtree)

	case types.Slice:
		return "[]" + g.typeString(t.Elem, subtree)

	case types.Array:
		return fmt.Sprintf("[%d]%s", t.Elem.Len, g.typeString(t.Elem, subtree))

	case types.Map:
		// Map keys are always included as-is; values may be subset types.
		keyStr := g.typeString(t.Key, nil)
		valStr := g.typeString(t.Elem, subtree)
		return fmt.Sprintf("map[%s]%s", keyStr, valStr)

	case types.Struct:
		return g.structTypeRef(t, subtree)

	case types.Alias:
		// For type aliases, check if the underlying type needs subsetting.
		if _, hasSubset := g.allSubsets[t.Name]; hasSubset {
			return g.structTypeRef(t, subtree)
		}
		return g.rawTypeName(t)

	default:
		return g.rawTypeName(t)
	}
}

// structTypeRef returns the type reference for a struct type.
// If the type has a subset definition, reference the generated subset package.
// Otherwise, reference the original type.
func (g *subsetGenerator) structTypeRef(t *types.Type, subtree *config.FieldTree) string {
	// If subtree is nil or IncludeAll, use the original type.
	if subtree == nil || subtree.IncludeAll {
		return g.rawTypeName(t)
	}

	// Check if this type has a generated subset.
	if rt, ok := g.allSubsets[t.Name]; ok {
		// Reference the subset type in its output package.
		if rt.outputPkg == g.localPkg {
			return t.Name.Name
		}
		g.imports.AddType(types.Ref(rt.outputPkg, t.Name.Name))
		return g.imports.LocalNameOf(rt.outputPkg) + "." + t.Name.Name
	}

	// No subset - use original.
	return g.rawTypeName(t)
}

// rawTypeName returns the fully qualified type name, tracking imports as needed.
func (g *subsetGenerator) rawTypeName(t *types.Type) string {
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

func (g *subsetGenerator) Finalize(c *generator.Context, w io.Writer) error {
	// Generate list types after all regular types.
	for _, li := range g.listInfos {
		if err := g.generateListType(w, li); err != nil {
			return err
		}
	}
	return nil
}

func (g *subsetGenerator) generateListType(w io.Writer, li *listTypeInfo) error {
	// List types get deepcopy-gen marker.
	fmt.Fprint(w, "// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object\n")
	fmt.Fprintf(w, "// %s is a list of %s objects.\n", li.listName, li.itemType.Name.Name)
	fmt.Fprintf(w, "type %s struct {\n", li.listName)

	// Emit TypeMeta and ListMeta from the source list type.
	if li.sourceListType != nil {
		for i := range li.sourceListType.Members {
			m := &li.sourceListType.Members[i]
			memberType := dereferenceType(m.Type)
			if memberType.Name.Name == "TypeMeta" || memberType.Name.Name == "ListMeta" {
				fieldType := g.rawTypeName(m.Type)
				if m.Embedded {
					if m.Tags != "" {
						fmt.Fprintf(w, "\t%s `%s`\n", fieldType, m.Tags)
					} else {
						fmt.Fprintf(w, "\t%s\n", fieldType)
					}
				}
			}
		}
	}

	// Emit Items field.
	itemsTags := `json:"items" protobuf:"bytes,2,rep,name=items"`
	if li.sourceListType != nil {
		for i := range li.sourceListType.Members {
			m := &li.sourceListType.Members[i]
			if m.Name == "Items" {
				itemsTags = m.Tags
				break
			}
		}
	}
	fmt.Fprintf(w, "\tItems []%s `%s`\n", li.itemType.Name.Name, itemsTags)

	fmt.Fprint(w, "}\n\n")
	return nil
}

// filterCommentMarkers removes comment lines starting with '+' (codegen markers)
// and ensures all comments have the proper // prefix.
func filterCommentMarkers(comments []string) string {
	b := strings.Builder{}
	for _, comment := range comments {
		trimmed := strings.TrimSpace(comment)
		if strings.HasPrefix(trimmed, "+") {
			continue
		}
		b.WriteString("// " + trimmed + "\n")
	}
	return b.String()
}

// docGenerator generates a doc.go file with package-level marker comments.
type docGenerator struct {
	generator.GoGenerator
	groupName string
}

var _ generator.Generator = &docGenerator{}

func (g *docGenerator) Filter(*generator.Context, *types.Type) bool {
	return false
}

func (g *docGenerator) Imports(*generator.Context) []string {
	return nil
}

func (g *docGenerator) Init(c *generator.Context, w io.Writer) error {
	fmt.Fprint(w, "// +k8s:deepcopy-gen=package\n")
	fmt.Fprintf(w, "// +groupName=%s\n", g.groupName)
	return nil
}
