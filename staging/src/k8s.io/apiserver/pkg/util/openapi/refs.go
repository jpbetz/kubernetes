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

package openapi

import (
	"fmt"
	"slices"
	"strings"

	"github.com/go-openapi/jsonreference"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func ParseRef(ref jsonreference.Ref) (groupVersion schema.GroupVersion, typeName string, err error) {
	gv, err := parseRefURLPath(ref.GetURL().Path)
	if err != nil {
		return schema.GroupVersion{}, "", fmt.Errorf("invalid path %v", ref.GetURL().Path)
	}
	r := ref.GetPointer().String()
	g, v, typeName, err := parseRefFragment(r)
	if err != nil {
		return schema.GroupVersion{}, "", fmt.Errorf("invalid fragment %v", r)
	}
	if gv.Group != g {
		if g == "core" {
			if gv.Group != "" {
				return schema.GroupVersion{}, "", fmt.Errorf("group in fragment does not match group in path")
			}
		} else if strings.Split(gv.Group, ".")[0] != g {
			return schema.GroupVersion{}, "", fmt.Errorf("group in fragment does not match group in path")
		}
	}
	if gv.Version != v {
		return schema.GroupVersion{}, "", fmt.Errorf("version in fragment does not match version in path")
	}

	return gv, typeName, nil
}

func parseRefURLPath(path string) (schema.GroupVersion, error) {
	if !strings.HasPrefix(path, "/openapi/v3/") {
		return schema.GroupVersion{}, fmt.Errorf("must start with /openapi/v3/")
	}
	path = strings.TrimPrefix(path, "/openapi/v3/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return schema.GroupVersion{}, fmt.Errorf("not a valid path")
	}
	switch parts[0] {
	case "api":
		return schema.GroupVersion{Group: "", Version: parts[1]}, nil
	case "apis":
		if len(parts) < 3 {
			return schema.GroupVersion{}, fmt.Errorf("not a valid path")
		}
		return schema.GroupVersion{Group: parts[1], Version: parts[2]}, nil
	}
	return schema.GroupVersion{}, fmt.Errorf("not a valid path")
}

func parseRefFragment(fragment string) (groupPart string, versionPart string, typePart string, err error) {
	if !strings.HasPrefix(fragment, "/components/schemas/") {
		return "", "", "", fmt.Errorf("must start with /components/schemas/")
	}
	definitionName := strings.TrimPrefix(fragment, "/components/schemas/")

	nameParts := strings.Split(definitionName, ".")

	if len(nameParts) < 6 {
		return "", "", "", fmt.Errorf("invalid OpenAPI definition name")
	}
	if !slices.Equal(nameParts[:3], []string{"io", "k8s", "api"}) {
		return "", "", "", fmt.Errorf("invalid OpenAPI definition name must start with io.k8s.api")
	}
	groupPart = nameParts[3]
	versionPart = nameParts[4]
	typePart = nameParts[5]
	return groupPart, versionPart, typePart, nil
}
