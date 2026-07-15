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

// Container-consistency, immutability, and cross-implementation parity laws
// for the CEL ref.Val wrappers. See values_laws_test.go for the law-checking
// conventions and the lawKnownViolations pinning mechanism.

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/cel/common"
	"k8s.io/apiserver/pkg/cel/openapi"
)

func lawReceivers(s lawSubject, w lawWrapping) []struct {
	name string
	val  func() ref.Val
} {
	return []struct {
		name string
		val  func() ref.Val
	}{
		{"wrapper", func() ref.Val { return w.x(s) }},
		{"concat", func() ref.Val { return lawConcat(w.x(s)) }},
	}
}

// TestLawContainsIffGetEqual: Contains(e) is true exactly when some element
// of the list equals e, for elements taken from a fresh wrapper, for CEL
// literal elements, and for a disjoint non-member.
func TestLawContainsIffGetEqual(t *testing.T) {
	for _, s := range lawSubjects() {
		for _, w := range lawWrappings() {
			for _, recv := range lawReceivers(s, w) {
				t.Run(s.name+"/"+w.name+"/"+recv.name, func(t *testing.T) {
					r := recv.val().(traits.Lister)
					fresh := w.x(s).(traits.Lister)
					for i := range s.rawX() {
						e := fresh.Get(types.Int(i))
						o := lawOutcome(func() ref.Val { return r.Contains(e) })
						lawCheck(t, fmt.Sprintf("contains-member/%s/%s/%s", s.name, w.name, recv.name),
							o == "true", fmt.Sprintf("Contains(x[%d]) = %s, want true", i, o))
					}
					nonMember := w.z(s).(traits.Lister).Get(types.Int(0))
					o := lawOutcome(func() ref.Val { return r.Contains(nonMember) })
					lawCheck(t, fmt.Sprintf("contains-nonmember/%s/%s/%s", s.name, w.name, recv.name),
						o == "false", fmt.Sprintf("Contains(z[0]) = %s, want false", o))
				})
			}
		}
	}

	// Literal elements: membership must not depend on element provenance.
	for _, s := range lawSubjects() {
		switch s.name {
		case "atomicStrings", "atomicStructs", "intSet", "listSet":
		default:
			continue
		}
		for _, w := range lawWrappings() {
			t.Run(s.name+"/"+w.name+"/literal-element", func(t *testing.T) {
				r := w.x(s).(traits.Lister)
				o := lawOutcome(func() ref.Val { return r.Contains(lawLit(s.rawX()[0])) })
				lawCheck(t, fmt.Sprintf("contains-literal/%s/%s", s.name, w.name),
					o == "true", fmt.Sprintf("Contains(literal(x[0])) = %s, want true", o))
			})
		}
	}
}

// TestLawIteratorAgreement: draining Iterator yields exactly Size() non-nil
// values that match Get(0..n-1) in order.
func TestLawIteratorAgreement(t *testing.T) {
	for _, s := range lawSubjects() {
		for _, w := range lawWrappings() {
			for _, recv := range lawReceivers(s, w) {
				t.Run(s.name+"/"+w.name+"/"+recv.name, func(t *testing.T) {
					caseID := fmt.Sprintf("iter-agreement/%s/%s/%s", s.name, w.name, recv.name)
					r := recv.val().(traits.Lister)
					sz, _ := r.Size().(types.Int)
					it := r.(traits.Iterable).Iterator()
					var count int
					ok := true
					detail := ""
					for it.HasNext() == types.True {
						e := it.Next()
						if e == nil {
							ok, detail = false, fmt.Sprintf("iterator yielded nil at %d", count)
							break
						}
						if count >= int(sz) {
							ok, detail = false, fmt.Sprintf("iterator yielded more than Size()=%d values", sz)
							break
						}
						if eq := lawEq(e, r.Get(types.Int(count))); eq != "true" {
							ok, detail = false, fmt.Sprintf("iterator value %d disagrees with Get: %s", count, eq)
							break
						}
						count++
					}
					if ok && count != int(sz) {
						ok, detail = false, fmt.Sprintf("iterator yielded %d values, Size()=%d", count, sz)
					}
					lawCheck(t, caseID, ok, detail)
				})
			}
		}
	}

	// Next() after exhaustion must yield an error value, not panic.
	// One representative subject per wrapping/receiver keeps this focused.
	for _, w := range lawWrappings() {
		var s lawSubject
		for _, cand := range lawSubjects() {
			if cand.name == "atomicStrings" {
				s = cand
			}
		}
		for _, recv := range lawReceivers(s, w) {
			t.Run("exhausted/"+w.name+"/"+recv.name, func(t *testing.T) {
				caseID := fmt.Sprintf("iter-exhausted/atomicStrings/%s/%s", w.name, recv.name)
				it := recv.val().(traits.Iterable).Iterator()
				for it.HasNext() == types.True {
					it.Next()
				}
				o := lawOutcome(func() ref.Val { return it.Next() })
				lawCheck(t, caseID, o == "error",
					fmt.Sprintf("Next() after exhaustion = %s, want an error value", o))
			})
		}
	}
}

