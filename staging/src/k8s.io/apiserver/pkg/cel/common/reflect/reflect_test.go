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

package reflect

import (
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"k8s.io/apimachinery/pkg/util/intstr"
	"testing"
	"time"
)

// Basic Struct from user
type Struct struct {
	S string  `json:"s"`
	I int     `json:"i"`
	B bool    `json:"b"`
	F float64 `json:"f"`
}

// Nested Struct
type Nested struct {
	Name string `json:"name"`
	Info Struct `json:"info"`
}

// Struct with Collections and Time/Duration
type Complex struct {
	ID          string             `json:"id"`
	Tags        []string           `json:"tags"`
	Metadata    map[string]string  `json:"metadata"`
	NestedObj   Nested             `json:"nestedObj"`
	Timeout     time.Duration      `json:"timeout"`
	RawBytes    []byte             `json:"rawBytes"`
	ChildPtr    *Struct            `json:"childPtr"`
	NilPtr      *Struct            `json:"nilPtr"` // Always nil
	EmptySlice  []int              `json:"emptySlice"`
	NilSlice    []int              `json:"nilSlice"` // Use nil slice
	EmptyMap    map[string]int     `json:"emptyMap"`
	NilMap      map[string]int     `json:"nilMap"` // Use nil map
	IntOrString intstr.IntOrString `json:"intOrString"`
}

// Helper to create activations easily, converting values with TypedToVal
func createActivation(vals map[string]interface{}) map[string]interface{} {
	activation := make(map[string]interface{}, len(vals))
	for k, v := range vals {
		activation[k] = TypedToVal(v)
	}
	return activation
}

// Helper to create basic struct instance
func basicStructInstance(s string, i int, b bool, f float64) Struct {
	return Struct{S: s, I: i, B: b, F: f}
}

// Helper to create complex struct instance
func complexStructInstance(id string, tags []string, meta map[string]string, nested Nested, timeout time.Duration, bytes []byte, child *Struct, emptySlice []int, nilSlice []int, emptyMap map[string]int, nilMap map[string]int, intOrString intstr.IntOrString) Complex {
	return Complex{
		ID:          id,
		Tags:        tags,
		Metadata:    meta,
		NestedObj:   nested,
		Timeout:     timeout,
		RawBytes:    bytes,
		ChildPtr:    child,
		NilPtr:      nil, // Explicitly nil
		EmptySlice:  emptySlice,
		NilSlice:    nilSlice,
		EmptyMap:    emptyMap,
		NilMap:      nilMap,
		IntOrString: intOrString,
	}
}

