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

package generators

import (
	"strings"
	"testing"

	"k8s.io/gengo/v2/types"
)

func newStruct(pkg, name string, members ...types.Member) *types.Type {
	return &types.Type{Name: types.Name{Package: pkg, Name: name}, Kind: types.Struct, Members: members}
}

func newMember(name string, t *types.Type) types.Member {
	return types.Member{Name: name, Type: t}
}

func newPointer(t *types.Type) *types.Type {
	return &types.Type{Name: types.Name{Name: "*" + t.Name.Name}, Kind: types.Pointer, Elem: t}
}

func newSlice(t *types.Type) *types.Type {
	return &types.Type{Name: types.Name{Name: "[]" + t.Name.Name}, Kind: types.Slice, Elem: t}
}

func newMap(k, v *types.Type) *types.Type {
	return &types.Type{Name: types.Name{Name: "map[" + k.Name.Name + "]" + v.Name.Name}, Kind: types.Map, Key: k, Elem: v}
}

func newAlias(pkg, name string, underlying *types.Type) *types.Type {
	return &types.Type{Name: types.Name{Package: pkg, Name: name}, Kind: types.Alias, Underlying: underlying}
}

func newInterface(pkg, name string) *types.Type {
	return &types.Type{Name: types.Name{Package: pkg, Name: name}, Kind: types.Interface}
}

func newConvertFunc(in, out *types.Type) *types.Type {
	return &types.Type{
		Name: types.Name{Package: "example.com/conv", Name: "Convert_a_" + in.Name.Name + "_To_b_" + out.Name.Name},
		Kind: types.DeclarationOf,
	}
}