// TestLawIndexErrors: out-of-bounds and non-integer indexes produce error
// values, never panics.
func TestLawIndexErrors(t *testing.T) {
	for _, s := range lawSubjects() {
		for _, w := range lawWrappings() {
			for _, recv := range lawReceivers(s, w) {
				t.Run(s.name+"/"+w.name+"/"+recv.name, func(t *testing.T) {
					caseID := fmt.Sprintf("index-errors/%s/%s/%s", s.name, w.name, recv.name)
					r := recv.val().(traits.Lister)
					sz, _ := r.Size().(types.Int)
					oob := lawOutcome(func() ref.Val { return r.Get(sz) })
					neg := lawOutcome(func() ref.Val { return r.Get(types.Int(-1)) })
					str := lawOutcome(func() ref.Val { return r.Get(types.String("a")) })
					ok := oob == "error" && neg == "error" && str == "error"
					lawCheck(t, caseID, ok,
						fmt.Sprintf("Get(size):%s Get(-1):%s Get('a'):%s (want error/error/error)", oob, neg, str))
				})
			}
		}
	}
}

// TestLawValueRoundtrip: Value() of a source wrapper returns the underlying
// Go value: the original type for typed wrappers, the raw data for
// unstructured wrappers.
func TestLawValueRoundtrip(t *testing.T) {
	for _, s := range lawSubjects() {
		t.Run(s.name+"/typed", func(t *testing.T) {
			caseID := "value-roundtrip/" + s.name + "/typed"
			in := s.typedX()
			got := lawWrapTyped(s, in).Value()
			lawCheck(t, caseID, reflect.DeepEqual(got, in),
				fmt.Sprintf("Value() = %T %v, want the wrapped value %T %v", got, got, in, in))
		})
		t.Run(s.name+"/unstructured", func(t *testing.T) {
			caseID := "value-roundtrip/" + s.name + "/unstructured"
			conv := lawRawToUnstructured(append([]any{}, s.rawX()...)).([]interface{})
			got := common.UnstructuredToVal(conv, &openapi.Schema{Schema: s.schema}).Value()
			lawCheck(t, caseID, reflect.DeepEqual(got, conv),
				fmt.Sprintf("Value() = %T %v, want the wrapped data", got, got))
		})
	}
}

