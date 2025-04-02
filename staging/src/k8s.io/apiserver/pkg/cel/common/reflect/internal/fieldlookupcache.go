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

package internal

import (
	"reflect"
	"sync"
	"sync/atomic"
)

// NewFieldLookupCache creates a cache for fast access to struct fields by JSON field name.
// This cache is designed to match json.Marshal's handling of JSON tags,
// including the handling of name, inline, omit and omitempty.
//
// This cache expects a cache hit heavy workload. It precomputes data for fast lookup
// using reflection and JSON tag parsing. Then, it loads the information into a
// copy-on-write map for fast read access.
func NewFieldLookupCache() *FieldLookupCache {
	cache := &FieldLookupCache{}
	cache.value.Store(make(fieldsCacheMap))
	return cache
}

// FieldLookupCache provides fast access to fields using JSON field names.
type FieldLookupCache struct {
	sync.Mutex
	value atomic.Value
}

// Get finds a field value from the struct given a field name.
func (fc *FieldLookupCache) Get(structType reflect.Type, structVal reflect.Value, name string) (reflect.Value, bool) {
	for _, f := range fc.lookup(structType) {
		if f.inline != nil {
			if inlineField, ok := fc.Get(f.inline, structVal.Field(f.index), name); ok {
				return inlineField, true
			}
		}
		if f.jsonTag.Name == name {
			fieldVal := structVal.Field(f.index)
			if f.jsonTag.Omitempty && fieldVal.IsZero() {
				return reflect.Value{}, false
			}
			return fieldVal, true
		}
	}

	return reflect.Value{}, false
}

type fieldsCacheMap map[reflect.Type]fields

type fields []field // do not switch to a map without supporting benchmark

type field struct {
	jsonTag JSON
	index   int

	inline reflect.Type
}

func (fc *FieldLookupCache) lookup(structType reflect.Type) fields {
	fieldCacheMap := fc.value.Load().(fieldsCacheMap)
	if info, ok := fieldCacheMap[structType]; ok {
		return info
	}

	// Cache miss
	f := fc.findFields(structType)

	fc.Lock()
	defer fc.Unlock()
	fieldCacheMap = fc.value.Load().(fieldsCacheMap)
	newFieldCacheMap := make(fieldsCacheMap)
	for k, v := range fieldCacheMap {
		newFieldCacheMap[k] = v
	}
	newFieldCacheMap[structType] = f
	fc.value.Store(newFieldCacheMap)
	return f
}

// findFields uses reflection and JSON tag parsing to compute fields in the case of a cache miss.
func (fc *FieldLookupCache) findFields(t reflect.Type) fields {
	baseType := deref(t)
	if baseType.Kind() != reflect.Struct {
		return nil
	}

	result := make([]field, 0, baseType.NumField())
	for i := 0; i < baseType.NumField(); i++ {
		f := baseType.Field(i)
		jsonTag, tagged := LookupJSON(f)
		if tagged && jsonTag.Omit {
			continue
		}

		if f.Anonymous {
			fieldType := deref(f.Type)
			if fieldType.Kind() != reflect.Struct {
				continue
			}

			if tagged && jsonTag.Name != "" {
				result = append(result, field{jsonTag: jsonTag, index: i})
				continue
			}

			if !tagged || jsonTag.Inline { // json marshal inlines untagged embedded structs
				result = append(result, field{inline: fieldType, jsonTag: jsonTag, index: i})
				continue
			}
			continue
		}

		if tagged && jsonTag.Name != "" {
			result = append(result, field{jsonTag: jsonTag, index: i})
		}
	}

	return result
}

func deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}