func TestExplainNonIdentical(t *testing.T) {
	iface1 := newInterface("a", "I")
	iface2 := newInterface("b", "I")

	recursiveA := newStruct("a", "Node")
	recursiveA.Members = []types.Member{newMember("Next", newPointer(recursiveA)), newMember("V", types.Int64)}
	recursiveB := newStruct("b", "Node")
	recursiveB.Members = []types.Member{newMember("Next", newPointer(recursiveB)), newMember("V", types.Int64)}
	recursiveC := newStruct("c", "Node")
	recursiveC.Members = []types.Member{newMember("Next", newPointer(recursiveC)), newMember("V", types.Int32)}

	// One recursive type paired against two mutually-recursive types, only the
	// second of which diverges; an in-type-keyed cycle assumption misses this.
	crossA := newStruct("a", "Ring")
	crossA.Members = []types.Member{newMember("Next", newPointer(crossA)), newMember("V", types.Int64)}
	crossB1 := newStruct("b", "Ring")
	crossB2 := newStruct("b", "Ring2")
	crossB1.Members = []types.Member{newMember("Next", newPointer(crossB2)), newMember("V", types.Int64)}
	crossB2.Members = []types.Member{newMember("Next", newPointer(crossB1)), newMember("V", types.Int64), newMember("Extra", types.String)}

	blockedIn := newStruct("a", "X", newMember("F", types.String))
	blockedOut := newStruct("b", "X", newMember("F", types.String))
	blockerFn := newConvertFunc(blockedIn, blockedOut)

	cases := []struct {
		name       string
		a, b       *types.Type
		blockers   conversionFuncMap
		wantEqual  bool
		wantSubstr string
	}{
		{
			name:      "identical",
			a:         newStruct("a", "T", newMember("A", types.String), newMember("B", types.Int64)),
			b:         newStruct("b", "T", newMember("A", types.String), newMember("B", types.Int64)),
			wantEqual: true,
		},
		{
			name:       "member reorder",
			a:          newStruct("a", "T", newMember("A", types.String), newMember("B", types.Int64)),
			b:          newStruct("b", "T", newMember("B", types.Int64), newMember("A", types.String)),
			wantEqual:  false,
			wantSubstr: "T.A/B",
		},
		{
			name:       "one-sided member add",
			a:          newStruct("a", "T", newMember("A", types.String)),
			b:          newStruct("b", "T", newMember("A", types.String), newMember("B", types.String)),
			wantEqual:  false,
			wantSubstr: "members only in b.T: B",
		},
		{
			name:       "pointer vs value",
			a:          newStruct("a", "T", newMember("E", types.Int64)),
			b:          newStruct("b", "T", newMember("E", newPointer(types.Int64))),
			wantEqual:  false,
			wantSubstr: "T.E: kind mismatch",
		},
		{
			name:       "builtin widen",
			a:          newStruct("a", "T", newMember("N", types.Int32)),
			b:          newStruct("b", "T", newMember("N", types.Int64)),
			wantEqual:  false,
			wantSubstr: "builtin type mismatch",
		},
		{
			name:       "distinct interface members",
			a:          newStruct("a", "T", newMember("I", iface1)),
			b:          newStruct("b", "T", newMember("I", iface2)),
			wantEqual:  false,
			wantSubstr: "interface types",
		},
		{
			name:      "same interface type",
			a:         newStruct("a", "T", newMember("I", iface1)),
			b:         newStruct("b", "T", newMember("I", iface1)),
			wantEqual: true,
		},
		{
			name:       "slice elem divergence",
			a:          newStruct("a", "T", newMember("L", newSlice(types.Int32))),
			b:          newStruct("b", "T", newMember("L", newSlice(types.Int64))),
			wantEqual:  false,
			wantSubstr: "T.L[*]",
		},
		{
			name:       "map value divergence",
			a:          newStruct("a", "T", newMember("M", newMap(types.String, types.Int32))),
			b:          newStruct("b", "T", newMember("M", newMap(types.String, types.Int64))),
			wantEqual:  false,
			wantSubstr: "T.M[value]",
		},
		{
			name:       "manual conversion blocker",
			a:          newStruct("a", "T", newMember("F", blockedIn)),
			b:          newStruct("b", "T", newMember("F", blockedOut)),
			blockers:   conversionFuncMap{{inType: blockedIn, outType: blockedOut}: blockerFn},
			wantEqual:  false,
			wantSubstr: "manual conversion function example.com/conv.Convert_a_X_To_b_X",
		},
		{
			// Skip seeds the cache on the named types from the manual function
			// signature; an alias-typed member bypasses that cache entry in
			// equalMemoryTypes today. The explainer must agree with Equal, so this
			// pins the laundering behavior rather than "fixing" it one-sided.
			name:      "blocker laundered by alias member",
			a:         newStruct("a", "T", newMember("F", newAlias("a", "AX", blockedIn))),
			b:         newStruct("b", "T", newMember("F", blockedOut)),
			blockers:  conversionFuncMap{{inType: blockedIn, outType: blockedOut}: blockerFn},
			wantEqual: true,
		},
		{
			// A manual self-conversion (e.g. metav1 TypeMeta's) Skips the (T, T)
			// pair, but cachingEqual's a == b identity check wins before the cache
			// lookup, so the Skip never poisons containers of T. The explainer must
			// mirror that order.
			name:      "skipped self-pair does not poison container",
			a:         newStruct("a", "T", newMember("F", blockedIn)),
			b:         newStruct("b", "T", newMember("F", blockedIn)),
			blockers:  conversionFuncMap{{inType: blockedIn, outType: blockedIn}: newConvertFunc(blockedIn, blockedIn)},
			wantEqual: true,
		},
		{
			name:      "alias to same underlying",
			a:         newStruct("a", "T", newMember("S", newAlias("a", "S", types.String))),
			b:         newStruct("b", "T", newMember("S", types.String)),
			wantEqual: true,
		},
		{
			name:      "recursive identical",
			a:         recursiveA,
			b:         recursiveB,
			wantEqual: true,
		},
		{
			name:       "recursive diverging",
			a:          recursiveA,
			b:          recursiveC,
			wantEqual:  false,
			wantSubstr: "Node.V",
		},
		{
			name:       "cross-recursive diverging pair",
			a:          crossA,
			b:          crossB1,
			wantEqual:  false,
			wantSubstr: "Ring.Next: struct member count differs",
		},
		{
			name:       "struct vs slice member",
			a:          newStruct("a", "T", newMember("F", blockedIn)),
			b:          newStruct("b", "T", newMember("F", newSlice(blockedOut))),
			wantEqual:  false,
			wantSubstr: "T.F: kind mismatch",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eq := equalMemoryTypes{}
			for pair := range tc.blockers {
				eq.Skip(pair.inType, pair.outType)
			}
			gotEqual := eq.Equal(tc.a, tc.b)
			if gotEqual != tc.wantEqual {
				t.Fatalf("Equal() = %v, want %v", gotEqual, tc.wantEqual)
			}
			explanation := explainNonIdentical(tc.a, tc.b, tc.a.Name.Name, tc.blockers)
			if (explanation == "") != gotEqual {
				t.Fatalf("explainNonIdentical() = %q, disagrees with Equal() = %v", explanation, gotEqual)
			}
			if tc.wantSubstr != "" && !strings.Contains(explanation, tc.wantSubstr) {
				t.Errorf("explainNonIdentical() = %q, want substring %q", explanation, tc.wantSubstr)
			}
		})
	}
}

