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
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/managedfields"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/openapi"
	"k8s.io/kube-openapi/pkg/spec3"
	"sigs.k8s.io/structured-merge-diff/v4/typed"
)

// TypeConverterManager manages type converters for different GroupVersionKinds
type TypeConverterManager interface {
	// GetTypeConverter returns a type converter for the given GVK
	GetTypeConverter(gvk schema.GroupVersionKind) managedfields.TypeConverter
	Run(ctx context.Context)
}

type typeConverterCacheEntry struct {
	typeConverter managedfields.TypeConverter
	entry         openapi.GroupVersion
}

type typeConverterManager struct {
	lock                sync.RWMutex
	staticTypeConverter managedfields.TypeConverter
	typeConverterMap    map[schema.GroupVersion]typeConverterCacheEntry
	lastFetchedPaths    map[schema.GroupVersion]openapi.GroupVersion
	client              openapi.Client
}

// NewTypeConverterManager creates a new TypeConverterManager
func NewTypeConverterManager(staticTypeConverter managedfields.TypeConverter, client openapi.Client) TypeConverterManager {
	return &typeConverterManager{
		staticTypeConverter: staticTypeConverter,
		typeConverterMap:    make(map[schema.GroupVersion]typeConverterCacheEntry),
		lastFetchedPaths:    make(map[schema.GroupVersion]openapi.GroupVersion),
		client:              client,
	}
}

func (t *typeConverterManager) Run(ctx context.Context) {
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		paths, err := t.client.Paths()
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to get paths: %w", err))
			return
		}

		parsedPaths := make(map[schema.GroupVersion]openapi.GroupVersion)
		for path, entry := range paths {
			if !strings.HasPrefix(path, "/apis/") && !strings.HasPrefix(path, "/api/") {
				continue
			}

			gv, err := schema.ParseGroupVersion(path)
			if err != nil {
				utilruntime.HandleError(fmt.Errorf("failed to parse group version %q: %w", path, err))
				return
			}

			parsedPaths[gv] = entry
		}

		t.lock.Lock()
		defer t.lock.Unlock()
		t.lastFetchedPaths = parsedPaths
	}, 10*time.Second)
}

func (t *typeConverterManager) GetTypeConverter(gvk schema.GroupVersionKind) managedfields.TypeConverter {
	// Check to see if the static type converter handles this GVK
	if t.staticTypeConverter != nil {
		stub := &unstructured.Unstructured{}
		stub.SetGroupVersionKind(gvk)

		if _, err := t.staticTypeConverter.ObjectToTyped(stub); err == nil {
			return t.staticTypeConverter
		}
	}

	gv := gvk.GroupVersion()

	existing, entry, err := func() (managedfields.TypeConverter, openapi.GroupVersion, error) {
		t.lock.RLock()
		defer t.lock.RUnlock()

		// If schema is not supported by static type converter, ask discovery
		// for the schema
		entry, ok := t.lastFetchedPaths[gv]
		if !ok {
			// If we can't get the schema, we can't do anything
			return nil, nil, fmt.Errorf("no schema for %v", gvk)
		}

		// If the entry schema has not changed, used the same type converter
		if existing, ok := t.typeConverterMap[gv]; ok && existing.entry.ServerRelativeURL() == entry.ServerRelativeURL() {
			// If we have a type converter for this GVK, return it
			return existing.typeConverter, existing.entry, nil
		}

		return nil, entry, nil
	}()
	if err != nil {
		utilruntime.HandleError(err)
		return nil
	} else if existing != nil {
		return existing
	}

	schBytes, err := entry.Schema(runtime.ContentTypeJSON)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to get schema for %v: %w", gvk, err))
		return nil
	}

	var sch spec3.OpenAPI
	if err := json.Unmarshal(schBytes, &sch); err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to unmarshal schema for %v: %w", gvk, err))
		return nil
	}

	// The schema has changed, or there is no entry for it, generate
	// a new type converter for this GV
	tc, err := managedfields.NewTypeConverter(sch.Components.Schemas, false)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to create type converter for %v: %w", gvk, err))
		return nil
	}

	t.lock.Lock()
	defer t.lock.Unlock()

	t.typeConverterMap[gv] = typeConverterCacheEntry{
		typeConverter: tc,
		entry:         entry,
	}

	return tc
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
