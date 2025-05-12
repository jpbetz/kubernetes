/*
Copyright 2023 The Kubernetes Authors.

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

package resolver

import (
	"fmt"
	"strings"

	"github.com/go-openapi/jsonreference"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/endpoints/openapi"
	utilopenapi "k8s.io/apiserver/pkg/util/openapi"
	"k8s.io/kube-openapi/pkg/common"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

// DefinitionsSchemaResolver resolves the schema of a built-in type
// by looking up the OpenAPI definitions.
type DefinitionsSchemaResolver struct {
	defs     map[string]common.OpenAPIDefinition
	gvkToRef map[schema.GroupVersionKind]string
}

// NewDefinitionsSchemaResolver creates a new DefinitionsSchemaResolver.
// An example working setup:
// getDefinitions = "k8s.io/kubernetes/pkg/generated/openapi".GetOpenAPIDefinitions
// scheme         = "k8s.io/client-go/kubernetes/scheme".Scheme
func NewDefinitionsSchemaResolver(getDefinitions common.GetOpenAPIDefinitions, schemes ...*runtime.Scheme) *DefinitionsSchemaResolver {
	gvkToRef := make(map[schema.GroupVersionKind]string)
	namer := openapi.NewDefinitionNamer(schemes...)
	defs := getDefinitions(func(path string) spec.Ref {
		return spec.MustCreateRef(path)
	})
	for name := range defs {
		_, e := namer.GetDefinitionName(name)
		gvks := extensionsToGVKs(e)
		for _, gvk := range gvks {
			gvkToRef[gvk] = name
		}
	}
	return &DefinitionsSchemaResolver{
		gvkToRef: gvkToRef,
		defs:     defs,
	}
}

func (d *DefinitionsSchemaResolver) ResolveSchema(gvk schema.GroupVersionKind) (*spec.Schema, error) {
	ref, ok := d.gvkToRef[gvk]
	if !ok {
		return nil, fmt.Errorf("cannot resolve %v: %w", gvk, ErrSchemaNotFound)
	}
	s, err := PopulateRefs(func(ref string) (*spec.Schema, bool) {
		// find the schema by the ref string, and return a deep copy
		def, ok := d.defs[ref]
		if !ok {
			return nil, false
		}
		s := def.Schema
		return &s, true
	}, ref)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (d *DefinitionsSchemaResolver) ResolveRefs(schema *spec.Schema) (*spec.Schema, error) {
	ptr := schema.Ref.GetPointer()
	if ptr != nil && !ptr.IsEmpty() {
		s, err := d.resolveRef(schema.Ref.Ref)
		if err != nil {
			return nil, fmt.Errorf("failed to get definition name for ref %q: %v", ptr.String(), err)
		}
		return s, nil
	}
	if schema.Items != nil && schema.Items.Schema != nil {
		if replacement, err := d.ResolveRefs(schema.Items.Schema); err != nil {
			return nil, err
		} else if replacement != nil {
			schema.Items.Schema = replacement
		}
	}
	if schema.AllOf != nil {
		for i, s := range schema.AllOf {
			if replacement, err := d.ResolveRefs(&s); err != nil {
				return nil, err
			} else if replacement != nil {
				schema.AllOf[i] = *replacement
			}
		}
	}
	if schema.AnyOf != nil {
		for i, s := range schema.AnyOf {
			if replacement, err := d.ResolveRefs(&s); err != nil {
				return nil, err
			} else if replacement != nil {
				schema.AnyOf[i] = *replacement
			}
		}
	}
	if schema.OneOf != nil {
		for i, s := range schema.OneOf {
			if replacement, err := d.ResolveRefs(&s); err != nil {
				return nil, err
			} else if replacement != nil {
				schema.OneOf[i] = *replacement
			}
		}
	}
	if schema.Not != nil {
		if replacement, err := d.ResolveRefs(schema.Not); err != nil {
			return nil, err
		} else if replacement != nil {
			schema.Not = replacement
		}
	}
	for k, p := range schema.Properties {
		if replacement, err := d.ResolveRefs(&p); err != nil {
			return nil, err
		} else if replacement != nil {
			schema.Properties[k] = *replacement
		}
	}
	if schema.AdditionalProperties != nil && schema.AdditionalProperties.Schema != nil {
		if replacement, err := d.ResolveRefs(schema.AdditionalProperties.Schema); err != nil {
			return nil, err
		} else if replacement != nil {
			schema.AdditionalProperties.Schema = replacement
		}
	}
	if schema.PatternProperties != nil {
		for k, p := range schema.PatternProperties {
			if replacement, err := d.ResolveRefs(&p); err != nil {
				return nil, err
			} else if replacement != nil {
				schema.PatternProperties[k] = *replacement
			}
		}
	}
	if schema.Dependencies != nil {
		// TODO
	}
	if schema.AdditionalItems != nil && schema.AdditionalItems.Schema != nil {
		if replacement, err := d.ResolveRefs(schema.AdditionalItems.Schema); err != nil {
			return nil, err
		} else if replacement != nil {
			schema.AdditionalItems.Schema = replacement
		}
	}
	if schema.Definitions != nil {
		for k, p := range schema.Definitions {
			if replacement, err := d.ResolveRefs(&p); err != nil {
				return nil, err
			} else if replacement != nil {
				schema.Definitions[k] = *replacement
			}
		}
	}
	return schema, nil
}

func (d *DefinitionsSchemaResolver) resolveRef(ref jsonreference.Ref) (*spec.Schema, error) {
	internalName, err := toInternalName(ref)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve %v due to %w: %w", ref, err, ErrSchemaNotFound)
	}

	s, err := PopulateRefs(func(ref string) (*spec.Schema, bool) {
		// find the schema by the ref string, and return a deep copy
		def, ok := d.defs[ref]
		if !ok {
			return nil, false
		}
		s := def.Schema
		return &s, true
	}, internalName)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func toInternalName(ref jsonreference.Ref) (string, error) {
	gv, typeName, err := utilopenapi.ParseRef(ref)
	if err != nil {
		return "", fmt.Errorf("error parsing $ref %v: %w", ref, err)
	}
	group := gv.Group
	if gv.Group == "" {
		group = "core"
	} else {
		group, _, _ = strings.Cut(gv.Group, ".")
	}
	return fmt.Sprintf("k8s.io/api/%s/%s.%s", group, gv.Version, typeName), nil
}

func extensionsToGVKs(extensions spec.Extensions) []schema.GroupVersionKind {
	gvksAny, ok := extensions[extGVK]
	if !ok {
		return nil
	}
	gvks, ok := gvksAny.([]any)
	if !ok {
		return nil
	}
	result := make([]schema.GroupVersionKind, 0, len(gvks))
	for _, gvkAny := range gvks {
		// type check the map and all fields
		gvkMap, ok := gvkAny.(map[string]any)
		if !ok {
			return nil
		}
		g, ok := gvkMap["group"].(string)
		if !ok {
			return nil
		}
		v, ok := gvkMap["version"].(string)
		if !ok {
			return nil
		}
		k, ok := gvkMap["kind"].(string)
		if !ok {
			return nil
		}
		result = append(result, schema.GroupVersionKind{
			Group:   g,
			Version: v,
			Kind:    k,
		})
	}
	return result
}
