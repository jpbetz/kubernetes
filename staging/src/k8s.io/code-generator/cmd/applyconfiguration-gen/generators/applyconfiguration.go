/*
Copyright 2021 The Kubernetes Authors.

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

	"k8s.io/code-generator/cmd/client-gen/generators/util"
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
		"raw":          namer.NewRawNamer(g.localPackage.Package, g.imports),
		"singularKind": namer.NewPublicNamer(0),
	}
}

func (g *applyConfigurationGenerator) Imports(*generator.Context) (imports []string) {
	return g.imports.ImportLines()
}

// TypeParams provides a struct that an apply configuration
// is generated for as well as the apply configuration details
// and types referenced by the struct.
type TypeParams struct {
	Struct      *types.Type
	ApplyConfig applyConfig
	FieldStruct *types.Type
	Tags        util.Tags
	APIVersion  string
}

type memberParams struct {
	TypeParams
	Member     types.Member
	MemberType *types.Type
	JSONTags   JSONTags
}

func (g *applyConfigurationGenerator) GenerateType(c *generator.Context, t *types.Type, w io.Writer) error {
	sw := generator.NewSnippetWriter(w, c, "$", "$")

	klog.V(5).Infof("processing type %v", t)
	typeParams := TypeParams{
		Struct:      t,
		ApplyConfig: g.applyConfig,
		FieldStruct: types.Ref(g.applyConfig.ApplyConfiguration.Name.Package, g.applyConfig.Type.Name.Name+FieldTypeSuffix),
		Tags:        genclientTags(t),
		APIVersion:  g.groupVersion.ToAPIVersion(),
	}

	g.generateStruct(sw, typeParams)

	if typeParams.Tags.GenerateClient {
		if typeParams.Tags.NonNamespaced {
			sw.Do(clientgenTypeConstructorNonNamespaced, typeParams)
		} else {
			sw.Do(clientgenTypeConstructorNamespaced, typeParams)
		}
	} else {
		sw.Do(constructor, typeParams)
	}

	for _, member := range t.Members {
		memberType := g.refGraph.applyConfigForType(member.Type)
		if g.refGraph.isApplyConfig(member.Type) {
			memberType = &types.Type{Kind: types.Pointer, Elem: memberType}
		}
		if jsonTags, ok := lookupJSONTags(member); ok {
			memberParams := memberParams{
				TypeParams: typeParams,
				Member:     member,
				MemberType: memberType,
				JSONTags:   jsonTags,
			}
			if memberParams.Member.Embedded {
				continue
			}
			g.generateMemberSet(sw, memberParams)
		}
	}

	return sw.Error()
}

func (g *applyConfigurationGenerator) generateStruct(sw *generator.SnippetWriter, typeParams TypeParams) {
	sw.Do("// $.ApplyConfig.ApplyConfiguration|public$ represents an declarative configuration of the $.ApplyConfig.Type|public$ type for use\n", typeParams)
	sw.Do("// with apply.\n", typeParams)
	sw.Do("type $.ApplyConfig.ApplyConfiguration|public$ struct {\n", typeParams)
	for _, structMember := range typeParams.Struct.Members {
		if structMemberTags, ok := lookupJSONTags(structMember); ok {
			if !structMemberTags.inline {
				structMemberTags.omitempty = true
			}
			params := memberParams{
				TypeParams: typeParams,
				Member:     structMember,
				MemberType: g.refGraph.applyConfigForType(structMember.Type),
				JSONTags:   structMemberTags,
			}
			if structMember.Embedded {
				sw.Do("$.MemberType|raw$ `json:\"$.JSONTags$\"`\n", params)
			} else {
				sw.Do("$.Member.Name$ *$.MemberType|raw$ `json:\"$.JSONTags$\"`\n", params)
			}
		}
	}
	sw.Do("}\n", typeParams)
}

func (g *applyConfigurationGenerator) generateMemberSet(sw *generator.SnippetWriter, memberParams memberParams) {
	sw.Do("// Set$.Member.Name$ sets the $.Member.Name$ field in the declarative configuration to the given value.\n", memberParams)
	sw.Do("func (b *$.ApplyConfig.ApplyConfiguration|public$) Set$.Member.Name$(value $.MemberType|raw$) *$.ApplyConfig.ApplyConfiguration|public$ {\n", memberParams)
	if g.refGraph.isApplyConfig(memberParams.Member.Type) {
		sw.Do("b.$.Member.Name$ = value\n", memberParams)
	} else {
		sw.Do("b.$.Member.Name$ = &value\n", memberParams)
	}
	sw.Do("  return b\n", memberParams)
	sw.Do("}\n", memberParams)
}

var clientgenTypeConstructorNamespaced = `
// $.ApplyConfig.ApplyConfiguration|public$ constructs an declarative configuration of the $.ApplyConfig.Type|public$ type for use with
// apply. 
func $.ApplyConfig.Type|public$(name, namespace string) *$.ApplyConfig.ApplyConfiguration|public$ {
  b := &$.ApplyConfig.ApplyConfiguration|public${}
  b.SetName(name)
  b.SetNamespace(namespace)
  b.SetKind("$.ApplyConfig.Type|singularKind$")
  b.SetAPIVersion("$.APIVersion$")
  return b
}
`

var clientgenTypeConstructorNonNamespaced = `
// $.ApplyConfig.ApplyConfiguration|public$ constructs an declarative configuration of the $.ApplyConfig.Type|public$ type for use with
// apply.
func $.ApplyConfig.Type|public$(name string) *$.ApplyConfig.ApplyConfiguration|public$ {
  b := &$.ApplyConfig.ApplyConfiguration|public${}
  b.SetName(name)
  b.SetKind("$.ApplyConfig.Type|singularKind$")
  b.SetAPIVersion("$.APIVersion$")
  return b
}
`

var constructor = `
// $.ApplyConfig.ApplyConfiguration|public$ constructs an declarative configuration of the $.ApplyConfig.Type|public$ type for use with
// apply.
func $.ApplyConfig.Type|public$() *$.ApplyConfig.ApplyConfiguration|public$ {
  return &$.ApplyConfig.ApplyConfiguration|public${}
}
`
