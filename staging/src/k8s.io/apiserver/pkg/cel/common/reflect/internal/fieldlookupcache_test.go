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
	"encoding/json"
	"maps"
	"reflect"
	"testing"
)

type SimpleStruct struct {
	Name string `json:"name"`
	Age  int    `json:"age,omitempty"`
	City string `json:"-"`
}

type EmbeddedStruct struct {
	Street string `json:"street"`
	Number int    `json:"number"`
}

type TrulyInlineStruct struct {
	EmbeddedStruct `json:",inline"`
	PostCode       string `json:"postCode"`
}

type ComplexStruct struct {
	SimpleStruct
	Details  EmbeddedStruct         `json:"details,inline"`
	ZipCode  string                 `json:"zipCode"`
	Country  *string                `json:"country,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// TODO: Also test against ConvertToUnstructured

// TestFieldsCacheMatchesJsonMarshal tests the cache implementation against json Marshal to ensure compatibility.
func TestFieldsCacheMatchesJsonMarshal(t *testing.T) {
	cache := NewFieldLookupCache()
	country := "Wonderland"
	detailsInstance := EmbeddedStruct{Street: "Main St", Number: 123}
	meta := map[string]interface{}{"key1": "val1", "nested": map[string]int{"num": 1}}
	testCases := []struct {
		name  string
		input interface{} // must be a struct
	}{
		{
			name:  "simple struct",
			input: SimpleStruct{Name: "Alice", Age: 30, City: "London"},
		},
		{
			name:  "simple struct with omitempty zero",
			input: SimpleStruct{Name: "Bob", City: "Paris"},
		},
		{
			name:  "complex struct",
			input: ComplexStruct{SimpleStruct: SimpleStruct{Name: "Charlie", Age: 25}, Details: detailsInstance, ZipCode: "12345", Country: &country, Metadata: meta},
		},
		{
			name:  "complex struct with omitempty zero",
			input: ComplexStruct{SimpleStruct: SimpleStruct{Name: "David"}, Details: EmbeddedStruct{Street: "Side St", Number: 456}, ZipCode: "67890"},
		},
		{
			name:  "inline struct",
			input: TrulyInlineStruct{EmbeddedStruct: EmbeddedStruct{Street: "Inline St", Number: 789}, PostCode: "INL INE"},
		},
		{
			name:  "pointer to struct",
			input: &ComplexStruct{ZipCode: "PtrZip", Details: detailsInstance, Metadata: map[string]interface{}{"ptrKey": true}},
		},
		{
			name:  "struct with nil pointer field",
			input: ComplexStruct{ZipCode: "NilPtrTest", Country: nil},
		},
		{
			name:  "empty struct",
			input: SimpleStruct{},
		},
		{
			name:  "nil pointer input",
			input: (*SimpleStruct)(nil),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cacheFields := getCacheFields(t, cache, tc.input)
			marshalFields := getMarshalFields(t, tc.input)

			if marshalFields == nil {
				if len(cacheFields) != 0 {
					t.Errorf("marshal was not an object, but cache returned fields: %v", maps.Keys(cacheFields))
				}
				return
			}
			if len(marshalFields) == 0 {
				if len(cacheFields) != 0 {
					t.Errorf("marshal was empty object '{}', but cache returned fields: %v", maps.Keys(cacheFields))
				}
				return
			}

			cacheNames := make(map[string]struct{}, len(cacheFields))
			for k := range cacheFields {
				cacheNames[k] = struct{}{}
			}
			marshalNames := make(map[string]struct{}, len(marshalFields))
			for k := range marshalFields {
				marshalNames[k] = struct{}{}
			}

			if !maps.Equal(cacheNames, marshalNames) {
				missingInCache := []string{}
				for name := range marshalNames {
					if _, ok := cacheNames[name]; !ok {
						missingInCache = append(missingInCache, name)
					}
				}
				missingInMarshal := []string{}
				for name := range cacheNames {
					if _, ok := marshalNames[name]; !ok {
						missingInMarshal = append(missingInMarshal, name)
					}
				}
				t.Errorf("nameValue name mismatch:\n  cache: %v\n  marshal: %v", missingInCache, missingInMarshal)
			}

			for name, marshalVal := range marshalFields {
				cacheVal, ok := cacheFields[name]
				if !ok {
					continue
				}
				compareValues(t, name, cacheVal, marshalVal)
			}
		})
	}
}

func getCacheFields(t *testing.T, cache *FieldLookupCache, input interface{}) map[string]reflect.Value {
	t.Helper()

	inputType := reflect.TypeOf(input)
	inputVal := reflect.ValueOf(input)
	if inputType.Kind() == reflect.Pointer {
		if inputVal.IsNil() {
			return make(map[string]reflect.Value)
		}
		inputType = inputType.Elem()
		inputVal = inputVal.Elem()
	}
	listed := cache.list(inputType, inputVal)
	cacheFieldsMap := make(map[string]reflect.Value, len(listed))
	for _, f := range listed {
		if _, exists := cacheFieldsMap[f.name]; exists {
			t.Fatalf("cache returned duplicate field name: %s", f.name)
		}
		cacheFieldsMap[f.name] = f.value
	}
	return cacheFieldsMap
}

type nameValue struct {
	name  string
	value reflect.Value
}

func (fc *FieldLookupCache) list(structType reflect.Type, structVal reflect.Value) []nameValue {
	entries := make([]nameValue, 0, structVal.NumField())
	for _, f := range fc.lookup(structType) {
		fieldVal := structVal.Field(f.index)
		if f.inline != nil {
			entries = append(entries, fc.list(f.inline, fieldVal)...)
			continue
		}
		if f.jsonTag.Omitempty && fieldVal.IsZero() {
			continue
		}
		entries = append(entries, nameValue{name: f.jsonTag.Name, value: fieldVal})
	}

	return entries
}

func getMarshalFields(t *testing.T, input interface{}) map[string]interface{} {
	t.Helper()

	b, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	if len(b) == 0 || b[0] != '{' {
		return nil
	}

	var result map[string]interface{}
	err = json.Unmarshal(b, &result)
	if err != nil {
		t.Fatalf("json.Unmarshal into map failed: %v", err)
	}
	return result
}

func compareValues(t *testing.T, name string, cacheVal reflect.Value, marshalVal interface{}) {
	t.Helper()

	b, err := json.Marshal(cacheVal.Interface())
	if err != nil {
		t.Errorf("value comparison failed for field '%s': error marshalling cache value: %v", name, err)
		return
	}

	var cacheOut interface{}
	if err := json.Unmarshal(b, &cacheOut); err != nil {
		t.Errorf("value comparison failed for field '%s': error unmarshalling cache JSON back to interface{}: %v\nJSON: %s", name, err, string(b))
		return
	}

	b, err = json.Marshal(marshalVal)
	if err != nil {
		t.Errorf("value comparison failed for field '%s': error marshalling JSON map value: %v", name, err)
		return
	}

	var marshallOut interface{}
	if err := json.Unmarshal(b, &marshallOut); err != nil {
		t.Errorf("value comparison failed for field '%s': error unmarshalling map JSON back to interface{}: %v\nJSON: %s", name, err, string(b))
		return
	}

	if !reflect.DeepEqual(cacheOut, marshallOut) {
		t.Errorf("value mismatch for field '%s':\n  Cache : %#v\n  marshal : %#v",
			name, cacheOut, marshallOut)
	}
}

func BenchmarkFieldsCache(b *testing.B) {
	benchCache := NewFieldLookupCache()
	benchVal := reflect.ValueOf(ComplexStruct{
		SimpleStruct: SimpleStruct{Name: "Benchmark", Age: 100},
		Details:      EmbeddedStruct{Street: "Bench St", Number: 99},
		ZipCode:      "12345",
	})
	benchTyp := reflect.TypeOf(ComplexStruct{})
	_ = benchCache.lookup(benchTyp) // warm cache
	fieldsToGet := []string{"details", "zipCode", "country"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, fieldName := range fieldsToGet {
			_, _ = benchCache.Get(benchTyp, benchVal, fieldName)
		}
	}
}
