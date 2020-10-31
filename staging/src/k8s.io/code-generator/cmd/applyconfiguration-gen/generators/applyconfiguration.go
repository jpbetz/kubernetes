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

// applyConfigurationGenerator produces apply configurations for a given GroupVersion and type.
type applyConfigurationGenerator struct {
	generator.DefaultGen
	outputPackage string
	localPackage  types.Name
	groupVersion  clientgentypes.GroupVersion
	applyConfig   applyConfig
	imports       namer.ImportTracker
	refGraph      refGraph
}

var _ generator.Generator = &applyConfigurationGenerator{}

func (g *applyConfigurationGenerator) Filter(_ *generator.Context, t *types.Type) bool {
	return t == g.applyConfig.Type
}

func (g *applyConfigurationGenerator) Namers(*generator.Context) namer.NameSystems {
	return namer.NameSystems{
		"raw": namer.NewRawNamer(g.localPackage.Package, g.imports),
	}
}

func (g *applyConfigurationGenerator) Imports(*generator.Context) (imports []string) {
	return g.imports.ImportLines()
}

type TypeParams struct {
	Struct      *types.Type
	ApplyConfig applyConfig
	FieldStruct *types.Type
	Refs        map[string]*types.Type
}

type memberParams struct {
	TypeParams
	Member     types.Member
	MemberType *types.Type
	JsonTags   jsonTags
}

func (g *applyConfigurationGenerator) GenerateType(c *generator.Context, t *types.Type, w io.Writer) error {
	sw := generator.NewSnippetWriter(w, c, "$", "$")

	klog.V(5).Infof("processing type %v", t)
	typeParams := TypeParams{
		Struct:      t,
		ApplyConfig: g.applyConfig,
		FieldStruct: types.Ref(g.applyConfig.ApplyConfiguration.Name.Package, g.applyConfig.Type.Name.Name+FieldTypeSuffix),
		Refs: map[string]*types.Type{
			"unstructuredConverter": unstructuredConverter,
			"unstructured":          unstructured,
			"jsonUnmarshal":         jsonUnmarshal,
			"jsonMarshal":           jsonMarshal,
		},
	}

	g.generateStruct(t, sw, typeParams)
	sw.Do(constructor, typeParams)

	g.generateFieldsStruct(sw, typeParams)

	for _, member := range t.Members {
		memberType := g.refGraph.applyConfigForType(member.Type)
		if g.refGraph.isApplyConfig(member.Type) {
			memberType = &types.Type{Kind: types.Pointer, Elem: memberType}
		}
		if jsonTags, ok := lookupJsonTags(member); ok {
			memberParams := memberParams{
				TypeParams: typeParams,
				Member:     member,
				MemberType: memberType,
				JsonTags:   jsonTags,
			}
			g.generateMemberSet(sw, memberParams)
			g.generateMemberRemove(sw, memberParams)
			g.generateMemberGet(sw, memberParams)
		}
	}

	sw.Do(toUnstructured, typeParams)
	sw.Do(fromUnstructured, typeParams)
	sw.Do(marshal, typeParams)
	sw.Do(unmarshal, typeParams)
	sw.Do(listAlias, typeParams)
	sw.Do(mapAlias, typeParams)

	g.generatePrePostFunctions(sw, typeParams)

	return sw.Error()
}

func (g *applyConfigurationGenerator) generateStruct(t *types.Type, sw *generator.SnippetWriter, typeParams TypeParams) {
	sw.Do("// $.ApplyConfig.ApplyConfiguration|public$ represents an declarative configuration of the $.ApplyConfig.Type|public$ type for use\n", typeParams)
	sw.Do("// with apply.\n", typeParams)
	sw.Do("type $.ApplyConfig.ApplyConfiguration|public$ struct {\n", typeParams)
	for _, member := range t.Members {
		if jsonTags, ok := lookupJsonTags(member); ok {
			if !jsonTags.inline {
				continue
			}
			memberParams := memberParams{
				Member:     member,
				MemberType: g.refGraph.applyConfigForType(member.Type),
			}
			// Inlined types cannot be embedded because they do not expose the fields of the
			// type they represent.
			sw.Do("$.Member.Type|private$ *$.MemberType|raw$ // inlined type\n", memberParams)
		}
	}
	sw.Do("  fields $.FieldStruct|private$\n", typeParams)
	sw.Do("}\n", typeParams)
}

