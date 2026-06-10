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
	"fmt"
	"sort"
	"strings"

	"k8s.io/gengo/v2/types"
	"k8s.io/klog/v2"
)

// knownTagSuffixes guards against silently-ignored tag-name typos: any other
// "+k8s:conversion-gen:<suffix>" found on a scanned type comment is an error.
var knownTagSuffixes = map[string]bool{
	"explicit-from":       true,
	"memory-identical-to": true,
}

// checkMemoryIdentical enforces +k8s:conversion-gen:memory-identical-to tags found on
// types in the loaded packages, using the same equalMemoryTypes instance (after the
// same manual-conversion Skip seeding) that decides unsafe-cast emission, so the gate
// cannot drift from what is generated. It must be called after all manual conversions
// have been discovered and before any generation.
//
// Enforcement and tag validation apply only to tags this invocation owns: those in an
// input package or a tag-declared peer package (ownedPkgs) whose hub is an
// external-types package of an input. Tags in packages loaded only via
// --base-peer-dirs/--extra-peer-dirs (e.g. another repo's types scanned for manual
// conversions) are skipped; the invocation that generates conversions for them
// enforces them.
func checkMemoryIdentical(universe types.Universe, pkgToExternal map[string]string, ownedPkgs map[string]bool, scanPkgs []string, eq equalMemoryTypes, blockers conversionFuncMap, skipUnsafe bool) {
	violations, err := memoryIdenticalViolations(universe, pkgToExternal, ownedPkgs, scanPkgs, eq, blockers, skipUnsafe)
	if err != nil {
		klog.Exitf("memory-identity check: %v", err)
	}
	if len(violations) > 0 {
		klog.Exitf("memory-identity check failed (%d violation(s)):\n\n%s", len(violations), strings.Join(violations, "\n\n"))
	}
}

func memoryIdenticalViolations(universe types.Universe, pkgToExternal map[string]string, ownedPkgs map[string]bool, scanPkgs []string, eq equalMemoryTypes, blockers conversionFuncMap, skipUnsafe bool) ([]string, error) {
	externalPkgs := map[string]bool{}
	for _, ext := range pkgToExternal {
		externalPkgs[ext] = true
	}

	seen := map[string]bool{}
	sortedPkgs := make([]string, 0, len(scanPkgs))
	for _, p := range scanPkgs {
		if !seen[p] {
			seen[p] = true
			sortedPkgs = append(sortedPkgs, p)
		}
	}
	sort.Strings(sortedPkgs)

	var violations []string
	for _, pkgPath := range sortedPkgs {
		pkg := universe[pkgPath]
		if pkg == nil {
			continue
		}
		typeNames := make([]string, 0, len(pkg.Types))
		for n := range pkg.Types {
			typeNames = append(typeNames, n)
		}
		sort.Strings(typeNames)
		owned := ownedPkgs[pkgPath]
		for _, n := range typeNames {
			t := pkg.Types[n]
			comments := append(append([]string{}, t.SecondClosestCommentLines...), t.CommentLines...)
			if err := checkTagSuffixes(t.Name, comments); err != nil {
				if owned {
					return nil, err
				}
				// Another tree's tags are not this invocation's to validate; a
				// newer suffix there must not fail older generators scanning it.
				klog.V(2).Infof("memory-identity check: ignoring tag in non-owned package: %v", err)
			}
			vals, err := extractTagValues(memoryIdenticalTagName, comments)
			if err != nil {
				if owned {
					return nil, fmt.Errorf("%v: %w", t.Name, err)
				}
				continue
			}
			if len(vals) == 0 {
				continue
			}
			if len(vals) > 1 {
				return nil, fmt.Errorf("%v: at most one +%s tag is allowed, found %d", t.Name, memoryIdenticalTagName, len(vals))
			}
			hubPkgPath := vals[0]
			if !externalPkgs[hubPkgPath] {
				if owned {
					return nil, fmt.Errorf("%v: +%s=%s: %q is not the external-types package of any package being generated in this invocation; either the tag value is wrong, or the matching versioned package is missing from the inputs", t.Name, memoryIdenticalTagName, hubPkgPath, hubPkgPath)
				}
				klog.V(2).Infof("memory-identity check: skipping +%s on %v: %q is not an external-types package of this invocation", memoryIdenticalTagName, t.Name, hubPkgPath)
				continue
			}
			if externalPkgs[t.Name.Package] {
				return nil, fmt.Errorf("%v: +%s must be declared on the internal type, not on a type in an external-types package", t.Name, memoryIdenticalTagName)
			}
			if skipUnsafe {
				return nil, fmt.Errorf("--skip-unsafe cannot be used with +%s (declared on %v)", memoryIdenticalTagName, t.Name)
			}
			hubPkg := universe[hubPkgPath]
			if hubPkg == nil || !hubPkg.Has(t.Name.Name) {
				return nil, fmt.Errorf("%v: +%s=%s: no type named %q in %q (renamed or removed?)", t.Name, memoryIdenticalTagName, hubPkgPath, t.Name.Name, hubPkgPath)
			}
			hub := hubPkg.Types[t.Name.Name]
			if !eq.Equal(t, hub) {
				violations = append(violations, layoutViolationMessage(t, hub, hubPkgPath, blockers))
			} else if mismatch := explainNameMismatch(t, hub, t.Name.Name, nil); mismatch != "" {
				violations = append(violations, nameViolationMessage(t, hub, hubPkgPath, mismatch))
			}
		}
	}
	return violations, nil
}

