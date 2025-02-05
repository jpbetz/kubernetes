/*
Copyright 2024 The Kubernetes Authors.

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

package patch

import (
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/openapi"
	"k8s.io/kube-openapi/pkg/spec3"
	"sigs.k8s.io/structured-merge-diff/v4/typed"
)

// TypeConverterProvider is a function that returns a type converter for a given GVK
type TypeConverterProvider func(gvk schema.GroupVersionKind) (managedfields.TypeConverter, error)

// NewStaticTypeConverterProvider creates a type converter that maps GVKs to their corresponding type converters.
// The client is expected to be backed by a static file that never changes.
func NewStaticTypeConverterProvider(client openapi.Client) (TypeConverterProvider, error) {
	// Initialize type converter map
	typeConverterMap := make(map[schema.GroupVersion]managedfields.TypeConverter)

	// Get all paths once since the client is static
	paths, err := client.Paths()
	if err != nil {
		return nil, fmt.Errorf("failed to get paths: %w", err)
	}

	// Process all paths and create type converters
	for path, entry := range paths {
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		if !strings.HasPrefix(path, "/apis/") && !strings.HasPrefix(path, "/api/") {
			continue
		}

		gv, err := schema.ParseGroupVersion(path)
		if err != nil {
			return nil, fmt.Errorf("failed to parse group version %q: %w", path, err)
		}
		schBytes, err := entry.Schema(runtime.ContentTypeJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to get schema for %v: %w", gv, err)
		}

		var sch spec3.OpenAPI
		if err := json.Unmarshal(schBytes, &sch); err != nil {
			return nil, fmt.Errorf("failed to unmarshal schema for %v: %w", gv, err)
		}

		tc, err := managedfields.NewTypeConverter(sch.Components.Schemas, false)
		if err != nil {
			return nil, fmt.Errorf("failed to create type converter for %v: %w", gv, err)
		}

		typeConverterMap[gv] = tc
	}
	// Return a function that maps GVKs to their type converters
	return func(gvk schema.GroupVersionKind) (managedfields.TypeConverter, error) {
		if tc := typeConverterMap[gvk.GroupVersion()]; tc != nil {
			return tc, nil
		}

		return nil, fmt.Errorf("no schema for %v", gvk)
	}, nil
}

// ApplyStructuredMergeDiff applies a structured merge diff to an object and returns a copy of the object
// with the patch applied.
func ApplyStructuredMergeDiff(
	typeConverter managedfields.TypeConverter,
	originalObject runtime.Object,
	patch *unstructured.Unstructured,
) (runtime.Object, error) {
	if patch.GroupVersionKind() != originalObject.GetObjectKind().GroupVersionKind() {
		return nil, fmt.Errorf("patch (%v) and original object (%v) are not of the same gvk", patch.GroupVersionKind().String(), originalObject.GetObjectKind().GroupVersionKind().String())
	} else if typeConverter == nil {
		return nil, fmt.Errorf("type converter must not be nil")
	}

	patchObjTyped, err := typeConverter.ObjectToTyped(patch)
	if err != nil {
		return nil, fmt.Errorf("failed to convert patch object to typed object: %w", err)
	}

	err = validatePatch(patchObjTyped)
	if err != nil {
		return nil, fmt.Errorf("invalid ApplyConfiguration: %w", err)
	}

	liveObjTyped, err := typeConverter.ObjectToTyped(originalObject)
	if err != nil {
		return nil, fmt.Errorf("failed to convert original object to typed object: %w", err)
	}

	newObjTyped, err := liveObjTyped.Merge(patchObjTyped)
	if err != nil {
		return nil, fmt.Errorf("failed to merge patch: %w", err)
	}

	newObj, err := typeConverter.TypedToObject(newObjTyped)
	if err != nil {
		return nil, fmt.Errorf("failed to convert typed object back to object: %w", err)
	}

	return newObj, nil
}

// validatePatch validates that the patch is valid for apply
func validatePatch(patch *typed.TypedValue) error {
	if patch == nil {
		return fmt.Errorf("patch must not be nil")
	}

	// Validate that the patch does not try to modify atomic fields
	if err := validateNoAtomicChanges(patch); err != nil {
		return err
	}

	return nil
}

// validateNoAtomicChanges validates that the patch does not try to modify atomic fields
func validateNoAtomicChanges(tv *typed.TypedValue) error {
	if tv == nil {
		return nil
	}

	val := tv.AsValue()
	if val == nil {
		return nil
	}

	// Recursively check children
	switch {
	case val.IsList():
		list := val.AsList()
		for i := 0; i < list.Length(); i++ {
			item := list.At(i)
			childTV, err := typed.AsTyped(item, tv.Schema(), tv.TypeRef())
			if err != nil {
				return fmt.Errorf("failed to convert list item to typed value: %w", err)
			}
			if err := validateNoAtomicChanges(childTV); err != nil {
				return err
			}
		}
	case val.IsMap():
		m := val.AsMap()
		keys, found := m.Get("")
		if !found {
			return nil
		}
		keysList := keys.AsList()
		for i := 0; i < keysList.Length(); i++ {
			key := keysList.At(i)
			item, found := m.Get(key.AsString())
			if !found {
				continue
			}
			childTV, err := typed.AsTyped(item, tv.Schema(), tv.TypeRef())
			if err != nil {
				return fmt.Errorf("failed to convert map value to typed value: %w", err)
			}
			if err := validateNoAtomicChanges(childTV); err != nil {
				return err
			}
		}
	}

	return nil
}