func (g *applyConfigurationGenerator) generateFieldsStruct(sw *generator.SnippetWriter, typeParams TypeParams) {
	sw.Do("// $.FieldStruct|private$ owns all fields except inlined fields.\n", typeParams)
	sw.Do("// Inline fields are owned by their respective inline type in $.ApplyConfig.ApplyConfiguration|public$.\n", typeParams)
	sw.Do("// They are copied to this type before marshalling, and are copied out\n", typeParams)
	sw.Do("// after unmarshalling. The inlined types cannot be embedded because they do\n", typeParams)
	sw.Do("// not expose their fields directly.\n", typeParams)
	sw.Do("type $.FieldStruct|private$ struct {\n", typeParams)
	for _, structMember := range typeParams.Struct.Members {
		if structMemberTags, ok := lookupJsonTags(structMember); ok {
			if !structMemberTags.inline {
				structMemberTags.omitempty = true
			}
			if structMemberTags.inline {
				inlined := memberParams{
					TypeParams: typeParams,
					Member:     structMember,
					MemberType: g.refGraph.applyConfigForType(structMember.Type),
					JsonTags:   structMemberTags,
				}
				for _, member := range structMember.Type.Members {
					if memberTags, ok := lookupJsonTags(member); ok {
						if !memberTags.inline {
							memberTags.omitempty = true
						}
						params := map[string]memberParams{
							"member": {
								TypeParams: typeParams,
								Member:     member,
								MemberType: g.refGraph.applyConfigForType(member.Type),
								JsonTags:   memberTags,
							},
							"inlined": inlined,
						}
						sw.Do("$.member.Member.Name$ *$.member.MemberType|raw$ `json:\"$.member.JsonTags$\"` // inlined $.inlined.ApplyConfig.ApplyConfiguration|public$.$.inlined.Member.Type|private$.$.member.Member.Name$ field\n", params)
					}
				}
			} else {
				params := memberParams{
					TypeParams: typeParams,
					Member:     structMember,
					MemberType: g.refGraph.applyConfigForType(structMember.Type),
					JsonTags:   structMemberTags,
				}
				sw.Do("$.Member.Name$ *$.MemberType|raw$ `json:\"$.JsonTags$\"`\n", params)
			}
		}
	}
	sw.Do("}\n", typeParams)
}

func (g *applyConfigurationGenerator) generateMemberSet(sw *generator.SnippetWriter, memberParams memberParams) {
	sw.Do("// Set$.Member.Name$ sets the $.Member.Name$ field in the declarative configuration to the given value.\n", memberParams)
	sw.Do("func (b *$.ApplyConfig.ApplyConfiguration|public$) Set$.Member.Name$(value $.MemberType|raw$) *$.ApplyConfig.ApplyConfiguration|public$ {\n", memberParams)
	if memberParams.JsonTags.inline {
		sw.Do("b.$.Member.Type|private$ = value\n", memberParams)
	} else if g.refGraph.isApplyConfig(memberParams.Member.Type) {
		sw.Do("b.fields.$.Member.Name$ = value\n", memberParams)
	} else {
		sw.Do("b.fields.$.Member.Name$ = &value\n", memberParams)
	}
	sw.Do("  return b\n", memberParams)
	sw.Do("}\n", memberParams)
}

func (g *applyConfigurationGenerator) generateMemberRemove(sw *generator.SnippetWriter, memberParams memberParams) {
	sw.Do("// Remove$.Member.Name$ removes the $.Member.Name$ field from the declarative configuration.\n", memberParams)
	sw.Do("func (b *$.ApplyConfig.ApplyConfiguration|public$) Remove$.Member.Name$() *$.ApplyConfig.ApplyConfiguration|public$ {\n", memberParams)
	if memberParams.JsonTags.inline {
		sw.Do("b.$.Member.Type|private$ = nil\n", memberParams)
	} else {
		sw.Do("b.fields.$.Member.Name$ = nil\n", memberParams)
	}
	sw.Do("  return b\n", memberParams)
	sw.Do("}\n", memberParams)
}

func (g *applyConfigurationGenerator) generateMemberGet(sw *generator.SnippetWriter, memberParams memberParams) {
	sw.Do("// Get$.Member.Name$ gets the $.Member.Name$ field from the declarative configuration.\n", memberParams)
	sw.Do("func (b *$.ApplyConfig.ApplyConfiguration|public$) Get$.Member.Name$() (value $.MemberType|raw$, ok bool) {\n", memberParams)
	if memberParams.JsonTags.inline {
		sw.Do("return b.$.Member.Type|private$, true\n", memberParams)
	} else if g.refGraph.isApplyConfig(memberParams.Member.Type) {
		sw.Do("return b.fields.$.Member.Name$, b.fields.$.Member.Name$ != nil\n", memberParams)
	} else {
		sw.Do("if v := b.fields.$.Member.Name$; v != nil {\n", memberParams)
		sw.Do("  return *v, true\n", memberParams)
		sw.Do("}\n", memberParams)
		sw.Do("return value, false\n", memberParams)
	}
	sw.Do("}\n", memberParams)
}

