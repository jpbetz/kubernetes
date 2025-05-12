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
	"slices"
	"strings"

	"github.com/go-openapi/jsonreference"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/endpoints/openapi"
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

// Examples of valid input:
// "/openapi/v3/api/v1#/components/schemas/io.k8s.api.core.v1.PodSpec"
// "/openapi/v3/apis/apps/v1#/components/schemas/io.k8s.api.apps.v1.DaemonSet"
// "/openapi/v3/apis/certificates.k8s.io/v1#/components/schemas/io.k8s.api.certificates.v1.CertificateSigningRequest"

func (d *DefinitionsSchemaResolver) ResolveRef(ref jsonreference.Ref) (*spec.Schema, error) {
	// TODO: Dropping the URL part is not really safe. We should validate it.
	r := ref.GetPointer().String()
	if !strings.HasPrefix(r, "/components/schemas/") {
		return nil, fmt.Errorf("cannot resolve %v: %w", r, ErrSchemaNotFound)
	}
	definitionName := strings.TrimPrefix(r, "/components/schemas/")
	internalName, err := toInternalName(definitionName)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve %v: %w", r, err)
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

func toInternalName(name string) (string, error) {
	nameParts := strings.Split(name, ".")

	if len(nameParts) < 6 {
		return "", fmt.Errorf("invalid OpenAPI definition name: %v", name)
	}
	if !slices.Equal(nameParts[:3], []string{"io", "k8s", "api"}) {
		return "", fmt.Errorf("invalid OpenAPI definition name: %v", name)
	}

	group := nameParts[3]
	version := nameParts[4]
	typ := nameParts[5]

	return fmt.Sprintf("k8s.io/api/%s/%s.%s", group, version, typ), nil
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
