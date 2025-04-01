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

// NewFieldsCache creates a cache for fast access to fields of a struct using JSON tag field names.
func NewFieldsCache() *FieldsCache {
	cache := &FieldsCache{}
	cache.value.Store(make(fieldsCacheMap))
	return cache
}

type FieldsCache struct {
	sync.Mutex
	value atomic.Value
}

type fieldsCacheMap map[reflect.Type]fields

type fields []field

type field struct {
	jsonTag JSON
	index   int

	inline reflect.Type
}

func (fc *FieldsCache) lookup(structType reflect.Type) fields {
	fieldCacheMap := fc.value.Load().(fieldsCacheMap)
	if info, ok := fieldCacheMap[structType]; ok {
		return info
	}

	// Cache miss - we need to parse the json tags.
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

func (fc *FieldsCache) findFields(t reflect.Type) fields {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	result := make([]field, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		jsonTag, ok := LookupJSON(f)
		if !ok || jsonTag.Omit {
			continue
		}

		if f.Anonymous { // embedded struct
			fieldType := f.Type
			for fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}
			if fieldType.Kind() == reflect.Struct && jsonTag.Inline {
				// embedded inline
				result = append(result, field{inline: fieldType, jsonTag: jsonTag, index: i})
				continue
			}
			// embedded in Go but treated as normal jsonTag in JSON
			if jsonTag.Name != "" {
				result = append(result, field{jsonTag: jsonTag, index: i})
				continue
			}
			continue
		}

		// normal jsonTag
		if jsonTag.Name != "" {
			result = append(result, field{jsonTag: jsonTag, index: i})
		}
	}

	return result
}

type Field struct {
	Name  string
	Value reflect.Value
}

// List returns the fields the given value according to the JSON tags specified on given type. Embedded structs
// marked with `json:",inline"` are included. List returns both the field value and JSON name of each field.
// Zero valued omitempty fields are not included in the result.
func (fc *FieldsCache) List(t reflect.Type, v reflect.Value) []Field {
	var entries []Field
	for _, f := range fc.lookup(t) {
		fieldVal := v.Field(f.index)
		if f.inline != nil {
			entries = append(entries, fc.List(f.inline, fieldVal)...)
			continue
		}
		if f.jsonTag.Omitempty && fieldVal.IsZero() {
			continue
		}
		entries = append(entries, Field{Name: f.jsonTag.Name, Value: fieldVal})
	}

	return entries
}

// Get finds a single field from the given value, respecting the name given to fields by the JSON tags on the given type.
func (fc *FieldsCache) Get(t reflect.Type, v reflect.Value, name string) (reflect.Value, bool) {
	for _, f := range fc.lookup(t) {
		if f.inline != nil {
			if inlineField, ok := fc.Get(f.inline, v.Field(f.index), name); ok {
				return inlineField, true
			}
		}
		if f.jsonTag.Name == name {
			fieldVal := v.Field(f.index)
			if f.jsonTag.Omitempty && fieldVal.IsZero() {
				return reflect.Value{}, false
			}
			return fieldVal, true
		}
	}

	return reflect.Value{}, false
}
