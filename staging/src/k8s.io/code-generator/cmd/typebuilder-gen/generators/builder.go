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
	"io"

	"k8s.io/gengo/generator"
	"k8s.io/gengo/namer"
	"k8s.io/gengo/types"
	"k8s.io/klog/v2"

	clientgentypes "k8s.io/code-generator/cmd/client-gen/types"
)

// builderTypeGenerator produces apply builders for a given GroupVersion and type.
type builderTypeGenerator struct {
	generator.DefaultGen
	outputPackage  string
	localPackage   types.Name
	groupVersion   clientgentypes.GroupVersion
	typeToGenerate *types.Type
	imports        namer.ImportTracker
	builderRefs    refGraph
}

var _ generator.Generator = &builderTypeGenerator{}

func (g *builderTypeGenerator) Filter(_ *generator.Context, t *types.Type) bool {
	return t == g.typeToGenerate
}

func (g *builderTypeGenerator) Namers(*generator.Context) namer.NameSystems {
	return namer.NameSystems{
		"raw": namer.NewRawNamer(g.localPackage.Package, g.imports),
	}
}

func (g *builderTypeGenerator) Imports(*generator.Context) (imports []string) {
	return g.imports.ImportLines()
}

func (g *builderTypeGenerator) GenerateType(c *generator.Context, t *types.Type, w io.Writer) error {
	sw := generator.NewSnippetWriter(w, c, "$", "$")

	klog.V(5).Infof("processing type %v", t)
	m := map[string]interface{}{
		"type":                  t,
		"unstructuredConverter": unstructuredConverter,
		"unstructured":          unstructured,
		"jsonUnmarshal":         jsonUnmarshal,
		"jsonMarshal":           jsonMarshal,
	}
	
	g.generateBuilderStruct(c, t, sw)
	g.generateFieldsStruct(c, t, sw)

	sw.Do(typeBuilderEnsureInitialized, m)
	sw.Do(typeBuilderConstructor, m)

	for _, member := range t.Members {
		g.generateMemberSet(t, member, sw)
		g.generateMemberRemove(t, member, sw)
		g.generateMemberGet(t, member, sw)
	}

	sw.Do(typeBuilderToUnstructured, m)
	sw.Do(typeBuilderFromUnstructured, m)
	sw.Do(typeBuilderMarshal, m)
	sw.Do(typeBuilderUnmarshal, m)
	sw.Do(typeBuilderList, m)
	sw.Do(typeBuilderMap, m)

	g.generatePrePostFunctions(t, sw)

	return sw.Error()
}

func (g *builderTypeGenerator) generateBuilderStruct(c *generator.Context, t *types.Type, sw *generator.SnippetWriter) {
	m := map[string]interface{}{
		"type": t,
	}
	sw.Do("// $.type|public$Builder represents an declarative configuration of the $.type|public$ type for use\n", m)
	sw.Do("// with apply.\n", m)
	sw.Do("type $.type|public$Builder struct {\n", m)
	for _, member := range t.Members {
		if jsonTags, ok := lookupJsonTags(member); ok {
			if !jsonTags.inline {
				continue
			}
			m := map[string]interface{}{
				"member":     member,
				"memberType": g.builderRefs.builderFieldType(member.Type),
			}
			// Inlined types cannot be embedded because they do not expose the fields of the
			// type they represent.
			sw.Do("$.member.Type|private$ $.memberType|raw$ // inlined type\n", m)
		}
	}
	sw.Do("  fields *$.type|private$Fields\n", m)
	sw.Do("}\n", m)
}

func (g *builderTypeGenerator) generateFieldsStruct(c *generator.Context, t *types.Type, sw *generator.SnippetWriter) {
	m := map[string]interface{}{
		"type": t,
	}
	sw.Do("// $.type|private$Fields is used by $.type|public$Builder for json marshalling and unmarshalling.\n", m)
	sw.Do("// Is the source-of-truth for all fields except inlined fields.\n", m)
	sw.Do("// Inline fields are copied in from their builder type in $.type|public$Builder before marshalling, and\n", m)
	sw.Do("// are copied out to the builder type in $.type|public$Builder after unmarshalling.\n", m)
	sw.Do("// Inlined builder types cannot be embedded because they do not expose their fields directly.\n", m)
	sw.Do("type $.type|private$Fields struct {\n", m)
	for _, member := range t.Members {
		if memberTags, ok := lookupJsonTags(member); ok {
			if !memberTags.inline {
				memberTags.omitempty = true
			}
			m := map[string]interface{}{
				"member":     member,
				"memberType": g.builderRefs.builderFieldType(member.Type),
				"memberTags":   memberTags,
			}
			if memberTags.inline {
				for _, inlineMember := range member.Type.Members {
					if inlineMemberTags, ok := lookupJsonTags(inlineMember); ok {
						if !inlineMemberTags.inline {
							inlineMemberTags.omitempty = true
						}
						m := map[string]interface{}{
							"type": t,
							"member":     member,
							"memberType": g.builderRefs.builderFieldType(member.Type),
							"inlinedMember":         inlineMember,
							"inlinedMemberType":     g.builderRefs.builderFieldType(inlineMember.Type),
							"inlineMemberTags":      inlineMemberTags,
						}
						sw.Do("$.inlinedMember.Name$ *$.inlinedMemberType|raw$ `json:\"$.inlineMemberTags$\"` // inlined $.type|public$Builder.$.member.Type|private$.$.inlinedMember.Name$ field\n", m)
					}
				}
			} else {
				sw.Do("$.member.Name$ *$.memberType|raw$ `json:\"$.memberTags$\"`\n", m)
			}
		}
	}
	sw.Do("}\n", m)
}