func TestExplainNameMismatch(t *testing.T) {
	crossA := newStruct("a", "Ring")
	crossA.Members = []types.Member{newMember("Next", newPointer(crossA)), newMember("A", types.String)}
	crossB1 := newStruct("b", "Ring")
	crossB2 := newStruct("b", "Ring2")
	crossB1.Members = []types.Member{newMember("Next", newPointer(crossB2)), newMember("A", types.String)}
	crossB2.Members = []types.Member{newMember("Next", newPointer(crossB1)), newMember("B", types.String)}

	cases := []struct {
		name       string
		a, b       *types.Type
		wantSubstr string
	}{
		{
			name: "matching names",
			a:    newStruct("a", "T", newMember("A", types.String), newMember("B", types.String)),
			b:    newStruct("b", "T", newMember("A", types.String), newMember("B", types.String)),
		},
		{
			name:       "swapped same-typed members",
			a:          newStruct("a", "T", newMember("A", types.String), newMember("B", types.String)),
			b:          newStruct("b", "T", newMember("B", types.String), newMember("A", types.String)),
			wantSubstr: `member 0 is named "A" in a.T but "B" in b.T`,
		},
		{
			name:       "nested mismatch through pointer and slice",
			a:          newStruct("a", "T", newMember("L", newSlice(newPointer(newStruct("a", "U", newMember("X", types.String)))))),
			b:          newStruct("b", "T", newMember("L", newSlice(newPointer(newStruct("b", "U", newMember("Y", types.String)))))),
			wantSubstr: "T.L[*]: member 0",
		},
		{
			name:       "cross-recursive mismatch in the second paired type",
			a:          crossA,
			b:          crossB1,
			wantSubstr: `Ring.Next: member 1 is named "A" in a.Ring but "B" in b.Ring2`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eq := equalMemoryTypes{}
			if !eq.Equal(tc.a, tc.b) {
				t.Fatal("fixture is not memory-identical; name-mismatch cases must keep Equal() true")
			}
			got := explainNameMismatch(tc.a, tc.b, tc.a.Name.Name, nil)
			if (got == "") != (tc.wantSubstr == "") {
				t.Fatalf("explainNameMismatch() = %q, want substring %q", got, tc.wantSubstr)
			}
			if tc.wantSubstr != "" && !strings.Contains(got, tc.wantSubstr) {
				t.Errorf("explainNameMismatch() = %q, want substring %q", got, tc.wantSubstr)
			}
		})
	}
}