// TestLawOperandImmutability: Add never mutates its operands' underlying Go
// data — including spare backing-array capacity, where an in-place append
// would be invisible to length-bounded comparisons — and results of Add do
// not share mutable state with the operand: an earlier result must be
// unchanged after later Adds on the same operand.
func TestLawOperandImmutability(t *testing.T) {
	backing := func(v any) any {
		rv := reflect.ValueOf(v)
		full := rv.Slice(0, rv.Cap())
		cp := reflect.MakeSlice(rv.Type(), full.Len(), full.Len())
		reflect.Copy(cp, full)
		return cp.Interface()
	}
	inspect := func(r ref.Val) {
		lister := r.(traits.Lister)
		sz, _ := lister.Size().(types.Int)
		for i := types.Int(0); i < sz; i++ {
			_ = lister.Get(i).Value()
		}
		for it := lister.(traits.Iterable).Iterator(); it.HasNext() == types.True; {
			_ = it.Next()
		}
		_ = r.Value()
	}
	for _, s := range lawSubjects() {
		switch s.name {
		case "atomicStrings", "intSet", "mapList":
		default:
			continue
		}
		t.Run(s.name+"/typed", func(t *testing.T) {
			caseID := "operand-immutability/" + s.name + "/typed"
			x := s.typedX()
			y := s.typedY()
			snapshot := backing(x)
			snapshotY := backing(y)
			w := lawWrapTyped(s, x)
			r1 := lawAdd(w, lawLit(s.rawZ()))
			r1Snapshot := lawSnapshotList(t, s, r1)
			r2 := lawAdd(w, lawWrapTyped(s, y))
			r3 := lawAdd(lawConcat(w), lawWrapTyped(s, s.typedZ()))
			inspect(r1)
			inspect(r2)
			inspect(r3)
			lawCheck(t, caseID,
				reflect.DeepEqual(backing(x), snapshot) && reflect.DeepEqual(backing(y), snapshotY),
				fmt.Sprintf("backing array changed after Add: x=%v y=%v", backing(x), backing(y)))
			lawCheck(t, caseID+"/result-independence",
				reflect.DeepEqual(lawSnapshotList(t, s, r1), r1Snapshot),
				fmt.Sprintf("x+z changed after later Adds on x: %v, want %v", lawSnapshotList(t, s, r1), r1Snapshot))
		})
		t.Run(s.name+"/unstructured", func(t *testing.T) {
			caseID := "operand-immutability/" + s.name + "/unstructured"
			conv := lawUnstrWithSpareCap(s.rawX())
			snapshot := runtime.DeepCopyJSONValue(conv)
			backingX := backing(conv)
			w := common.UnstructuredToVal(conv, &openapi.Schema{Schema: s.schema})
			convY := lawUnstrWithSpareCap(s.rawY())
			snapshotY := runtime.DeepCopyJSONValue(convY)
			backingY := backing(convY)
			wy := common.UnstructuredToVal(convY, &openapi.Schema{Schema: s.schema})
			r1 := lawAdd(w, wy)
			r1Snapshot := lawSnapshotList(t, s, r1)
			r2 := lawAdd(w, lawLit(s.rawZ()))
			r3 := lawAdd(lawConcat(w), wy)
			inspect(r1)
			inspect(r2)
			inspect(r3)
			lawCheck(t, caseID,
				reflect.DeepEqual(conv, snapshot) && reflect.DeepEqual(convY, snapshotY) &&
					reflect.DeepEqual(backing(conv), backingX) && reflect.DeepEqual(backing(convY), backingY),
				fmt.Sprintf("operand data changed after Add: x=%v y=%v", conv, convY))
			lawCheck(t, caseID+"/result-independence",
				reflect.DeepEqual(lawSnapshotList(t, s, r1), r1Snapshot),
				fmt.Sprintf("x+y changed after later Adds on x: %v, want %v", lawSnapshotList(t, s, r1), r1Snapshot))
		})
	}
}

// lawUnstrWithSpareCap converts raw elements to unstructured form in a slice
// with spare capacity, as append-grown JSON decoding produces: the shape in
// which an in-place append by Add lands inside the operand's backing array.
func lawUnstrWithSpareCap(raw []any) []interface{} {
	conv := make([]interface{}, len(raw), len(raw)+8)
	for i, e := range raw {
		conv[i] = lawRawToUnstructured(e)
	}
	return conv
}

// TestLawConcatReceiverImmutability: computing z2 = z + q where q intersects
// z (so the merge overwrite path executes) must not change any observable of
// z, for z itself a concatenation result.
func TestLawConcatReceiverImmutability(t *testing.T) {
	for _, s := range lawSubjects() {
		switch s.name {
		case "intSet", "mapList":
		default:
			continue
		}
		for _, w := range lawWrappings() {
			t.Run(s.name+"/"+w.name, func(t *testing.T) {
				caseID := "concat-receiver-immutability/" + s.name + "/" + w.name
				z := lawAdd(w.x(s), w.y(s))
				before := lawSnapshotList(t, s, z)
				var q ref.Val
				if s.listType == "map" {
					q = lawLit([]any{lawMapEntryRaw("a", "a", 99)}) // intersects z's first key
				} else {
					q = lawLit([]any{int64(1), int64(99)}) // 1 intersects z
				}
				z2 := lawAdd(z, q)
				after := lawSnapshotList(t, s, z)
				ok := reflect.DeepEqual(before, after)
				lawCheck(t, caseID, ok,
					fmt.Sprintf("z observables changed after z+q: before=%v after=%v", before, after))
				z2Snapshot := lawSnapshotList(t, s, z2)
				_ = lawAdd(z, q)
				lawCheck(t, caseID+"/result-independence",
					reflect.DeepEqual(lawSnapshotList(t, s, z2), z2Snapshot),
					fmt.Sprintf("z+q changed after repeating z+q: %v, want %v", lawSnapshotList(t, s, z2), z2Snapshot))
				if s.listType == "map" {
					got := z2.(traits.Lister).Get(types.Int(0)).(traits.Indexer).Get(types.String(s.valueField))
					lawCheck(t, caseID+"/overwrite-executed", lawEq(got, lawLit(int64(99))) == "true",
						fmt.Sprintf("z2[0].%s = %v, want 99 (proves the overwrite path ran)", s.valueField, got.Value()))
				}
			})
		}
	}
}