func (g *builderTypeGenerator) generateMemberSet(t *types.Type, member types.Member, sw *generator.SnippetWriter) {
	m := map[string]interface{}{
		"type":       t,
		"member":     member,
		"memberType": g.builderRefs.builderFieldType(member.Type),
	}
	sw.Do("// Set$.member.Name$ sets the $.member.Name$ field in the declarative configuration to the given value.\n", m)
	sw.Do("func (b $.type|public$Builder) Set$.member.Name$(value $.memberType|raw$) $.type|public$Builder {\n", m)
	sw.Do("  b.ensureInitialized()\n", m)
	g.generateMemberSetExpressions(member, false, sw)
	sw.Do("  return b\n", m)
	sw.Do("}\n", m)
}

func (g *builderTypeGenerator) generatePrePostFunctions(t *types.Type, sw *generator.SnippetWriter) {
	m := map[string]interface{}{
		"type":     t,
	}
	sw.Do("func (b *$.type|public$Builder) preMarshal() {\n", m)
	for _, inlineMember := range t.Members {
		if jsonTags, ok := lookupJsonTags(inlineMember); ok {
			if !jsonTags.inline {
				continue
			}
			for _, member := range inlineMember.Type.Members {
				if _, ok := lookupJsonTags(member); ok {
					m := map[string]interface{}{
						"inlineMember": inlineMember,
						"member": member,
					}
					sw.Do("if v, ok := b.$.inlineMember.Type|private$.Get$.member.Name$(); ok { \n", m)
					sw.Do("  b.fields.$.member.Name$ = &v\n", m)
					sw.Do("}\n", m)
				}
			}
		}
	}
	sw.Do("}\n", m)

	sw.Do("func (b *$.type|public$Builder) postUnmarshal() {\n", m)
	for _, inlineMember := range t.Members {
		if jsonTags, ok := lookupJsonTags(inlineMember); ok {
			if !jsonTags.inline {
				continue
			}
			for _, member := range inlineMember.Type.Members {
				if _, ok := lookupJsonTags(member); ok {
					m := map[string]interface{}{
						"inlineMember": inlineMember,
						"member": member,
					}
					sw.Do("if b.fields.$.member.Name$ != nil { \n", m)
					sw.Do("  b.$.inlineMember.Type|private$.Set$.member.Name$(*b.fields.$.member.Name$)\n", m)
					sw.Do("}\n", m)
				}
			}
		}
	}
	sw.Do("}\n", m)
}

func (g *builderTypeGenerator) generateMemberSetExpressions(member types.Member, inlined bool, sw *generator.SnippetWriter) {
	if jsonTags, ok := lookupJsonTags(member); ok {
		m := map[string]interface{}{
			"memberJsonName": jsonTags.name,
			"member":         member,
		}
		if jsonTags.inline {
			sw.Do("b.$.member.Type|private$ = value\n", m)
		} else {
			sw.Do("b.fields.$.member.Name$ = &value\n", m)
		}
	}
}

func (g *builderTypeGenerator) generateMemberRemove(t *types.Type, member types.Member, sw *generator.SnippetWriter) {
	m := map[string]interface{}{
		"type":       t,
		"member":     member,
		"memberType": g.builderRefs.builderFieldType(member.Type),
	}
	sw.Do("// Remove$.member.Name$ removes the $.member.Name$ field from the declarative configuration.\n", m)
	sw.Do("func (b $.type|public$Builder) Remove$.member.Name$() $.type|public$Builder {\n", m)
	sw.Do("  b.ensureInitialized()\n", m)
	g.generateMemberRemoveExpressions(member, sw)
	sw.Do("  return b\n", m)
	sw.Do("}\n", m)
}