// checkTagSuffixes rejects unrecognized "+k8s:conversion-gen:<suffix>" tags so that a
// misspelled memory-identical-to tag cannot be silently ignored.
func checkTagSuffixes(typeName types.Name, comments []string) error {
	for _, line := range comments {
		// codetags.Extract accepts leading whitespace before the marker; mirror it.
		rest, ok := strings.CutPrefix(strings.TrimLeft(line, " \t"), "+"+tagName+":")
		if !ok {
			continue
		}
		suffix := rest
		if i := strings.IndexAny(rest, "=( "); i >= 0 {
			suffix = rest[:i]
		}
		if !knownTagSuffixes[suffix] {
			return fmt.Errorf("%v: unknown tag +%s:%s", typeName, tagName, suffix)
		}
	}
	return nil
}

func layoutViolationMessage(t, hub *types.Type, hubPkgPath string, blockers conversionFuncMap) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%v is declared memory-identical to %v by +%s=%s, but zero-copy conversion between them is not possible.\n", t.Name, hub.Name, memoryIdenticalTagName, hubPkgPath)
	if d := explainNonIdentical(t, hub, t.Name.Name, blockers); d != "" {
		fmt.Fprintf(&b, "  first divergence: %s\n", d)
	} else {
		fmt.Fprintf(&b, "  (the divergence could not be localized; compare the two types manually)\n")
	}
	fmt.Fprintf(&b, "To fix, make the memory layouts of the two types (and every type they reference) identical: members must match in count, order, and memory layout.\n")
	fmt.Fprintf(&b, "To exempt this pair instead, remove the +%s tag from %v; conversions will then be generated field-by-field.", memoryIdenticalTagName, t.Name)
	return b.String()
}

func nameViolationMessage(t, hub *types.Type, hubPkgPath, mismatch string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%v is declared memory-identical to %v by +%s=%s; the memory layouts match, but member names diverge, which would silently cross-wire values during zero-copy conversion.\n", t.Name, hub.Name, memoryIdenticalTagName, hubPkgPath)
	fmt.Fprintf(&b, "  first mismatch: %s\n", mismatch)
	fmt.Fprintf(&b, "To fix, rename the members so the two types match.\n")
	fmt.Fprintf(&b, "To exempt this pair instead, remove the +%s tag from %v.", memoryIdenticalTagName, t.Name)
	return b.String()
}

// explainNonIdentical returns a description of the first divergence that makes a and b
// non-memory-identical, or "" if none is found. It mirrors equalMemoryTypes.equal —
// including consulting blockers on the declared pair BEFORE unwrapping aliases, exactly
// where cachingEqual consults its Skip-seeded cache — but the enforcement verdict always
// comes from equalMemoryTypes.Equal; this only affects message quality.
func explainNonIdentical(a, b *types.Type, path string, blockers conversionFuncMap) string {
	return explainWalk(a, b, path, blockers, nil)
}