func TestMemoryIdenticalViolations(t *testing.T) {
	const (
		internalPkg = "example.com/apis/core"
		hubPkg      = "example.com/api/core/v1"
		inputPkg    = "example.com/apis/core/v1"
	)
	tag := "+k8s:conversion-gen:memory-identical-to=" + hubPkg

	universe := func(internal, hub map[string]*types.Type) types.Universe {
		return types.Universe{
			internalPkg: &types.Package{Path: internalPkg, Types: internal},
			hubPkg:      &types.Package{Path: hubPkg, Types: hub},
		}
	}
	pkgToExternal := map[string]string{inputPkg: hubPkg}
	scanPkgs := []string{internalPkg, hubPkg, internalPkg} // duplicate on purpose

	identicalPod := func(pkg string) *types.Type {
		return newStruct(pkg, "Pod", newMember("A", types.String))
	}

	blockedInternalPod := withComments(identicalPod(internalPkg), tag)
	blockedHubPod := identicalPod(hubPkg)

	cases := []struct {
		name           string
		universe       types.Universe
		blockers       conversionFuncMap
		skipUnsafe     bool
		wantViolations []string
		wantErr        string
	}{
		{
			name: "identical pair passes",
			universe: universe(
				map[string]*types.Type{"Pod": withComments(identicalPod(internalPkg), tag)},
				map[string]*types.Type{"Pod": identicalPod(hubPkg)},
			),
		},
		{
			name: "layout violation",
			universe: universe(
				map[string]*types.Type{"Pod": withComments(newStruct(internalPkg, "Pod", newMember("A", types.String), newMember("B", types.Int64)), tag)},
				map[string]*types.Type{"Pod": identicalPod(hubPkg)},
			),
			wantViolations: []string{"declared memory-identical", "first divergence", "members only in"},
		},
		{
			name: "name mismatch violation",
			universe: universe(
				map[string]*types.Type{"Pod": withComments(newStruct(internalPkg, "Pod", newMember("B", types.String)), tag)},
				map[string]*types.Type{"Pod": identicalPod(hubPkg)},
			),
			wantViolations: []string{"member names diverge", `member 0 is named "B"`},
		},
		{
			name: "tag in detached comment block",
			universe: universe(
				map[string]*types.Type{"Pod": withDetachedComments(newStruct(internalPkg, "Pod", newMember("A", types.String), newMember("B", types.Int64)), tag)},
				map[string]*types.Type{"Pod": identicalPod(hubPkg)},
			),
			wantViolations: []string{"first divergence"},
		},
		{
			name: "violations are sorted by type name",
			universe: universe(
				map[string]*types.Type{
					"Pod":     withComments(newStruct(internalPkg, "Pod", newMember("A", types.Int64)), tag),
					"Binding": withComments(newStruct(internalPkg, "Binding", newMember("A", types.Int64)), tag),
				},
				map[string]*types.Type{
					"Pod":     identicalPod(hubPkg),
					"Binding": newStruct(hubPkg, "Binding", newMember("A", types.String)),
				},
			),
			wantViolations: []string{"core.Binding is declared", "core.Pod is declared"},
		},
		{
			name: "value is not an external package",
			universe: universe(
				map[string]*types.Type{"Pod": withComments(identicalPod(internalPkg), "+k8s:conversion-gen:memory-identical-to=example.com/api/core/v2")},
				map[string]*types.Type{"Pod": identicalPod(hubPkg)},
			),
			wantErr: "not the external-types package",
		},
		{
			name: "missing hub type",
			universe: universe(
				map[string]*types.Type{"Renamed": withComments(newStruct(internalPkg, "Renamed", newMember("A", types.String)), tag)},
				map[string]*types.Type{"Pod": identicalPod(hubPkg)},
			),
			wantErr: `no type named "Renamed"`,
		},
		{
			name: "skip-unsafe is incompatible",
			universe: universe(
				map[string]*types.Type{"Pod": withComments(identicalPod(internalPkg), tag)},
				map[string]*types.Type{"Pod": identicalPod(hubPkg)},
			),
			skipUnsafe: true,
			wantErr:    "--skip-unsafe cannot be used",
		},
		{
			name: "duplicate tag",
			universe: universe(
				map[string]*types.Type{"Pod": withComments(identicalPod(internalPkg), tag, tag)},
				map[string]*types.Type{"Pod": identicalPod(hubPkg)},
			),
			wantErr: "at most one",
		},
		{
			name: "misspelled tag suffix",
			universe: universe(
				map[string]*types.Type{"Pod": withComments(identicalPod(internalPkg), "+k8s:conversion-gen:memory-identicl-to="+hubPkg)},
				map[string]*types.Type{"Pod": identicalPod(hubPkg)},
			),
			wantErr: "unknown tag +k8s:conversion-gen:memory-identicl-to",
		},
		{
			name: "explicit-from suffix is known",
			universe: universe(
				map[string]*types.Type{"Pod": withComments(identicalPod(internalPkg), "+k8s:conversion-gen:explicit-from=net/url.Values")},
				map[string]*types.Type{"Pod": identicalPod(hubPkg)},
			),
		},
		{
			name: "misspelled tag suffix with leading whitespace",
			universe: universe(
				map[string]*types.Type{"Pod": withComments(identicalPod(internalPkg), "  +k8s:conversion-gen:memory-identicl-to="+hubPkg)},
				map[string]*types.Type{"Pod": identicalPod(hubPkg)},
			),
			wantErr: "unknown tag +k8s:conversion-gen:memory-identicl-to",
		},
		{
			name: "tag on a hub type is rejected",
			universe: universe(
				map[string]*types.Type{"Pod": identicalPod(internalPkg)},
				map[string]*types.Type{"Pod": withComments(identicalPod(hubPkg), tag)},
			),
			wantErr: "must be declared on the internal type",
		},
		{
			name: "manual conversion blocker through production Skip wiring",
			universe: universe(
				map[string]*types.Type{"Pod": blockedInternalPod},
				map[string]*types.Type{"Pod": blockedHubPod},
			),
			blockers:       conversionFuncMap{{inType: blockedInternalPod, outType: blockedHubPod}: newConvertFunc(blockedInternalPod, blockedHubPod)},
			wantViolations: []string{"manual conversion function"},
		},
	}

	ownedPkgs := map[string]bool{inputPkg: true, internalPkg: true}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eq := equalMemoryTypes{}
			for pair := range tc.blockers {
				eq.Skip(pair.inType, pair.outType)
			}
			violations, err := memoryIdenticalViolations(tc.universe, pkgToExternal, ownedPkgs, scanPkgs, eq, tc.blockers, tc.skipUnsafe)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(tc.wantViolations) == 0 {
				if len(violations) != 0 {
					t.Fatalf("unexpected violations: %v", violations)
				}
				return
			}
			joined := strings.Join(violations, "\n\n")
			lastIndex := -1
			for _, want := range tc.wantViolations {
				i := strings.Index(joined, want)
				if i < 0 {
					t.Fatalf("violations missing %q:\n%s", want, joined)
				}
				if i < lastIndex {
					t.Errorf("violation %q out of order:\n%s", want, joined)
				}
				lastIndex = i
			}
		})
	}
}