func TestTypedToVal(t *testing.T) {
	// Define common values for reuse
	struct1 := basicStructInstance("hello", 10, true, 1.5)
	struct1Ptr := &struct1
	struct2 := basicStructInstance("world", 20, false, 2.5)
	struct1Again := basicStructInstance("hello", 10, true, 1.5)
	zeroStruct := Struct{}
	zeroStructPtr := &Struct{}

	now := time.Now().Truncate(0) // Truncate monotonic clock for reliable comparison
	duration1 := 5 * time.Second
	nested1 := Nested{Name: "nested1", Info: struct1}

	complex1 := complexStructInstance(
		"c1",
		[]string{"a", "b", "c"},
		map[string]string{"key1": "val1", "key2": "val2"},
		nested1,
		duration1,
		[]byte("bytes1"),
		&struct2,         // Pointer to struct2
		[]int{},          // Empty Slice
		nil,              // Nil Slice
		map[string]int{}, // Empty Map
		nil,              // Nil Map
		intstr.FromInt32(5),
	)
	complex1Again := complex1
	complex2 := complexStructInstance(
		"c2",
		[]string{"x", "y"},
		map[string]string{"key3": "val3"},
		Nested{Name: "nested2", Info: struct2},
		10*time.Second,
		[]byte("bytes2"),
		&struct1, // Pointer to struct1
		[]int{1},
		[]int{1}, // non-nil slice for comparison testing
		map[string]int{"a": 1},
		map[string]int{"a": 1}, // non-nil map for comparison testing
		intstr.FromString("port"),
	)

	// Define test cases
	tests := []struct {
		name           string
		expression     string
		activation     map[string]any
		wantCompileErr bool
		wantErr        bool
		want           ref.Val
	}{
		// --- Basic Struct Access ---
		{
			name:       "basic: zero value struct",
			expression: "obj.s == '' && obj.i == 0 && obj.b == false && obj.f == 0.0",
			activation: map[string]interface{}{"obj": zeroStruct},
			want:       types.True,
		},
		{
			name:       "basic: zero value struct pointer",
			expression: "obj.s == '' && obj.i == 0 && obj.b == false && obj.f == 0.0",
			activation: map[string]interface{}{"obj": zeroStructPtr},
			want:       types.True,
		},
		{
			name:       "basic: populated struct field access",
			expression: "obj.s == 'hello' && obj.i == 10 && obj.b == true && obj.f == 1.5",
			activation: map[string]interface{}{"obj": struct1},
			want:       types.True,
		},
		{
			name:       "basic: populated struct pointer field access",
			expression: "obj.s == 'hello' && obj.i == 10 && obj.b == true && obj.f == 1.5",
			activation: map[string]interface{}{"obj": struct1Ptr},
			want:       types.True,
		},
		{
			name:       "basic: access non-existent field",
			expression: "has(obj.nonExistent)",
			activation: map[string]interface{}{"obj": struct1},
			want:       types.False, // has() returns false for non-existent fields
		},
		{
			name:       "basic: access non-existent field direct (error)",
			expression: "obj.nonExistent",
			activation: map[string]interface{}{"obj": struct1},
			wantErr:    true, // Direct access should produce an error
		},

		// --- Equality Comparisons ---
		{
			name:       "compare: identical structs",
			expression: "s1 == s1_again",
			activation: map[string]interface{}{"s1": struct1, "s1_again": struct1Again},
			want:       types.True,
		},
		{
			name:       "compare: different structs",
			expression: "s1 == s2",
			activation: map[string]interface{}{"s1": struct1, "s2": struct2},
			want:       types.False,
		},
		{
			name: "compare: struct and pointer to identical struct",
			// Note: CEL itself might not consider these equal by default depending on env setup,
			// but the underlying Go values are equal via apiequality.Semantic used in structVal.Equal
			expression: "s1 == s1_ptr",
			activation: map[string]interface{}{"s1": struct1, "s1_ptr": struct1Ptr},
			want:       types.True,
		},
		{
			name:       "compare: struct and nil",
			expression: "s1 == null",
			activation: map[string]interface{}{"s1": struct1},
			want:       types.False,
		},
		{
			name: "compare: nil struct pointer and null",
			// A nil pointer Go value becomes types.NullValue via TypedToVal
			expression: "nil_obj == null",
			activation: map[string]interface{}{"nil_obj": (*Struct)(nil)},
			want:       types.True,
		},
		{
			name:       "compare: identical complex structs",
			expression: "c1 == c1_again",
			activation: map[string]interface{}{"c1": complex1, "c1_again": complex1Again},
			want:       types.True,
		},
		{
			name:       "compare: different complex structs",
			expression: "c1 == c2",
			activation: map[string]interface{}{"c1": complex1, "c2": complex2},
			want:       types.False,
		},
		{
			name:       "compare: identical slices",
			expression: "[1, 2] == [1, 2]",
			activation: map[string]interface{}{}, // No specific vars needed
			want:       types.True,
		},
		{
			name:       "compare: different slices",
			expression: "[1, 2] == [1, 3]",
			activation: map[string]interface{}{},
			want:       types.False,
		},
		{
			name:       "compare: identical maps",
			expression: "{'a': 1, 'b': 2} == {'b': 2, 'a': 1}", // Order doesn't matter
			activation: map[string]interface{}{},
			want:       types.True,
		},
		{
			name:       "compare: different maps (value)",
			expression: "{'a': 1, 'b': 2} == {'a': 1, 'b': 3}",
			activation: map[string]interface{}{},
			want:       types.False,
		},
		{
			name:       "compare: different maps (key)",
			expression: "{'a': 1, 'b': 2} == {'a': 1, 'c': 2}",
			activation: map[string]interface{}{},
			want:       types.False,
		},
		{
			name:       "compare: time instances (equal)",
			expression: "t1 == t2",
			activation: map[string]interface{}{"t1": now, "t2": now},
			want:       types.True,
		},
		{
			name:       "compare: time instances (different)",
			expression: "t1 == t2",
			activation: map[string]interface{}{"t1": now, "t2": now.Add(time.Nanosecond)},
			want:       types.False,
		},
		{
			name:       "compare: duration instances (equal)",
			expression: "d1 == d2",
			activation: map[string]interface{}{"d1": duration1, "d2": 5 * time.Second},
			want:       types.True,
		},
		{
			name:       "compare: duration instances (different)",
			expression: "d1 == d2",
			activation: map[string]interface{}{"d1": duration1, "d2": 6 * time.Second},
			want:       types.False,
		},
		{
			name:       "compare: bytes instances (equal)",
			expression: "b1 == b2",
			activation: map[string]interface{}{"b1": []byte("abc"), "b2": []byte("abc")},
			want:       types.True,
		},
		{
			name:       "compare: bytes instances (different)",
			expression: "b1 == b2",
			activation: map[string]interface{}{"b1": []byte("abc"), "b2": []byte("abd")},
			want:       types.False,
		},
		{
			name:       "compare: empty slices",
			expression: "e1 == e2",
			// TypedToVal converts nil slices to empty ones for CEL
			activation: map[string]interface{}{"e1": []int{}, "e2": []string(nil)},
			want:       types.True,
		},
		{
			name:       "compare: empty maps",
			expression: "m1 == m2",
			// TypedToVal might handle nil maps differently or convert to empty maps
			// Check the implementation - assuming it converts nil maps for CEL comparison
			activation: map[string]interface{}{"m1": map[string]int{}, "m2": map[string]bool(nil)},
			want:       types.True,
		},

		// --- Nested Access ---
		{
			name:       "nested: access field",
			expression: "c.nestedObj.info.s == 'hello'",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "nested: compare nested struct",
			expression: "c1.nestedObj == c2.nestedObj", // Should be false
			activation: map[string]interface{}{"c1": complex1, "c2": complex2},
			want:       types.False,
		},
		{
			name:       "nested: compare identical nested struct",
			expression: "c1.nestedObj == c1_again.nestedObj", // Should be true
			activation: map[string]interface{}{"c1": complex1, "c1_again": complex1Again},
			want:       types.True,
		},

		// --- Slice/List Operations ---
		{
			name:       "slice: access element",
			expression: "c.tags[1] == 'b'",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "slice: size",
			expression: "size(c.tags) == 3",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "slice: contains ('in')",
			expression: "'b' in c.tags",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "slice: not contains ('in')",
			expression: "'d' in c.tags",
			activation: map[string]interface{}{"c": complex1},
			want:       types.False,
		},
		{
			name:       "slice: all() true",
			expression: "c.tags.all(t, t.startsWith(''))", // All non-empty strings start with ""
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "slice: all() false",
			expression: "c.tags.all(t, t == 'a')",
			activation: map[string]interface{}{"c": complex1},
			want:       types.False,
		},
		{
			name:       "slice: exists() true",
			expression: "c.tags.exists(t, t == 'c')",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "slice: exists() false",
			expression: "c.tags.exists(t, t == 'z')",
			activation: map[string]interface{}{"c": complex1},
			want:       types.False,
		},
		{
			name:       "slice: out of bounds access",
			expression: "c.tags[5]",
			activation: map[string]interface{}{"c": complex1},
			wantErr:    true,
			want:       nil,
		},
		{
			name:       "slice: empty slice size",
			expression: "size(c.emptySlice) == 0",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "slice: nil slice size (TypedToVal likely makes it empty)",
			expression: "size(c.nilSlice) == 0",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True, // Assuming TypedToVal converts nil slice to empty list for CEL
		},
		{
			name:       "slice: exists() on empty",
			expression: "c.emptySlice.exists(x, true)",
			activation: map[string]interface{}{"c": complex1},
			want:       types.False,
		},
		{
			name:       "slice: all() on empty",
			expression: "c.emptySlice.all(x, false)",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True, // all() is true for empty lists
		},

		// --- Map Operations ---
		{
			name:       "map: access element",
			expression: "c.metadata['key1'] == 'val1'",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "map: size",
			expression: "size(c.metadata) == 2",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "map: contains key ('in')",
			expression: "'key1' in c.metadata",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "map: not contains key ('in')",
			expression: "'key3' in c.metadata",
			activation: map[string]interface{}{"c": complex1},
			want:       types.False,
		},
		{
			name:       "map: has() key",
			expression: "has(c.metadata.key1)",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "map: has() non-existent key",
			expression: "has(c.metadata.key3)",
			activation: map[string]interface{}{"c": complex1},
			want:       types.False,
		},
		{
			name:       "map: access non-existent key (error)",
			expression: "c.metadata['key3']",
			activation: map[string]interface{}{"c": complex1},
			wantErr:    true,
			want:       nil,
		},
		{
			name: "map: all() on keys true",
			// Note: map iteration in CEL standard macros often iterates over keys.
			expression: "c.metadata.all(k, k.startsWith('key'))",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "map: all() on keys false",
			expression: "c.metadata.all(k, k == 'key1')",
			activation: map[string]interface{}{"c": complex1},
			want:       types.False,
		},
		{
			name:       "map: exists() on keys true",
			expression: "c.metadata.exists(k, k == 'key2')",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "map: exists() on keys false",
			expression: "c.metadata.exists(k, k == 'key3')",
			activation: map[string]interface{}{"c": complex1},
			want:       types.False,
		},
		// exists_one is useful too, but let's stick to all/exists for now
		{
			name:       "map: empty map size",
			expression: "size(c.emptyMap) == 0",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "map: nil map size (TypedToVal likely makes it empty)",
			expression: "size(c.nilMap) == 0",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True, // Assuming TypedToVal converts nil map to empty map for CEL
		},
		{
			name:       "map: exists() on empty",
			expression: "c.emptyMap.exists(k, true)",
			activation: map[string]interface{}{"c": complex1},
			want:       types.False,
		},
		{
			name:       "map: all() on empty",
			expression: "c.emptyMap.all(k, false)",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True, // all() is true for empty maps
		},

		// --- Pointer Handling ---
		{
			name:       "pointer: access through non-nil pointer field",
			expression: "c.childPtr.s == 'world'", // childPtr points to struct2
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "pointer: compare non-nil pointer field",
			expression: "c.childPtr == s2", // Comparing pointer field to actual struct
			activation: map[string]interface{}{"c": complex1, "s2": struct2},
			want:       types.True, // Equality checks underlying value
		},
		{
			name:       "pointer: access through nil pointer field (error)",
			expression: "c.nilPtr.s",
			activation: map[string]interface{}{"c": complex1},
			wantErr:    true, // Accessing field on null should error
			want:       nil,
		},
		{
			name:       "pointer: check if nil pointer field is null",
			expression: "c.nilPtr == null",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "pointer: has() on nil pointer field subfield",
			expression: "has(c.nilPtr.s)", // has() should handle the null base gracefully
			activation: map[string]interface{}{"c": complex1},
			want:       types.False, // Field doesn't exist because base is null
		},

		// --- Type Checking ---
		{
			name: "type: basic struct",
			// The exact type name depends on how cel-go represents custom types.
			// For reflection-based types, it might use Go's type path.
			// We use DynType for variables, so direct type checking might be tricky.
			// Let's test the type of fields instead.
			expression: "type(obj.s) == string",
			activation: map[string]interface{}{"obj": struct1},
			want:       types.True,
		},
		{
			name:       "type: int field",
			expression: "type(obj.i) == int",
			activation: map[string]interface{}{"obj": struct1},
			want:       types.True,
		},
		{
			name:       "type: bool field",
			expression: "type(obj.b) == bool",
			activation: map[string]interface{}{"obj": struct1},
			want:       types.True,
		},
		{
			name:       "type: float field",
			expression: "type(obj.f) == double", // CEL uses 'double' for float64
			activation: map[string]interface{}{"obj": struct1},
			want:       types.True,
		},
		{
			name:       "type: slice field",
			expression: "type(c.tags) == list", // CEL uses 'list'
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "type: map field",
			expression: "type(c.metadata) == map",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "type: duration field",
			expression: "type(c.timeout) == google.protobuf.Duration",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "type: bytes field",
			expression: "type(c.rawBytes) == bytes", // CEL uses 'bytes'
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "type: nil pointer field",
			expression: "type(c.nilPtr) == null_type", // CEL uses 'null_type'
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name: "type: struct field (might be dyn)",
			// Since we register with DynType, the outer object is dynamic.
			// Checking the type of the struct itself might yield `dynamic` or a specific object type
			// depending on CEL version and environment setup. Let's check a field's type within it.
			expression: "type(c.nestedObj.name) == string",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},

		// --- Duration Specific ---
		{
			name:       "duration: comparison",
			expression: "c.timeout == duration('5s')",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name: "duration: addition (requires CEL support)",
			// expression: "c.timestamp + c.timeout == expected_time",
			// This requires CEL math on time/duration to be fully tested
			// Let's stick to comparisons for this adapter test
			expression: "c.timeout > duration('1s')",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		// --- IntOrString Specific ---
		{
			name:       "intOrString: int comparison",
			expression: "c.intOrString == 5",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "intOrString: string comparison",
			expression: "c.intOrString == 'port'",
			activation: map[string]interface{}{"c": complex2},
			want:       types.True,
		},
		// --- Bytes Specific ---
		{
			name:       "bytes: size",
			expression: "size(c.rawBytes) == 6", // "bytes1"
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
		{
			name:       "bytes: equality",
			expression: "c.rawBytes == b'bytes1'",
			activation: map[string]interface{}{"c": complex1},
			want:       types.True,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []cel.EnvOption
			for k := range tt.activation {
				opts = append(opts, cel.Variable(k, cel.DynType))
			}
			opts = append(opts, cel.StdLib())

			env, err := cel.NewEnv(opts...)
			if err != nil {
				t.Fatalf("Env creation error: %v", err)
			}

			ast, iss := env.Compile(tt.expression)
			if iss.Err() != nil && !tt.wantCompileErr {
				t.Fatalf("Compile error: %v :: %s", iss.Err(), tt.expression)
			}
			if tt.wantCompileErr {
				if iss.Err() == nil {
					t.Fatalf("Expected an error for expression, but got none: %s", tt.expression)
				}
				return
			}

			prg, err := env.Program(ast)
			if err != nil {
				t.Fatalf("Program error: %v :: %s", err, tt.expression)
			}

			out, _, err := prg.Eval(createActivation(tt.activation))
			if err != nil && !tt.wantErr {
				t.Fatalf("Eval error: %v :: %s", err, tt.expression)
			}
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Expected an error during evaluation, but got none: %s", tt.expression)
				}
				return
			}
			expectedResult := tt.want
			if out.Equal(expectedResult) != types.True {
				t.Errorf("Mismatch for expression '%s':\n  got: %v (type: %v)\n want: %v (type: %v)",
					tt.expression, out, out.Type(), expectedResult, expectedResult.Type())
			}
		})
	}
}