func explainWalk(a, b *types.Type, path string, blockers conversionFuncMap, visited []conversionPair) string {
	// Identity short-circuits before the blocker lookup, mirroring cachingEqual,
	// where a == b wins before the Skip-seeded cache is consulted (this is how a
	// manual self-conversion like metav1 TypeMeta's does not poison containers).
	if a == b {
		return ""
	}
	if fn, ok := blockers[conversionPair{a, b}]; ok {
		return blockerMessage(path, fn)
	}
	if fn, ok := blockers[conversionPair{b, a}]; ok {
		return blockerMessage(path, fn)
	}
	in, out := unwrapAlias(a), unwrapAlias(b)
	if in == out {
		return ""
	}
	if in.Kind != out.Kind {
		return fmt.Sprintf("%s: kind mismatch: %v (%s) vs %v (%s)", path, a.Name, in.Kind, b.Name, out.Kind)
	}
	for _, v := range visited {
		if v.inType == in && v.outType == out {
			return ""
		}
	}
	visited = append(visited, conversionPair{in, out})
	switch in.Kind {
	case types.Struct:
		if len(in.Members) != len(out.Members) {
			return memberCountMessage(path, in, out)
		}
		for i := range in.Members {
			inMember, outMember := in.Members[i], out.Members[i]
			label := inMember.Name
			if inMember.Name != outMember.Name {
				label = inMember.Name + "/" + outMember.Name
			}
			if d := explainWalk(inMember.Type, outMember.Type, path+"."+label, blockers, visited); d != "" {
				return d
			}
		}
		return ""
	case types.Pointer:
		return explainWalk(in.Elem, out.Elem, path, blockers, visited)
	case types.Slice:
		return explainWalk(in.Elem, out.Elem, path+"[*]", blockers, visited)
	case types.Map:
		if d := explainWalk(in.Key, out.Key, path+"[key]", blockers, visited); d != "" {
			return d
		}
		return explainWalk(in.Elem, out.Elem, path+"[value]", blockers, visited)
	case types.Interface:
		return fmt.Sprintf("%s: interface types %v and %v are never considered memory-identical unless they are the same type", path, a.Name, b.Name)
	case types.Builtin:
		if in.Name.Name != out.Name.Name {
			return fmt.Sprintf("%s: builtin type mismatch: %v vs %v", path, in.Name, out.Name)
		}
		return ""
	default:
		return fmt.Sprintf("%s: kind %v is not supported for zero-copy conversion", path, in.Kind)
	}
}

func blockerMessage(path string, fn *types.Type) string {
	return fmt.Sprintf("%s: manual conversion function %v disqualifies zero-copy conversion for this pair and every type containing it; delete it, or tag it +k8s:conversion-fn=copy-only if it is a pure field-for-field copy", path, fn.Name)
}

func memberCountMessage(path string, in, out *types.Type) string {
	inNames := map[string]bool{}
	for _, m := range in.Members {
		inNames[m.Name] = true
	}
	outNames := map[string]bool{}
	for _, m := range out.Members {
		outNames[m.Name] = true
	}
	var onlyIn, onlyOut []string
	for n := range inNames {
		if !outNames[n] {
			onlyIn = append(onlyIn, n)
		}
	}
	for n := range outNames {
		if !inNames[n] {
			onlyOut = append(onlyOut, n)
		}
	}
	sort.Strings(onlyIn)
	sort.Strings(onlyOut)
	msg := fmt.Sprintf("%s: struct member count differs: %v has %d members, %v has %d members", path, in.Name, len(in.Members), out.Name, len(out.Members))
	if len(onlyIn) > 0 {
		msg += fmt.Sprintf(" (members only in %v: %s)", in.Name, strings.Join(onlyIn, ", "))
	}
	if len(onlyOut) > 0 {
		msg += fmt.Sprintf(" (members only in %v: %s)", out.Name, strings.Join(onlyOut, ", "))
	}
	return msg
}

// explainNameMismatch reports the first positionally-matched struct member whose name
// differs between two memory-identical types. equalMemoryTypes ignores member names, so
// a same-typed member swap keeps Equal true while the unsafe cast silently cross-wires
// the values; enforced pairs reject that.
func explainNameMismatch(a, b *types.Type, path string, visited []conversionPair) string {
	in, out := unwrapAlias(a), unwrapAlias(b)
	if in == out {
		return ""
	}
	if in.Kind != out.Kind {
		return ""
	}
	for _, v := range visited {
		if v.inType == in && v.outType == out {
			return ""
		}
	}
	visited = append(visited, conversionPair{in, out})
	switch in.Kind {
	case types.Struct:
		if len(in.Members) != len(out.Members) {
			return ""
		}
		for i := range in.Members {
			inMember, outMember := in.Members[i], out.Members[i]
			if inMember.Name != outMember.Name {
				return fmt.Sprintf("%s: member %d is named %q in %v but %q in %v", path, i, inMember.Name, in.Name, outMember.Name, out.Name)
			}
			if d := explainNameMismatch(inMember.Type, outMember.Type, path+"."+inMember.Name, visited); d != "" {
				return d
			}
		}
		return ""
	case types.Pointer:
		return explainNameMismatch(in.Elem, out.Elem, path, visited)
	case types.Slice:
		return explainNameMismatch(in.Elem, out.Elem, path+"[*]", visited)
	case types.Map:
		if d := explainNameMismatch(in.Key, out.Key, path+"[key]", visited); d != "" {
			return d
		}
		return explainNameMismatch(in.Elem, out.Elem, path+"[value]", visited)
	default:
		return ""
	}
}