// TestMemoryIdenticalForeignPackages covers the downstream shape: an invocation that
// generates for its own group while scanning another repo's packages (k/k's
// pkg/apis/core via --extra-peer-dirs) must not enforce or validate the foreign tags.
func TestMemoryIdenticalForeignPackages(t *testing.T) {
	const foreignPkg = "other.example.com/apis/core"
	const inputPkg = "example.com/apis/abac/v1beta1"
	universe := types.Universe{
		foreignPkg: &types.Package{Path: foreignPkg, Types: map[string]*types.Type{
			"Pod":    withComments(newStruct(foreignPkg, "Pod", newMember("A", types.String)), "+k8s:conversion-gen:memory-identical-to=other.example.com/api/core/v1"),
			"Future": withComments(newStruct(foreignPkg, "Future", newMember("A", types.String)), "+k8s:conversion-gen:some-future-tag=x"),
		}},
	}
	pkgToExternal := map[string]string{inputPkg: inputPkg}
	ownedPkgs := map[string]bool{inputPkg: true}

	for _, skipUnsafe := range []bool{false, true} {
		violations, err := memoryIdenticalViolations(universe, pkgToExternal, ownedPkgs, []string{foreignPkg}, equalMemoryTypes{}, nil, skipUnsafe)
		if err != nil {
			t.Errorf("skipUnsafe=%v: foreign tags must be skipped, got error: %v", skipUnsafe, err)
		}
		if len(violations) != 0 {
			t.Errorf("skipUnsafe=%v: foreign tags must not be enforced, got: %v", skipUnsafe, violations)
		}
	}
}

func withComments(t *types.Type, comments ...string) *types.Type {
	t.CommentLines = comments
	return t
}

func withDetachedComments(t *types.Type, comments ...string) *types.Type {
	t.SecondClosestCommentLines = comments
	return t
}
