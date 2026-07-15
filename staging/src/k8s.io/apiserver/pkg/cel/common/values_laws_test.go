/*
Copyright 2026 The Kubernetes Authors.

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

package common_test

// This file (and values_laws_consistency_test.go) checks algebraic laws that
// the CEL ref.Val wrappers must satisfy, across every wrapper implementation
// (TypedToVal, UnstructuredToVal, and SchemalessTypedToVal where its atomic
// semantics apply) and every operand kind (source wrapper, CEL literal,
// concatenation result, cross-implementation pair).
//
// The historical bugs in these wrappers were all law violations that only
// manifested for operand kinds no example test exercised: concat results
// losing set/map semantics, Add mutating operands, Equal accepting duplicate
// elements, literals merged under the wrong schema. Laws are therefore
// checked over the full wrapping matrix rather than as point examples.
//
// Known violations: lawKnownViolations pins law violations that exist in the
// current implementations. A pinned case that FAILS is logged and tolerated;
// a pinned case that PASSES fails the suite so the entry must be removed when
// the underlying bug is fixed. This keeps the suite green while bugs are
// worked off, without losing regression protection for everything else.

import (
	"fmt"
	"testing"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"

	"k8s.io/apiserver/pkg/cel/common"
	"k8s.io/apiserver/pkg/cel/openapi"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

// lawKnownViolations pins current, tracked violations. Key format:
// "<law>/<subject>/<detail>". Values reference the bug list in
// staging/src/k8s.io/apiserver/pkg/cel/common values-laws audit notes.
var lawKnownViolations = map[string]string{
	// Bug 1: addToMapList does not register appended keys, so RHS-internal
	// duplicate keys not present in the LHS escape last-writer-wins and the
	// result is not equal to itself.
	"map-lww-dup-rhs/mapList/typed":        "bug 1: appended keys not registered in keyToIdx",
	"map-lww-dup-rhs/mapList/unstructured": "bug 1: appended keys not registered in keyToIdx",

	// Bug 2: unstructuredMapList.toMapKey looks up escaped key prop names in
	// raw JSON data, so escaped-key map lists are not equal to themselves.
	"eq-reflexive/ifList/unstructured":                     "bug 2: escaped key props vs raw JSON names",
	"map-identity-wrapper-receiver/ifList/unstructured":    "bug 2: escaped key props vs raw JSON names",
	"map-idempotence-wrapper-receiver/ifList/unstructured": "bug 2: escaped key props vs raw JSON names",
	"cross-impl-parity/ifList/self-equality":               "bug 2: escaped key props vs raw JSON names",

	// Bug 4: map-list Equal computes merge keys from raw element shapes
	// (Go struct / map[string]interface{}), which CEL literal elements and
	// cross-implementation elements do not have. wideList repeats the mapList
	// pattern and is excluded from the symmetric law rather than re-pinned.
	"eq-literal/mapList/typed":                                "bug 4: toMapKey requires receiver-shaped raw elements",
	"eq-literal/mapList/unstructured":                         "bug 4: toMapKey requires receiver-shaped raw elements",
	"eq-literal/ifList/typed":                                 "bug 4: toMapKey requires receiver-shaped raw elements",
	"eq-literal/ifList/unstructured":                          "bugs 2+4: escaped keys and literal-shaped elements",
	"eq-literal-concat/ifList/unstructured":                   "bug 4: refValMapKey uses escaped names, literal maps carry raw names",
	"eq-symmetric/mapList/typed-wrapper~unstructured-wrapper": "bug 4: toMapKey requires receiver-shaped raw elements",
	"eq-symmetric/mapList/typed-wrapper~unstructured-concat":  "bugs 4+5: False one way, no-such-overload the other",
	"eq-symmetric/mapList/unstructured-wrapper~typed-concat":  "bugs 4+5: False one way, no-such-overload the other",
	"eq-symmetric/mapList/typed-concat~unstructured-concat":   "bug 5: typedStruct.Equal rejects unstructuredMap elements",

	// Bug 5: typedStruct.Equal accepts only *typedStruct, so comparisons with
	// CEL literal maps and unstructured objects error (or silently miss in
	// Contains). Note (x+[])==literal for typed atomic struct lists passes
	// because cel-go's native list Equal skips non-Bool element verdicts.
	"eq-literal/atomicStructs/typed":                                "bug 5: typedStruct.Equal rejects CEL literal maps",
	"eq-literal-concat/mapList/typed":                               "bug 5: typedStruct.Equal rejects CEL literal maps",
	"eq-literal-concat/ifList/typed":                                "bug 5: typedStruct.Equal rejects CEL literal maps",
	"contains-literal/atomicStructs/typed":                          "bug 5: typedStruct.Equal rejects CEL literal maps",
	"eq-symmetric/atomicStructs/typed-wrapper~unstructured-wrapper": "bug 5: typedStruct.Equal rejects traits.Mapper operands",
	"eq-symmetric/atomicStructs/typed-wrapper~unstructured-concat":  "bug 5: typedStruct.Equal rejects traits.Mapper operands",
	"eq-symmetric/atomicStructs/unstructured-wrapper~typed-concat":  "bug 5: typedStruct.Equal rejects traits.Mapper operands",

	// Iterator exhaustion: Next() past the end should be an error value, but
	// the unstructured listIterator panics and cel-go's native list iterator
	// (concatenation results) returns nil.
	"iter-exhausted/atomicStrings/unstructured/wrapper": "listIterator.Next indexes past the slice end",
	"iter-exhausted/atomicStrings/typed/concat":         "cel-go baseList iterator returns nil past exhaustion",
	"iter-exhausted/atomicStrings/unstructured/concat":  "cel-go baseList iterator returns nil past exhaustion",
}

// lawCheck records a law outcome. Violations of pinned cases are logged;
// passes of pinned cases fail the suite (remove the stale pin); violations of
// unpinned cases fail the suite.
func lawCheck(t *testing.T, caseID string, ok bool, detail string) {
	t.Helper()
	reason, pinned := lawKnownViolations[caseID]
	switch {
	case ok && pinned:
		t.Errorf("%s: pinned as a known violation (%s) but now passes; remove the lawKnownViolations entry", caseID, reason)
	case !ok && pinned:
		t.Logf("%s: known violation (%s): %s", caseID, reason, detail)
	case !ok:
		t.Errorf("%s: %s", caseID, detail)
	}
}

// lawOutcome classifies the result of an operation for verdict comparisons.
func lawOutcome(f func() ref.Val) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = "panic"
		}
	}()
	v := f()
	switch {
	case v == types.True:
		return "true"
	case v == types.False:
		return "false"
	case v == nil:
		return "nil"
	case types.IsError(v):
		return "error"
	default:
		return fmt.Sprintf("val:%v", v.Value())
	}
}

func lawEq(a, b ref.Val) string { return lawOutcome(func() ref.Val { return a.Equal(b) }) }
func lawTrue(o string) bool     { return o == "true" }
func lawEmptyLit() ref.Val      { return types.NewRefValList(types.DefaultTypeAdapter, []ref.Val{}) }

// lawLit builds a CEL value the way compiled CEL literals are represented:
// lists of ref.Vals and maps keyed by ref.Val. (This differs from
// types.DefaultTypeAdapter.NativeToValue(map[string]interface{}), whose
// Value() would round-trip to the raw map and mask representation bugs.)
func lawLit(v any) ref.Val {
	switch t := v.(type) {
	case []any:
		elems := make([]ref.Val, len(t))
		for i, e := range t {
			elems[i] = lawLit(e)
		}
		return types.NewRefValList(types.DefaultTypeAdapter, elems)
	case map[string]any:
		m := make(map[ref.Val]ref.Val, len(t))
		for k, e := range t {
			m[types.String(k)] = lawLit(e)
		}
		return types.NewDynamicMap(types.DefaultTypeAdapter, m)
	default:
		return types.DefaultTypeAdapter.NativeToValue(v)
	}
}

func lawAdd(a ref.Val, b ref.Val) ref.Val {
	return a.(traits.Adder).Add(b)
}

// lawConcat re-wraps a wrapper as its concatenation-result form (w + []).
func lawConcat(w ref.Val) ref.Val { return lawAdd(w, lawEmptyLit()) }

// --- corpus ---

type LawMapEntry struct {
	Key1  string `json:"key1"`
	Key2  string `json:"key2"`
	Value int64  `json:"value"`
}

type LawIfEntry struct {
	If  string `json:"if"`
	Val int64  `json:"v"`
}

type LawWideEntry struct {
	K1    string `json:"k1"`
	K2    string `json:"k2"`
	K3    string `json:"k3"`
	K4    string `json:"k4"`
	Value int64  `json:"value"`
}

type LawObj struct {
	Name string `json:"name"`
	Val  int64  `json:"val"`
}

type lawSubject struct {
	name     string
	listType string // "atomic", "set", "map"
	schema   *spec.Schema

	typedX, typedY, typedZ func() any
	rawX, rawY, rawZ       func() []any

	// Expected x+y result, in order. For map lists these are the value-field
	// values of each element (valueField != ""); otherwise raw elements.
	unionXY    []any
	valueField string
	// For map lists: position and value-field value proving right-bias.
	overlapPos int
	overlapVal int64
}

func lawListSchema(listType string, keys []any, items *spec.Schema) *spec.Schema {
	ext := map[string]interface{}{}
	if listType != "atomic" {
		ext["x-kubernetes-list-type"] = listType
	}
	if listType == "map" {
		ext["x-kubernetes-list-map-keys"] = keys
	}
	return &spec.Schema{
		VendorExtensible: spec.VendorExtensible{Extensions: ext},
		SchemaProps:      spec.SchemaProps{Type: []string{"array"}, Items: &spec.SchemaOrArray{Schema: items}},
	}
}

func lawObjectSchema(props map[string]spec.Schema) *spec.Schema {
	return &spec.Schema{SchemaProps: spec.SchemaProps{Type: []string{"object"}, Properties: props}}
}

var (
	lawMapEntrySchema = lawObjectSchema(map[string]spec.Schema{
		"key1": *stringSchema, "key2": *stringSchema, "value": *int64Schema})
	lawIfEntrySchema = lawObjectSchema(map[string]spec.Schema{
		"if": *stringSchema, "v": *int64Schema})
	lawWideEntrySchema = lawObjectSchema(map[string]spec.Schema{
		"k1": *stringSchema, "k2": *stringSchema, "k3": *stringSchema, "k4": *stringSchema, "value": *int64Schema})
	lawObjSchema = lawObjectSchema(map[string]spec.Schema{
		"name": *stringSchema, "val": *int64Schema})
)

func lawMapEntryRaw(k1, k2 string, v int64) map[string]any {
	return map[string]any{"key1": k1, "key2": k2, "value": v}
}

func lawSubjects() []lawSubject {
	intp := func(v int64) *int64 { return &v }
	return []lawSubject{
		{
			name: "atomicStrings", listType: "atomic",
			schema: lawListSchema("atomic", nil, stringSchema),
			// Spare capacity on the typed slice so in-place appends by Add
			// would be observable (see TestLawOperandImmutability).
			typedX:  func() any { s := make([]string, 3, 8); copy(s, []string{"a", "b", "c"}); return s },
			typedY:  func() any { return []string{"d", "e"} },
			typedZ:  func() any { return []string{"f"} },
			rawX:    func() []any { return []any{"a", "b", "c"} },
			rawY:    func() []any { return []any{"d", "e"} },
			rawZ:    func() []any { return []any{"f"} },
			unionXY: []any{"a", "b", "c", "d", "e"},
		},
		{
			name: "atomicStructs", listType: "atomic",
			schema: lawListSchema("atomic", nil, lawObjSchema),
			typedX: func() any { return []LawObj{{"n1", 1}, {"n2", 2}} },
			typedY: func() any { return []LawObj{{"n3", 3}} },
			typedZ: func() any { return []LawObj{{"n4", 4}} },
			rawX: func() []any {
				return []any{map[string]any{"name": "n1", "val": int64(1)}, map[string]any{"name": "n2", "val": int64(2)}}
			},
			rawY: func() []any { return []any{map[string]any{"name": "n3", "val": int64(3)}} },
			rawZ: func() []any { return []any{map[string]any{"name": "n4", "val": int64(4)}} },
			// Element-level comparisons for struct lists use the value field.
			valueField: "val",
			unionXY:    []any{int64(1), int64(2), int64(3)},
		},
		{
			name: "intSet", listType: "set",
			schema:  lawListSchema("set", nil, int64Schema),
			typedX:  func() any { s := make([]int64, 3, 8); copy(s, []int64{1, 2, 3}); return s },
			typedY:  func() any { return []int64{3, 30} },
			typedZ:  func() any { return []int64{4} },
			rawX:    func() []any { return []any{int64(1), int64(2), int64(3)} },
			rawY:    func() []any { return []any{int64(3), int64(30)} },
			rawZ:    func() []any { return []any{int64(4)} },
			unionXY: []any{int64(1), int64(2), int64(3), int64(30)},
		},
		{
			name: "int32Set", listType: "set",
			schema: lawListSchema("set", nil, int32Schema),
			// Same logical values as different Go int widths: the wrappers
			// must normalize before set-key comparisons.
			typedX:  func() any { return []int32{1, 2, 3} },
			typedY:  func() any { return []int32{3, 30} },
			typedZ:  func() any { return []int32{4} },
			rawX:    func() []any { return []any{int64(1), int64(2), int64(3)} },
			rawY:    func() []any { return []any{int64(3), int64(30)} },
			rawZ:    func() []any { return []any{int64(4)} },
			unionXY: []any{int64(1), int64(2), int64(3), int64(30)},
		},
		{
			name: "ptrSet", listType: "set",
			schema:  lawListSchema("set", nil, int64Schema),
			typedX:  func() any { return []*int64{intp(80), intp(81)} },
			typedY:  func() any { return []*int64{intp(81), intp(82)} },
			typedZ:  func() any { return []*int64{intp(83)} },
			rawX:    func() []any { return []any{int64(80), int64(81)} },
			rawY:    func() []any { return []any{int64(81), int64(82)} },
			rawZ:    func() []any { return []any{int64(83)} },
			unionXY: []any{int64(80), int64(81), int64(82)},
		},
		{
			name: "listSet", listType: "set",
			schema: lawListSchema("set", nil, lawListSchema("atomic", nil, stringSchema)),
			// Set elements that are atomic lists: non-comparable in Go, and
			// "a b"/"c" vs "a"/"b c" guards serialized-key boundary collisions.
			typedX:  func() any { return [][]string{{"a b", "c"}, {"x"}} },
			typedY:  func() any { return [][]string{{"a", "b c"}, {"x"}} },
			typedZ:  func() any { return [][]string{{"q"}} },
			rawX:    func() []any { return []any{[]any{"a b", "c"}, []any{"x"}} },
			rawY:    func() []any { return []any{[]any{"a", "b c"}, []any{"x"}} },
			rawZ:    func() []any { return []any{[]any{"q"}} },
			unionXY: []any{[]any{"a b", "c"}, []any{"x"}, []any{"a", "b c"}},
		},
		{
			name: "mapList", listType: "map",
			schema: lawListSchema("map", []any{"key1", "key2"}, lawMapEntrySchema),
			typedX: func() any {
				s := make([]LawMapEntry, 2, 8)
				copy(s, []LawMapEntry{{"a", "a", 1}, {"b", "b", 2}})
				return s
			},
			typedY: func() any { return []LawMapEntry{{"a", "a", 10}, {"c", "c", 3}} },
			typedZ: func() any { return []LawMapEntry{{"d", "d", 4}} },
			rawX: func() []any {
				return []any{lawMapEntryRaw("a", "a", 1), lawMapEntryRaw("b", "b", 2)}
			},
			rawY: func() []any {
				return []any{lawMapEntryRaw("a", "a", 10), lawMapEntryRaw("c", "c", 3)}
			},
			rawZ:       func() []any { return []any{lawMapEntryRaw("d", "d", 4)} },
			valueField: "value",
			unionXY:    []any{int64(10), int64(2), int64(3)},
			overlapPos: 0, overlapVal: 10,
		},
		{
			name: "ifList", listType: "map",
			schema: lawListSchema("map", []any{"if"}, lawIfEntrySchema),
			// Key prop is a CEL reserved word; identifiers must be escaped
			// (__if__) for CEL access but stay raw in data.
			typedX: func() any { return []LawIfEntry{{"a", 1}, {"b", 2}} },
			typedY: func() any { return []LawIfEntry{{"a", 10}, {"c", 3}} },
			typedZ: func() any { return []LawIfEntry{{"d", 4}} },
			rawX: func() []any {
				return []any{map[string]any{"if": "a", "v": int64(1)}, map[string]any{"if": "b", "v": int64(2)}}
			},
			rawY: func() []any {
				return []any{map[string]any{"if": "a", "v": int64(10)}, map[string]any{"if": "c", "v": int64(3)}}
			},
			rawZ:       func() []any { return []any{map[string]any{"if": "d", "v": int64(4)}} },
			valueField: "v",
			unionXY:    []any{int64(10), int64(2), int64(3)},
			overlapPos: 0, overlapVal: 10,
		},
		{
			name: "wideList", listType: "map",
			schema: lawListSchema("map", []any{"k1", "k2", "k3", "k4"}, lawWideEntrySchema),
			// Four key props exercise the serialized-key fallback; x and y
			// keys differ only by a shifted whitespace boundary and must stay
			// distinct.
			typedX: func() any { return []LawWideEntry{{"a b", "c", "d", "e", 1}} },
			typedY: func() any {
				return []LawWideEntry{{"a", "b c", "d", "e", 2}, {"a b", "c", "d", "e", 10}}
			},
			typedZ: func() any { return []LawWideEntry{{"z", "z", "z", "z", 4}} },
			rawX: func() []any {
				return []any{map[string]any{"k1": "a b", "k2": "c", "k3": "d", "k4": "e", "value": int64(1)}}
			},
			rawY: func() []any {
				return []any{
					map[string]any{"k1": "a", "k2": "b c", "k3": "d", "k4": "e", "value": int64(2)},
					map[string]any{"k1": "a b", "k2": "c", "k3": "d", "k4": "e", "value": int64(10)},
				}
			},
			rawZ: func() []any {
				return []any{map[string]any{"k1": "z", "k2": "z", "k3": "z", "k4": "z", "value": int64(4)}}
			},
			valueField: "value",
			unionXY:    []any{int64(10), int64(2)},
			overlapPos: 0, overlapVal: 10,
		},
	}
}

// lawWrapping is one way of producing a ref.Val for a subject's value.
type lawWrapping struct {
	name string
	x    func(s lawSubject) ref.Val
	y    func(s lawSubject) ref.Val
	z    func(s lawSubject) ref.Val
}

func lawWrapTyped(s lawSubject, v any) ref.Val {
	return common.TypedToVal(v, &openapi.Schema{Schema: s.schema})
}

func lawWrapUnstr(s lawSubject, raw []any) ref.Val {
	conv := make([]interface{}, len(raw))
	for i, e := range raw {
		conv[i] = lawRawToUnstructured(e)
	}
	return common.UnstructuredToVal(conv, &openapi.Schema{Schema: s.schema})
}

func lawRawToUnstructured(v any) interface{} {
	switch t := v.(type) {
	case []any:
		out := make([]interface{}, len(t))
		for i, e := range t {
			out[i] = lawRawToUnstructured(e)
		}
		return out
	case map[string]any:
		out := make(map[string]interface{}, len(t))
		for k, e := range t {
			out[k] = lawRawToUnstructured(e)
		}
		return out
	default:
		return v
	}
}

// lawWrappings returns the schema-aware wrappings. Schemaless is checked
// separately in the atomic laws since it has no set/map semantics.
func lawWrappings() []lawWrapping {
	return []lawWrapping{
		{
			name: "typed",
			x:    func(s lawSubject) ref.Val { return lawWrapTyped(s, s.typedX()) },
			y:    func(s lawSubject) ref.Val { return lawWrapTyped(s, s.typedY()) },
			z:    func(s lawSubject) ref.Val { return lawWrapTyped(s, s.typedZ()) },
		},
		{
			name: "unstructured",
			x:    func(s lawSubject) ref.Val { return lawWrapUnstr(s, s.rawX()) },
			y:    func(s lawSubject) ref.Val { return lawWrapUnstr(s, s.rawY()) },
			z:    func(s lawSubject) ref.Val { return lawWrapUnstr(s, s.rawZ()) },
		},
	}
}

// lawElementsMatch asserts that list r has exactly the expected elements in
// order. For map-list subjects the comparison uses the subject's value field
// (element-to-literal equality is itself under test elsewhere); otherwise
// elements are compared to literals of the expected raw values.
func lawElementsMatch(t *testing.T, caseID string, s lawSubject, r ref.Val, expected []any) {
	t.Helper()
	lister, ok := r.(traits.Lister)
	if !ok {
		lawCheck(t, caseID, false, fmt.Sprintf("result is not a Lister: %v", r))
		return
	}
	if sz, _ := lister.Size().(types.Int); int(sz) != len(expected) {
		lawCheck(t, caseID, false, fmt.Sprintf("size = %d, want %d", sz, len(expected)))
		return
	}
	for i, want := range expected {
		e := lister.Get(types.Int(i))
		if s.valueField != "" {
			got := e.(traits.Indexer).Get(types.String(s.valueField))
			if lawEq(got, lawLit(want)) != "true" {
				lawCheck(t, caseID, false, fmt.Sprintf("element %d %s = %v, want %v", i, s.valueField, got.Value(), want))
				return
			}
		} else {
			if lawEq(e, lawLit(want)) != "true" {
				lawCheck(t, caseID, false, fmt.Sprintf("element %d = %v, want %v", i, e.Value(), want))
				return
			}
		}
	}
	lawCheck(t, caseID, true, "")
}

// TestLawEqualReflexive: w == w for the same instance (twice, so lazily built
// caches are exercised), for independently constructed wrappers, and for
// concatenation results.
func TestLawEqualReflexive(t *testing.T) {
	for _, s := range lawSubjects() {
		for _, w := range lawWrappings() {
			t.Run(s.name+"/"+w.name, func(t *testing.T) {
				caseID := fmt.Sprintf("eq-reflexive/%s/%s", s.name, w.name)
				x := w.x(s)
				fresh := w.x(s)
				concat := lawConcat(w.x(s))
				ok := lawTrue(lawEq(x, x)) &&
					lawTrue(lawEq(x, x)) && // second call hits populated caches
					lawTrue(lawEq(x, fresh)) &&
					lawTrue(lawEq(fresh, x)) &&
					lawTrue(lawEq(concat, concat)) &&
					lawTrue(lawEq(concat, lawConcat(w.x(s))))
				lawCheck(t, caseID, ok, fmt.Sprintf(
					"x==x:%s x==x(again):%s x==fresh:%s fresh==x:%s concat==concat:%s concat==rebuilt:%s",
					lawEq(x, x), lawEq(x, x), lawEq(x, fresh), lawEq(fresh, x),
					lawEq(concat, concat), lawEq(concat, lawConcat(w.x(s)))))
			})
		}
	}
}

// TestLawEqualSymmetric: for the same logical value produced by every
// wrapping (typed/unstructured x wrapper/concat), equality must be true in
// both directions — never a Bool one way and an error the other.
func TestLawEqualSymmetric(t *testing.T) {
	for _, s := range lawSubjects() {
		if s.name == "ifList" || s.name == "int32Set" || s.name == "wideList" {
			// ifList unstructured equality is pinned under eq-reflexive
			// (bug 2); int32Set is covered by intSet plus the cross-impl
			// parity battery; wideList cross-representation equality fails
			// the same way as mapList (pinned there as bugs 4+5).
			continue
		}
		kinds := []struct {
			name string
			val  func() ref.Val
		}{
			{"typed-wrapper", func() ref.Val { return lawWrapTyped(s, s.typedX()) }},
			{"unstructured-wrapper", func() ref.Val { return lawWrapUnstr(s, s.rawX()) }},
			{"typed-concat", func() ref.Val { return lawConcat(lawWrapTyped(s, s.typedX())) }},
			{"unstructured-concat", func() ref.Val { return lawConcat(lawWrapUnstr(s, s.rawX())) }},
		}
		for i := range kinds {
			for j := i + 1; j < len(kinds); j++ {
				a, b := kinds[i], kinds[j]
				t.Run(fmt.Sprintf("%s/%s~%s", s.name, a.name, b.name), func(t *testing.T) {
					caseID := fmt.Sprintf("eq-symmetric/%s/%s~%s", s.name, a.name, b.name)
					av, bv := a.val(), b.val()
					ab, ba := lawEq(av, bv), lawEq(bv, av)
					ok := ab == "true" && ba == "true"
					lawCheck(t, caseID, ok, fmt.Sprintf("a==b:%s b==a:%s (same logical value; want true/true)", ab, ba))
				})
			}
		}
	}
}

// TestLawEqualSizeGate: values of different sizes are False (not an error),
// in both directions.
func TestLawEqualSizeGate(t *testing.T) {
	for _, s := range lawSubjects() {
		for _, w := range lawWrappings() {
			t.Run(s.name+"/"+w.name, func(t *testing.T) {
				caseID := fmt.Sprintf("eq-size-gate/%s/%s", s.name, w.name)
				x, z := w.x(s), w.z(s)
				xz, zx := lawEq(x, z), lawEq(z, x)
				ok := xz == "false" && zx == "false"
				lawCheck(t, caseID, ok, fmt.Sprintf("x==z:%s z==x:%s (different sizes; want false/false)", xz, zx))
			})
		}
	}
}

// TestLawEqualDuplicateOperands: a set/map list is never equal to a list of
// the same size holding duplicates of one of its elements, in either
// direction, whether the duplicate-holding list is a literal or a wrapper.
func TestLawEqualDuplicateOperands(t *testing.T) {
	for _, s := range lawSubjects() {
		if s.listType == "atomic" || len(s.rawX()) < 2 {
			// A single-element list is equal to its own "duplicates".
			continue
		}
		for _, w := range lawWrappings() {
			t.Run(s.name+"/"+w.name, func(t *testing.T) {
				caseID := fmt.Sprintf("eq-duplicates/%s/%s", s.name, w.name)
				raw := s.rawX()
				dupRaw := make([]any, len(raw))
				for i := range dupRaw {
					dupRaw[i] = raw[0]
				}
				x := w.x(s)
				dupLit := lawLit(dupRaw)
				dupWrapped := lawWrapUnstr(s, dupRaw) // invalid data as an operand
				ok := lawEq(x, dupLit) == "false" &&
					lawEq(x, dupWrapped) == "false" &&
					lawEq(dupWrapped, x) == "false"
				lawCheck(t, caseID, ok, fmt.Sprintf(
					"x==dupLit:%s x==dupWrapped:%s dupWrapped==x:%s (want false/false/false)",
					lawEq(x, dupLit), lawEq(x, dupWrapped), lawEq(dupWrapped, x)))
			})
		}
	}
}

// TestLawEqualLiteral: a wrapper equals a CEL literal of the same logical
// value, and concatenating the wrapper with [] must not change that verdict.
func TestLawEqualLiteral(t *testing.T) {
	for _, s := range lawSubjects() {
		if s.name == "wideList" || s.name == "int32Set" || s.name == "ptrSet" {
			continue // representative subjects are enough for the literal axis
		}
		for _, w := range lawWrappings() {
			t.Run(s.name+"/"+w.name, func(t *testing.T) {
				lit := lawLit(s.rawX())
				direct := lawEq(w.x(s), lit)
				lawCheck(t, fmt.Sprintf("eq-literal/%s/%s", s.name, w.name),
					direct == "true", fmt.Sprintf("x==literal(x): %s, want true", direct))
				viaConcat := lawEq(lawConcat(w.x(s)), lit)
				lawCheck(t, fmt.Sprintf("eq-literal-concat/%s/%s", s.name, w.name),
					viaConcat == "true", fmt.Sprintf("(x+[])==literal(x): %s, want true", viaConcat))
			})
		}
	}
}

// TestLawSetAlgebra: union laws for x-kubernetes-list-type=set, for wrapper
// and concat-result receivers.
func TestLawSetAlgebra(t *testing.T) {
	for _, s := range lawSubjects() {
		if s.listType != "set" {
			continue
		}
		for _, w := range lawWrappings() {
			receivers := []struct {
				name string
				x    func() ref.Val
			}{
				{"wrapper", func() ref.Val { return w.x(s) }},
				{"concat", func() ref.Val { return lawConcat(w.x(s)) }},
			}
			for _, recv := range receivers {
				t.Run(s.name+"/"+w.name+"/"+recv.name, func(t *testing.T) {
					id := func(law string) string {
						return fmt.Sprintf("%s/%s/%s/%s", law, s.name, w.name, recv.name)
					}
					x, y := recv.x(), w.y(s)

					// Identity: x + [] == x, checked with both the concat
					// result and the original as receiver.
					r := lawAdd(recv.x(), lawEmptyLit())
					lawCheck(t, id("set-identity"),
						lawTrue(lawEq(r, x)) && lawTrue(lawEq(x, r)),
						fmt.Sprintf("(x+[])==x:%s x==(x+[]):%s", lawEq(r, x), lawEq(x, r)))

					// Idempotence: x + x == x.
					r = lawAdd(recv.x(), w.x(s))
					lawCheck(t, id("set-idempotence"),
						lawTrue(lawEq(r, x)) && lawTrue(lawEq(x, r)),
						fmt.Sprintf("(x+x)==x:%s x==(x+x):%s", lawEq(r, x), lawEq(x, r)))

					// Absorption: (x+y)+y == x+y.
					xy := lawAdd(recv.x(), y)
					lawCheck(t, id("set-absorption"),
						lawTrue(lawEq(lawAdd(lawAdd(recv.x(), w.y(s)), w.y(s)), xy)),
						fmt.Sprintf("((x+y)+y)==(x+y):%s", lawEq(lawAdd(lawAdd(recv.x(), w.y(s)), w.y(s)), xy)))

					// Associativity: (x+y)+z == x+(y+z).
					left := lawAdd(lawAdd(recv.x(), w.y(s)), w.z(s))
					right := lawAdd(recv.x(), lawAdd(w.y(s), w.z(s)))
					lawCheck(t, id("set-associativity"),
						lawTrue(lawEq(left, right)) && lawTrue(lawEq(right, left)),
						fmt.Sprintf("(x+y)+z==x+(y+z):%s reverse:%s", lawEq(left, right), lawEq(right, left)))

					// Commutativity under set equality: x+y == y+x.
					yx := lawAdd(w.y(s), recv.x())
					lawCheck(t, id("set-commutativity"),
						lawTrue(lawEq(xy, yx)) && lawTrue(lawEq(yx, xy)),
						fmt.Sprintf("x+y==y+x:%s reverse:%s", lawEq(xy, yx), lawEq(yx, xy)))

					// Membership distributes over union; z stays out.
					union := lawAdd(recv.x(), w.y(s)).(traits.Container)
					memberOK := true
					detail := ""
					for _, e := range append(append([]any{}, s.rawX()...), s.rawY()...) {
						if o := lawOutcome(func() ref.Val { return union.Contains(lawLit(e)) }); o != "true" {
							memberOK, detail = false, fmt.Sprintf("union does not contain %v: %s", e, o)
							break
						}
					}
					if memberOK {
						if o := lawOutcome(func() ref.Val { return union.Contains(lawLit(s.rawZ()[0])) }); o != "false" {
							memberOK, detail = false, fmt.Sprintf("union contains disjoint element %v: %s", s.rawZ()[0], o)
						}
					}
					lawCheck(t, id("set-membership"), memberOK, detail)

					// Order contract: x positions preserved, y's new elements
					// appended in first-appearance order.
					lawElementsMatch(t, id("set-order"), s, lawAdd(recv.x(), w.y(s)), s.unionXY)
				})
			}

			// Result uniqueness: no two elements of a union are CEL-equal,
			// for wrapper operands and for literal operands (including
			// literals with internal duplicates).
			t.Run(s.name+"/"+w.name+"/uniqueness", func(t *testing.T) {
				operands := map[string]ref.Val{
					"wrapper": w.y(s),
					"literal": lawLit(append(append([]any{}, s.rawY()...), s.rawY()...)),
				}
				for opName, y := range operands {
					caseID := fmt.Sprintf("set-uniqueness/%s/%s/%s", s.name, w.name, opName)
					r := lawAdd(w.x(s), y).(traits.Lister)
					sz, _ := r.Size().(types.Int)
					ok := true
					detail := ""
					for i := types.Int(0); i < sz && ok; i++ {
						for j := i + 1; j < sz && ok; j++ {
							if lawEq(r.Get(i), r.Get(j)) == "true" {
								ok = false
								detail = fmt.Sprintf("elements %d and %d of the union are CEL-equal: %v", i, j, r.Get(i).Value())
							}
						}
					}
					lawCheck(t, caseID, ok, detail)
				}
			})
		}
	}
}

// TestLawMapAlgebra: merge laws for x-kubernetes-list-type=map, for wrapper
// and concat-result receivers.
func TestLawMapAlgebra(t *testing.T) {
	for _, s := range lawSubjects() {
		if s.listType != "map" {
			continue
		}
		for _, w := range lawWrappings() {
			receivers := []struct {
				name string
				x    func() ref.Val
			}{
				{"wrapper", func() ref.Val { return w.x(s) }},
				{"concat", func() ref.Val { return lawConcat(w.x(s)) }},
			}
			for _, recv := range receivers {
				t.Run(s.name+"/"+w.name+"/"+recv.name, func(t *testing.T) {
					id := func(law string) string {
						return fmt.Sprintf("%s/%s/%s/%s", law, s.name, w.name, recv.name)
					}
					x := recv.x()

					// Identity both directions. The wrapper-receiver
					// direction (x == x+[]) dispatches to the source
					// wrapper's Equal; split so bug pins stay precise.
					r := lawAdd(recv.x(), lawEmptyLit())
					lawCheck(t, id("map-identity"),
						lawTrue(lawEq(r, x)),
						fmt.Sprintf("(x+[])==x:%s", lawEq(r, x)))
					if recv.name == "wrapper" {
						lawCheck(t, fmt.Sprintf("map-identity-wrapper-receiver/%s/%s", s.name, w.name),
							lawTrue(lawEq(x, r)),
							fmt.Sprintf("x==(x+[]):%s", lawEq(x, r)))
					}

					// Idempotence: x + x == x.
					r = lawAdd(recv.x(), w.x(s))
					lawCheck(t, id("map-idempotence"),
						lawTrue(lawEq(r, x)),
						fmt.Sprintf("(x+x)==x:%s", lawEq(r, x)))
					if recv.name == "wrapper" {
						lawCheck(t, fmt.Sprintf("map-idempotence-wrapper-receiver/%s/%s", s.name, w.name),
							lawTrue(lawEq(x, r)),
							fmt.Sprintf("x==(x+x):%s", lawEq(x, r)))
					}

					// Associativity of merge.
					left := lawAdd(lawAdd(recv.x(), w.y(s)), w.z(s))
					right := lawAdd(recv.x(), lawAdd(w.y(s), w.z(s)))
					lawCheck(t, id("map-associativity"),
						lawTrue(lawEq(left, right)) && lawTrue(lawEq(right, left)),
						fmt.Sprintf("(x+y)+z==x+(y+z):%s reverse:%s", lawEq(left, right), lawEq(right, left)))

					// Right bias + order: intersecting keys keep x's
					// position with y's value; new keys append in order.
					merged := lawAdd(recv.x(), w.y(s))
					lawElementsMatch(t, id("map-merge-order"), s, merged, s.unionXY)
					got := merged.(traits.Lister).Get(types.Int(s.overlapPos)).(traits.Indexer).Get(types.String(s.valueField))
					lawCheck(t, id("map-right-bias"),
						lawEq(got, lawLit(s.overlapVal)) == "true",
						fmt.Sprintf("merged[%d].%s = %v, want %d", s.overlapPos, s.valueField, got.Value(), s.overlapVal))

					// Every merge result is equal to itself.
					lawCheck(t, id("map-result-reflexive"),
						lawTrue(lawEq(merged, merged)),
						fmt.Sprintf("(x+y)==(x+y) same instance:%s", lawEq(merged, merged)))
				})
			}
		}
	}

	// Last-writer-wins for RHS-internal duplicate keys, keys not in the LHS.
	// (When the key IS in the LHS, LWW works; these must not differ.)
	for _, w := range lawWrappings() {
		t.Run("mapList/"+w.name+"/lww-dup-rhs", func(t *testing.T) {
			var s lawSubject
			for _, cand := range lawSubjects() {
				if cand.name == "mapList" {
					s = cand
				}
			}
			caseID := "map-lww-dup-rhs/mapList/" + w.name
			dup := lawLit([]any{lawMapEntryRaw("N", "N", 1), lawMapEntryRaw("N", "N", 2)})
			collapsed := lawLit([]any{lawMapEntryRaw("N", "N", 2)})
			r := lawAdd(w.x(s), dup).(traits.Lister)
			rc := lawAdd(w.x(s), collapsed)
			sz, _ := r.Size().(types.Int)
			ok := int(sz) == 3 &&
				lawTrue(lawEq(r, rc)) &&
				lawTrue(lawEq(r, r))
			lawCheck(t, caseID, ok, fmt.Sprintf(
				"size(x+[dupN,dupN'])=%d (want 3, last-writer-wins); (x+dups)==(x+last):%s; r==r:%s",
				sz, lawEq(r, rc), lawEq(r, r)))
		})
	}
}

// TestLawAtomicMonoid: atomic list concatenation is a monoid that preserves
// order and never deduplicates; checked for typed, unstructured, and
// schemaless wrappers, with literal and wrapper operands.
func TestLawAtomicMonoid(t *testing.T) {
	for _, s := range lawSubjects() {
		if s.listType != "atomic" {
			continue
		}
		type wrapping struct {
			name string
			x, y func() ref.Val
		}
		wrappings := []wrapping{
			{"typed",
				func() ref.Val { return lawWrapTyped(s, s.typedX()) },
				func() ref.Val { return lawWrapTyped(s, s.typedY()) }},
			{"unstructured",
				func() ref.Val { return lawWrapUnstr(s, s.rawX()) },
				func() ref.Val { return lawWrapUnstr(s, s.rawY()) }},
			{"schemaless",
				func() ref.Val { return common.SchemalessTypedToVal(s.typedX()) },
				func() ref.Val { return common.SchemalessTypedToVal(s.typedY()) }},
		}
		for _, w := range wrappings {
			t.Run(s.name+"/"+w.name, func(t *testing.T) {
				id := func(law string) string { return fmt.Sprintf("%s/%s/%s", law, s.name, w.name) }

				// Identity: x+[] == x == []+x (empty literal on either side).
				xe := lawAdd(w.x(), lawEmptyLit())
				ex := lawAdd(lawEmptyLit(), w.x())
				lawCheck(t, id("atomic-identity"),
					lawTrue(lawEq(xe, w.x())) && lawTrue(lawEq(w.x(), xe)) &&
						lawTrue(lawEq(ex, w.x())),
					fmt.Sprintf("(x+[])==x:%s x==(x+[]):%s ([]+x)==x:%s",
						lawEq(xe, w.x()), lawEq(w.x(), xe), lawEq(ex, w.x())))

				// Associativity with a literal in the mix.
				lit := lawLit(s.rawY())
				left := lawAdd(lawAdd(w.x(), w.y()), lit)
				right := lawAdd(w.x(), lawAdd(w.y(), lit))
				lawCheck(t, id("atomic-associativity"),
					lawTrue(lawEq(left, right)),
					fmt.Sprintf("(x+y)+lit==x+(y+lit):%s", lawEq(left, right)))

				// Size additivity and no deduplication: x+x has 2*len(x).
				xx := lawAdd(w.x(), w.x()).(traits.Sizer)
				wantSz := 2 * len(s.rawX())
				gotSz, _ := xx.Size().(types.Int)
				lawCheck(t, id("atomic-size-additive"),
					int(gotSz) == wantSz,
					fmt.Sprintf("size(x+x)=%d, want %d (atomic lists never deduplicate)", gotSz, wantSz))

				// Order: x+y in exact operand order.
				lawElementsMatch(t, id("atomic-order"), s, lawAdd(w.x(), w.y()), s.unionXY)

				// Non-commutativity witness: x+y != y+x for nonempty x,y with
				// different heads.
				xy := lawAdd(w.x(), w.y())
				yx := lawAdd(w.y(), w.x())
				lawCheck(t, id("atomic-noncommutative"),
					lawEq(xy, yx) == "false",
					fmt.Sprintf("x+y==y+x:%s (want false: concatenation is ordered)", lawEq(xy, yx)))
			})
		}
	}
}
