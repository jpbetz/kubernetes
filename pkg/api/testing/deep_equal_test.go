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

package testing

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"unique"

	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
)

// internStrings walks an object via reflection and interns all string values
// using unique.Make, so that equal strings share the same underlying data pointer.
func internStrings(v reflect.Value) {
	switch v.Kind() {
	case reflect.Ptr:
		if !v.IsNil() {
			internStrings(v.Elem())
		}
	case reflect.Interface:
		if !v.IsNil() {
			elem := v.Elem()
			if elem.Kind() == reflect.Ptr {
				internStrings(elem)
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if field.CanSet() {
				internStrings(field)
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			internStrings(v.Index(i))
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			internStrings(v.Index(i))
		}
	case reflect.Map:
		if v.IsNil() {
			return
		}
		// We need to rebuild the map to intern both keys and values.
		// Must handle named string types (e.g. v1.ResourceName) by
		// converting back to the map's key/value types.
		mapType := v.Type()
		keys := v.MapKeys()
		entries := make([]struct {
			key, val reflect.Value
		}, len(keys))
		for i, k := range keys {
			val := v.MapIndex(k)
			entries[i].key = internStringValue(k, mapType.Key())
			entries[i].val = internStringValue(val, mapType.Elem())
		}
		// Clear and re-insert with interned strings
		for _, k := range keys {
			v.SetMapIndex(k, reflect.Value{})
		}
		for _, e := range entries {
			v.SetMapIndex(e.key, e.val)
		}
	case reflect.String:
		if v.CanSet() {
			s := v.String()
			v.SetString(unique.Make(s).Value())
		}
	}
}

// internStringValue returns a new reflect.Value with the string interned,
// converted to targetType to handle named string types (e.g. v1.ResourceName).
func internStringValue(v reflect.Value, targetType reflect.Type) reflect.Value {
	if v.Kind() == reflect.String {
		s := v.String()
		interned := reflect.ValueOf(unique.Make(s).Value())
		return interned.Convert(targetType)
	}
	return v
}

// copyStringBytes walks an object via reflection and forces all strings to have
// distinct backing data by round-tripping through a byte slice. This simulates
// what happens when two objects are deserialized independently.
func copyStringBytes(v reflect.Value) {
	switch v.Kind() {
	case reflect.Ptr:
		if !v.IsNil() {
			copyStringBytes(v.Elem())
		}
	case reflect.Interface:
		if !v.IsNil() {
			elem := v.Elem()
			if elem.Kind() == reflect.Ptr {
				copyStringBytes(elem)
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if field.CanSet() {
				copyStringBytes(field)
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			copyStringBytes(v.Index(i))
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			copyStringBytes(v.Index(i))
		}
	case reflect.Map:
		if v.IsNil() {
			return
		}
		mapType := v.Type()
		keys := v.MapKeys()
		entries := make([]struct {
			key, val reflect.Value
		}, len(keys))
		for i, k := range keys {
			val := v.MapIndex(k)
			entries[i].key = copyStringBytesValue(k, mapType.Key())
			entries[i].val = copyStringBytesValue(val, mapType.Elem())
		}
		for _, k := range keys {
			v.SetMapIndex(k, reflect.Value{})
		}
		for _, e := range entries {
			v.SetMapIndex(e.key, e.val)
		}
	case reflect.String:
		if v.CanSet() {
			s := v.String()
			v.SetString(string([]byte(s)))
		}
	}
}

func copyStringBytesValue(v reflect.Value, targetType reflect.Type) reflect.Value {
	if v.Kind() == reflect.String {
		s := v.String()
		copied := reflect.ValueOf(string([]byte(s)))
		return copied.Convert(targetType)
	}
	return v
}

// padStrings walks an object via reflection and pads all non-empty strings
// to at least minLen bytes by repeating the original content.
func padStrings(v reflect.Value, minLen int) {
	switch v.Kind() {
	case reflect.Ptr:
		if !v.IsNil() {
			padStrings(v.Elem(), minLen)
		}
	case reflect.Interface:
		if !v.IsNil() {
			elem := v.Elem()
			if elem.Kind() == reflect.Ptr {
				padStrings(elem, minLen)
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if field.CanSet() {
				padStrings(field, minLen)
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			padStrings(v.Index(i), minLen)
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			padStrings(v.Index(i), minLen)
		}
	case reflect.Map:
		if v.IsNil() {
			return
		}
		mapType := v.Type()
		keys := v.MapKeys()
		entries := make([]struct {
			key, val reflect.Value
		}, len(keys))
		for i, k := range keys {
			val := v.MapIndex(k)
			entries[i].key = padStringValue(k, mapType.Key(), minLen)
			entries[i].val = padStringValue(val, mapType.Elem(), minLen)
		}
		for _, k := range keys {
			v.SetMapIndex(k, reflect.Value{})
		}
		for _, e := range entries {
			v.SetMapIndex(e.key, e.val)
		}
	case reflect.String:
		if v.CanSet() && v.Len() > 0 {
			v.SetString(padString(v.String(), minLen))
		}
	}
}

func padStringValue(v reflect.Value, targetType reflect.Type, minLen int) reflect.Value {
	if v.Kind() == reflect.String && v.Len() > 0 {
		padded := reflect.ValueOf(padString(v.String(), minLen))
		return padded.Convert(targetType)
	}
	return v
}

func padString(s string, minLen int) string {
	if len(s) >= minLen {
		return s
	}
	// Repeat the original string content to reach minLen
	return (s + strings.Repeat(s, minLen/len(s)+1))[:minLen]
}

func BenchmarkDeepEqualAllTypes(b *testing.B) {
	// Collect all external types from the scheme
	var kinds []schema.GroupVersionKind
	for gvk := range legacyscheme.Scheme.AllKnownTypes() {
		if gvk.Version == runtime.APIVersionInternal {
			continue
		}
		kinds = append(kinds, gvk)
	}

	benchmarkDeepEqualVariants(b, kinds)
}

func BenchmarkDeepEqualPod(b *testing.B) {
	kinds := []schema.GroupVersionKind{{Group: "", Version: "v1", Kind: "Pod"}}
	benchmarkDeepEqualVariants(b, kinds)
}

// benchmarkDeepEqualVariants runs DeepEqual benchmarks across string lengths and interning strategies.
// - DistinctStrings: strings have the same content but different backing arrays (simulates independent deserialization)
// - InternedStrings: strings are interned via unique.Make so equal strings share backing data
func benchmarkDeepEqualVariants(b *testing.B, kinds []schema.GroupVersionKind) {
	b.Helper()

	stringLengths := []int{0, 100, 1024, 10240}

	for _, strLen := range stringLengths {
		name := "default"
		if strLen > 0 {
			name = fmt.Sprintf("strlen_%d", strLen)
		}
		b.Run(name, func(b *testing.B) {
			b.Run("DistinctStrings", func(b *testing.B) {
				pairs := makePairs(b, kinds, func(obj, objCopy runtime.Object) {
					if strLen > 0 {
						padStrings(reflect.ValueOf(obj), strLen)
						padStrings(reflect.ValueOf(objCopy), strLen)
					}
					// Force both objects to have distinct backing data for all strings
					copyStringBytes(reflect.ValueOf(obj))
					copyStringBytes(reflect.ValueOf(objCopy))
				})
				benchmarkDeepEqual(b, pairs)
			})
			b.Run("InternedStrings", func(b *testing.B) {
				pairs := makePairs(b, kinds, func(obj, objCopy runtime.Object) {
					if strLen > 0 {
						padStrings(reflect.ValueOf(obj), strLen)
						padStrings(reflect.ValueOf(objCopy), strLen)
					}
					// Intern all strings so equal strings share backing data
					internStrings(reflect.ValueOf(obj))
					internStrings(reflect.ValueOf(objCopy))
				})
				benchmarkDeepEqual(b, pairs)
			})
		})
	}
}

type objectPair struct {
	a, aCopy runtime.Object
}

func makePairs(b *testing.B, kinds []schema.GroupVersionKind, transform func(obj, objCopy runtime.Object)) []objectPair {
	b.Helper()
	f := fuzzer.FuzzerFor(FuzzerFuncs, rand.NewSource(1), legacyscheme.Codecs)
	pairs := make([]objectPair, 0, len(kinds))
	for _, gvk := range kinds {
		obj, err := legacyscheme.Scheme.New(gvk)
		if err != nil {
			b.Fatalf("Could not create %v: %v", gvk, err)
		}
		f.Fill(obj)
		objCopy := obj.DeepCopyObject()
		if transform != nil {
			transform(obj, objCopy)
		}
		pairs = append(pairs, objectPair{a: obj, aCopy: objCopy})
	}
	return pairs
}

func benchmarkDeepEqual(b *testing.B, pairs []objectPair) {
	b.Helper()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, p := range pairs {
			if !apiequality.Semantic.DeepEqual(p.a, p.aCopy) {
				b.Fatal("DeepEqual returned false for equal objects")
			}
		}
	}
}

func BenchmarkStringCompare(b *testing.B) {
	s1 := strings.Repeat("a", 10*1024)
	s2 := strings.Repeat("a", 10*1024)

	b.Run("Equal", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if s1 != s2 {
				b.Fatal()
			}
		}
	})
	b.Run("EqualInternedHandler", func(b *testing.B) {
		h1 := unique.Make(s1)
		h2 := unique.Make(s2)
		for i := 0; i < b.N; i++ {
			if h1 != h2 {
				b.Fatal()
			}
		}
	})
	b.Run("EqualInternedString", func(b *testing.B) {
		i1 := unique.Make(s1).Value()
		i2 := unique.Make(s2).Value()
		for i := 0; i < b.N; i++ {
			if i1 != i2 {
				b.Fatal()
			}
		}
	})
}