// lawSnapshotList captures the observable state of a list: its size and each
// element's comparison value.
func lawSnapshotList(t *testing.T, s lawSubject, r ref.Val) []any {
	t.Helper()
	lister := r.(traits.Lister)
	sz, _ := lister.Size().(types.Int)
	out := []any{int(sz)}
	for i := types.Int(0); i < sz; i++ {
		e := lister.Get(i)
		if s.valueField != "" {
			out = append(out, e.(traits.Indexer).Get(types.String(s.valueField)).Value())
		} else {
			out = append(out, fmt.Sprintf("%v", e.Value()))
		}
	}
	return out
}

// TestLawCacheStability: operations on a wrapper instance whose lazy caches
// were populated by a prior Equal must answer identically to a freshly
// constructed wrapper. (The historical violation: Add polluting the cached
// set used by later Equal/Contains calls.)
func TestLawCacheStability(t *testing.T) {
	for _, s := range lawSubjects() {
		switch s.name {
		case "intSet", "mapList", "listSet":
		default:
			continue
		}
		for _, w := range lawWrappings() {
			t.Run(s.name+"/"+w.name, func(t *testing.T) {
				caseID := "cache-stability/" + s.name + "/" + w.name
				x := w.x(s)
				// Populate lazy caches.
				if o := lawEq(x, w.x(s)); o != "true" {
					lawCheck(t, caseID, false, fmt.Sprintf("precondition x==fresh failed: %s", o))
					return
				}
				// Concatenate through the cached instance.
				r := lawAdd(x, w.z(s))
				// The receiver must be unchanged.
				zElem := w.z(s).(traits.Lister).Get(types.Int(0))
				containsZ := lawOutcome(func() ref.Val { return x.(traits.Container).Contains(zElem) })
				stillEqual := lawEq(x, w.x(s))
				sz, _ := x.(traits.Sizer).Size().(types.Int)
				rSz, _ := r.(traits.Sizer).Size().(types.Int)
				ok := containsZ == "false" && stillEqual == "true" &&
					int(sz) == len(s.rawX()) && int(rSz) == len(s.rawX())+len(s.rawZ())
				lawCheck(t, caseID, ok, fmt.Sprintf(
					"after Equal-then-Add: contains(z[0]):%s (want false) x==fresh:%s (want true) size=%d (want %d) size(x+z)=%d (want %d)",
					containsZ, stillEqual, sz, len(s.rawX()), rSz, len(s.rawX())+len(s.rawZ())))
			})
		}
	}
}

// TestLawCrossImplParity: the same logical value must give the same outcome
// for the same operation under the typed and unstructured wrappers.
func TestLawCrossImplParity(t *testing.T) {
	for _, s := range lawSubjects() {
		t.Run(s.name, func(t *testing.T) {
			ws := lawWrappings()
			typed, unstr := ws[0], ws[1]
			ops := []struct {
				name string
				run  func(w lawWrapping) string
			}{
				{"size", func(w lawWrapping) string {
					return lawOutcome(func() ref.Val { return w.x(s).(traits.Sizer).Size() })
				}},
				{"size-of-union", func(w lawWrapping) string {
					return lawOutcome(func() ref.Val { return lawAdd(w.x(s), w.y(s)).(traits.Sizer).Size() })
				}},
				{"contains-own-element", func(w lawWrapping) string {
					return lawOutcome(func() ref.Val {
						return w.x(s).(traits.Container).Contains(w.x(s).(traits.Lister).Get(types.Int(0)))
					})
				}},
				{"self-equality", func(w lawWrapping) string {
					return lawEq(w.x(s), w.x(s))
				}},
			}
			for _, op := range ops {
				caseID := fmt.Sprintf("cross-impl-parity/%s/%s", s.name, op.name)
				to, uo := op.run(typed), op.run(unstr)
				lawCheck(t, caseID, to == uo,
					fmt.Sprintf("typed:%s unstructured:%s (outcomes must agree)", to, uo))
			}
		})
	}
}