func (g *applyConfigurationGenerator) generatePrePostFunctions(sw *generator.SnippetWriter, typeParams TypeParams) {
	sw.Do("func (b *$.ApplyConfig.ApplyConfiguration|public$) preMarshal() {\n", typeParams)
	for _, inlineMember := range typeParams.Struct.Members {
		if jsonTags, ok := lookupJsonTags(inlineMember); ok {
			if !jsonTags.inline {
				continue
			}
			inlined := memberParams{
				Member:     inlineMember,
				MemberType: g.refGraph.applyConfigForType(inlineMember.Type),
			}
			sw.Do("if b.$.Member.Type|private$ != nil {\n", inlined)
			for _, member := range inlineMember.Type.Members {
				if _, ok := lookupJsonTags(member); ok {
					m := map[string]memberParams{
						"inlined": inlined,
						"member": {
							TypeParams: typeParams,
							Member:     member,
							MemberType: g.refGraph.applyConfigForType(member.Type),
						},
					}
					sw.Do("if v, ok := b.$.inlined.Member.Type|private$.Get$.member.Member.Name$(); ok { \n", m)
					if g.refGraph.isApplyConfig(member.Type) {
						sw.Do("  b.fields.$.member.Member.Name$ = v\n", m)
					} else {
						sw.Do("  b.fields.$.member.Member.Name$ = &v\n", m)
					}
					sw.Do("}\n", m)
				}
			}
			sw.Do("}\n", inlined)
		}
	}
	sw.Do("}\n", typeParams)

	sw.Do("func (b *$.ApplyConfig.ApplyConfiguration|public$) postUnmarshal() {\n", typeParams)
	for _, inlineMember := range typeParams.Struct.Members {
		if jsonTags, ok := lookupJsonTags(inlineMember); ok {
			if !jsonTags.inline {
				continue
			}
			inlined := memberParams{
				Member:     inlineMember,
				MemberType: g.refGraph.applyConfigForType(inlineMember.Type),
			}
			sw.Do("if b.$.Member.Type|private$ == nil {\n", inlined)
			sw.Do("  b.$.Member.Type|private$ = &$.MemberType|raw${}\n", inlined)
			sw.Do("}\n", inlined)
			for _, member := range inlineMember.Type.Members {
				if _, ok := lookupJsonTags(member); ok {
					m := map[string]memberParams{
						"inlined": inlined,
						"member": {
							TypeParams: typeParams,
							Member:     member,
							MemberType: g.refGraph.applyConfigForType(member.Type),
						},
					}
					sw.Do("if b.fields.$.member.Member.Name$ != nil { \n", m)
					if g.refGraph.isApplyConfig(member.Type) {
						sw.Do("  b.$.inlined.Member.Type|private$.Set$.member.Member.Name$(b.fields.$.member.Member.Name$)\n", m)
					} else {
						sw.Do("  b.$.inlined.Member.Type|private$.Set$.member.Member.Name$(*b.fields.$.member.Member.Name$)\n", m)
					}
					sw.Do("}\n", m)
				}
			}
		}
	}
	sw.Do("}\n", typeParams)
}

var constructor = `
// $.ApplyConfig.ApplyConfiguration|public$ constructs an declarative configuration of the $.ApplyConfig.Type|public$ type for use with
// apply.
func $.ApplyConfig.Type|public$() *$.ApplyConfig.ApplyConfiguration|public$ {
  return &$.ApplyConfig.ApplyConfiguration|public${}
}
`

var toUnstructured = `
// ToUnstructured converts $.ApplyConfig.ApplyConfiguration|public$ to unstructured.
func (b *$.ApplyConfig.ApplyConfiguration|public$) ToUnstructured() interface{} {
  if b == nil {
    return nil
  }
  b.preMarshal()
  u, err := $.Refs.unstructuredConverter|raw$.ToUnstructured(&b.fields)
  if err != nil {
    panic(err)
  }
  return u
}
`

var fromUnstructured = `
// FromUnstructured converts unstructured to $.ApplyConfig.ApplyConfiguration|public$, replacing the contents
// of $.ApplyConfig.ApplyConfiguration|public$.
func (b *$.ApplyConfig.ApplyConfiguration|public$) FromUnstructured(u map[string]interface{}) error {
  m := &$.FieldStruct|private${}
  err := $.Refs.unstructuredConverter|raw$.FromUnstructured(u, m)
  if err != nil {
    return err
  }
  b.fields = *m
  b.postUnmarshal()
  return nil
}
`

var marshal = `
// MarshalJSON marshals $.ApplyConfig.ApplyConfiguration|public$ to JSON.
func (b *$.ApplyConfig.ApplyConfiguration|public$) MarshalJSON() ([]byte, error) {
  b.preMarshal()
  return $.Refs.jsonMarshal|raw$(b.fields)
}
`

var unmarshal = `
// UnmarshalJSON unmarshals JSON into $.ApplyConfig.ApplyConfiguration|public$, replacing the contents of
// $.ApplyConfig.ApplyConfiguration|public$.
func (b *$.ApplyConfig.ApplyConfiguration|public$) UnmarshalJSON(data []byte) error {
  if err := $.Refs.jsonUnmarshal|raw$(data, &b.fields); err != nil {
    return err
  }
  b.postUnmarshal()
  return nil
}
`

var listAlias = `
// $.ApplyConfig.Type|public$List represents a listAlias of $.ApplyConfig.ApplyConfiguration|public$.
type $.ApplyConfig.Type|public$List []*$.ApplyConfig.ApplyConfiguration|public$
`

var mapAlias = `
// $.ApplyConfig.Type|public$List represents a map of $.ApplyConfig.ApplyConfiguration|public$.
type $.ApplyConfig.Type|public$Map map[string]$.ApplyConfig.ApplyConfiguration|public$
`