func (g *builderTypeGenerator) generateMemberRemoveExpressions(member types.Member, sw *generator.SnippetWriter) {
	if jsonTags, ok := lookupJsonTags(member); ok {
		m := map[string]interface{}{
			"memberJsonName": jsonTags.name,
			"member":         member,
			"memberType": g.builderRefs.builderFieldType(member.Type),
		}
		if jsonTags.inline {
			sw.Do("b.$.member.Type|private$ = $.memberType|raw${}\n", m)
		} else {
			sw.Do("b.fields.$.member.Name$ = nil\n", m)
		}
	}
}

func (g *builderTypeGenerator) generateMemberGet(t *types.Type, member types.Member, sw *generator.SnippetWriter) {
	memberType := g.builderRefs.builderFieldType(member.Type)
	m := map[string]interface{}{
		"type":       t,
		"member":     member,
		"memberType": memberType,
	}
	sw.Do("// Get$.member.Name$ gets the $.member.Name$ field from the declarative configuration.\n", m)
	sw.Do("func (b $.type|public$Builder) Get$.member.Name$() (value $.memberType|raw$, ok bool) {\n", m)
	sw.Do("  b.ensureInitialized()\n", m)
	g.generateMemberGetExpressions(member, false, sw)
	sw.Do("}\n", m)
}

func (g *builderTypeGenerator) generateMemberGetExpressions(member types.Member, inlined bool, sw *generator.SnippetWriter) {
	if jsonTags, ok := lookupJsonTags(member); ok {
		m := map[string]interface{}{
			"memberJsonName": jsonTags.name,
			"memberType":     g.builderRefs.builderFieldType(member.Type),
			"member":         member,
		}
		if jsonTags.inline {
			sw.Do("return b.$.member.Type|private$, true\n", m)
		} else {
			sw.Do("if v := b.fields.$.member.Name$; v != nil {\n", m)
			sw.Do("  return *v, true\n", m)
			sw.Do("}\n", m)
			sw.Do("return value, false\n", m)
		}
	}
}

// ensureInitialized ensures the builder is initialized. This must be called on
// all generated functions that access the fields map of the builder since the builder type
// is exported but the wrapped map is not.
var typeBuilderEnsureInitialized = `
func (b *$.type|public$Builder) ensureInitialized() {
  if b.fields == nil {
    b.fields = &$.type|private$Fields{}
  }
}
`

var typeBuilderConstructor = `
// $.type|public$ constructs an declarative configuration of the $.type|public$ type for use with
// apply.
// Provided as a convenience.
func $.type|public$() $.type|public$Builder {
  return $.type|public$Builder{fields: &$.type|private$Fields{}}
}
`

var typeBuilderToUnstructured = `
// ToUnstructured converts $.type|public$Builder to unstructured.
func (b *$.type|public$Builder) ToUnstructured() interface{} {
  if b == nil {
    return nil
  }
  b.ensureInitialized()
  b.preMarshal()
  u, err := $.unstructuredConverter|raw$.ToUnstructured(b.fields)
  if err != nil {
    panic(err)
  }
  return u
}
`

var typeBuilderFromUnstructured = `
// FromUnstructured converts unstructured to $.type|public$Builder, replacing the contents
// of $.type|public$Builder.
func (b *$.type|public$Builder) FromUnstructured(u map[string]interface{}) error {
  m := &$.type|private$Fields{}
  err := $.unstructuredConverter|raw$.FromUnstructured(u, m)
  if err != nil {
    return err
  }
  b.fields = m
  b.postUnmarshal()
  return nil
}
`

// TODO: use jsonMarshal directly and skip ToUnstructured
var typeBuilderMarshal = `
// MarshalJSON marshals $.type|public$Builder to JSON.
func (b *$.type|public$Builder) MarshalJSON() ([]byte, error) {
  b.ensureInitialized()
  b.preMarshal()
  return $.jsonMarshal|raw$(b.fields)
}
`

// Does not work to use unstructured.UnmarshalJSON or UnstructuredJSONScheme because it expects a top level object with a Kind
// It looks like if I want to inline, I really do need to embed as well for the json to work
// which implies that I also need to get the conversion functions dropped or hidden from embedded types somehow
var typeBuilderUnmarshal = `
// UnmarshalJSON unmarshals JSON into $.type|public$Builder, replacing the contents of
// $.type|public$Builder.
func (b *$.type|public$Builder) UnmarshalJSON(data []byte) error {
  b.ensureInitialized()
  if err := $.jsonUnmarshal|raw$(data, b.fields); err != nil {
    return err
  }
  b.postUnmarshal()
  return nil
}
`

var typeBuilderList = `
// $.type|public$List represents a list of $.type|public$Builder.
// Provided as a convenience.
type $.type|public$List []$.type|public$Builder
`

var typeBuilderMap = `
// $.type|public$List represents a map of $.type|public$Builder.
// Provided as a convenience.
type $.type|public$Map map[string]$.type|public$Builder
`
