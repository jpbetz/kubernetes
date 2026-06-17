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

package main

import (
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/code-generator/cmd/validation-gen/util"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/types"
	"k8s.io/klog/v2"
)

// genFeatureGate generates the runtime support for +k8s:featureGate into a
// per-package file: in-use detection (which gates have a field set, for
// ratcheting) and field dropping, registered into the scheme. Generated code
// references gate names as strings only -- the apiserver decides whether a gate
// is enabled -- so it has no feature-package dependency.
type genFeatureGate struct {
	genBase
	gatesCache map[*typeNode]gateSet
}

// gateSet holds the feature gates reachable from a type: all gates with a guarded
// leaf, and the subset whose leaf opted into dropping.
type gateSet struct {
	all  sets.Set[string]
	drop sets.Set[string]
}

func NewGenFeatureGate(outputFilename, outputPackage string, rootTypes []*types.Type, discovered *typeDiscoverer, schemeRegistry types.Name) *genFeatureGate {
	return &genFeatureGate{
		genBase:    newGenBase(outputFilename, outputPackage, rootTypes, discovered, schemeRegistry),
		gatesCache: map[*typeNode]gateSet{},
	}
}

// hasFeatureGates reports whether any root type has a feature-gated field, so
// targets can skip attaching the generator for packages with none.
func (g *genFeatureGate) hasFeatureGates() bool {
	for _, rt := range g.rootTypes {
		if tn := g.discovered.typeNodes[rt]; tn != nil && g.gatesForNode(tn).all.Len() > 0 {
			return true
		}
	}
	return false
}

// Init emits RegisterFeatureGate, which registers each root's in-use detection
// and drop functions into the scheme.
func (g *genFeatureGate) Init(c *generator.Context, w io.Writer) error {
	sw := generator.NewSnippetWriter(w, c, "$", "$")

	var roots []*typeNode
	for _, rt := range g.rootTypes {
		if tn := g.discovered.typeNodes[rt]; tn != nil && g.gatesForNode(tn).all.Len() > 0 {
			roots = append(roots, tn)
		}
	}
	if len(roots) == 0 {
		return sw.Error()
	}

	scheme := c.Universe.Type(g.schemeRegistry)
	schemePtr := &types.Type{Kind: types.Pointer, Elem: scheme}
	opType := c.Universe.Type(types.Name{Package: "k8s.io/apimachinery/pkg/api/operation", Name: "Operation"})

	sw.Do("func init() { localSchemeBuilder.Register(RegisterFeatureGate) }\n\n", nil)
	sw.Do("// RegisterFeatureGate adds feature-gate option and field-dropping support to the scheme.\n", nil)
	sw.Do("func RegisterFeatureGate(scheme $.scheme|raw$) error {\n", generator.Args{"scheme": schemePtr})
	for _, tn := range roots {
		t := tn.valueType
		gates := g.gatesForNode(tn)
		targs := generator.Args{"t": t, "name": t.Name.Name, "op": opType}

		sw.Do("scheme.AddFeatureGateFuncs(\n", nil)
		sw.Do("(*$.t|raw$)(nil),\n", targs)
		// Partitions the type's gates into in-use (ratcheted on) and not-in-use
		// (the apiserver keeps those only when their gate is enabled).
		sw.Do("featureGatesInUse_$.name$,\n", targs)
		// Drop function: clears droppable fields whose option is absent from op.
		if gates.drop.Len() == 0 {
			sw.Do("nil,\n", nil)
		} else {
			specDrop, statusDrop := g.dropGatesByScope(tn)
			sw.Do("func(op $.op|raw$, object interface{}) {\n", targs)
			sw.Do("obj := object.(*$.t|raw$)\n", targs)
			sw.Do("switch op.Request.SubresourcePath() {\n", nil)
			if specDrop.Len() > 0 {
				sw.Do("case \"/\":\nDropDisabledFields_$.name$(op, obj)\n", targs)
			}
			if statusDrop.Len() > 0 {
				sw.Do("case \"/status\":\nDropDisabledStatusFields_$.name$(op, obj)\n", targs)
			}
			sw.Do("}\n},\n", nil)
		}
		sw.Do(")\n", nil)
	}
	sw.Do("return nil\n}\n\n", nil)
	return sw.Error()
}

func (g *genFeatureGate) Filter(_ *generator.Context, t *types.Type) bool {
	tn := g.discovered.typeNodes[t]
	return tn != nil && tn.valueType.Kind == types.Struct && g.gatesForNode(tn).all.Len() > 0
}

func (g *genFeatureGate) GenerateType(c *generator.Context, t *types.Type, w io.Writer) error {
	sw := generator.NewSnippetWriter(w, c, "$", "$")
	tn := g.discovered.typeNodes[t]
	gates := g.gatesForNode(tn)

	// One in-use function per type, covering every gate, used for option
	// computation (ratcheting).
	g.emitInUse(tn, tn.fields, sw)

	if slices.Contains(g.rootTypes, t) {
		// The registered entry point: partition this root's gates into in-use (via
		// the walk) and not-in-use (the rest).
		g.emitFeatureGatesInUse(tn, sortedList(gates.all), sw)

		// A root partitions its drops into spec scope (everything but the Status
		// member) and status scope, run at the right point by the registered
		// closure's subresource switch.
		opType := c.Universe.Type(types.Name{Package: "k8s.io/apimachinery/pkg/api/operation", Name: "Operation"})
		specFields, statusFields := partitionRootFields(tn)
		specDrop, statusDrop := g.dropGatesByScope(tn)

		g.emitDropEntry(tn, "DropDisabledFields", "dropDisabled", sortedList(specDrop), opType, sw)
		for _, gate := range sortedList(specDrop) {
			g.emitDropWorker(tn, gate, "dropDisabled", specFields, sw)
		}
		if statusDrop.Len() > 0 {
			g.emitDropEntry(tn, "DropDisabledStatusFields", "dropDisabledStatus", sortedList(statusDrop), opType, sw)
			for _, gate := range sortedList(statusDrop) {
				g.emitDropWorker(tn, gate, "dropDisabledStatus", statusFields, sw)
			}
		}
		return sw.Error()
	}

	// Non-root types get one shared drop worker per drop gate.
	for _, gate := range sortedList(gates.drop) {
		g.emitDropWorker(tn, gate, "dropDisabled", tn.fields, sw)
	}
	return sw.Error()
}

// emitDropEntry emits DropDisabledFields_<Root> / DropDisabledStatusFields_<Root>,
// clearing each gate's fields whose option is absent from op.
func (g *genFeatureGate) emitDropEntry(tn *typeNode, entryPrefix, workerPrefix string, gates []string, opType *types.Type, sw *generator.SnippetWriter) {
	t := tn.valueType
	sw.Do("func $.entry$_$.name$(op $.op|raw$, obj *$.t|raw$) {\n", generator.Args{"entry": entryPrefix, "name": t.Name.Name, "t": t, "op": opType})
	sw.Do("if obj == nil {\nreturn\n}\n", nil)
	for _, gate := range gates {
		sw.Do("if !op.HasOption($.q$) {\n$.worker$_$.g$_$.name$(obj)\n}\n", generator.Args{"q": strconv.Quote(gate), "worker": workerPrefix, "g": gate, "name": t.Name.Name})
	}
	sw.Do("}\n\n", nil)
}

// emitDropWorker emits <prefix>_<gate>_<Type>, clearing the gate's drop leaves in
// the given fields and recursing into sub-types via the shared dropDisabled_*.
func (g *genFeatureGate) emitDropWorker(tn *typeNode, gate, prefix string, fields []*childNode, sw *generator.SnippetWriter) {
	t := tn.valueType
	sw.Do("func $.p$_$.g$_$.name$(obj *$.t|raw$) {\n", generator.Args{"p": prefix, "g": gate, "name": t.Name.Name, "t": t})
	sw.Do("if obj == nil {\nreturn\n}\n", nil)
	for _, ch := range fields {
		if isDropLeaf(ch, gate) {
			g.emitClearLeaf(t, ch, sw)
			continue
		}
		if ch.node != nil && g.gatesForNode(ch.node).drop.Has(gate) {
			g.emitRecurseDrop(t, ch, gate, sw)
		}
	}
	sw.Do("}\n\n", nil)
}

// emitFeatureGatesInUse emits featureGatesInUse_<Root>, the registered entry point.
// It runs the in-use walk and partitions the root's gates into those in use and
// the rest (notInUse). A nil oldObject (create) yields no in-use gates, so every
// gate lands in notInUse.
func (g *genFeatureGate) emitFeatureGatesInUse(tn *typeNode, allGates []string, sw *generator.SnippetWriter) {
	t := tn.valueType
	contains := &types.Type{Name: types.Name{Package: "slices", Name: "Contains"}}
	args := generator.Args{"t": t, "name": t.Name.Name, "gates": insertArgs(allGates), "contains": contains}
	sw.Do("func featureGatesInUse_$.name$(oldObject interface{}) (inUse []string, notInUse []string) {\n", args)
	sw.Do("var obj *$.t|raw$\n", args)
	sw.Do("if oldObject != nil {\nobj = oldObject.(*$.t|raw$)\n}\n", args)
	sw.Do("inUse = inUse_$.name$(obj, nil)\n", args)
	sw.Do("for _, gate := range []string{$.gates$} {\n", args)
	sw.Do("if !$.contains|raw$(inUse, gate) {\nnotInUse = append(notInUse, gate)\n}\n", args)
	sw.Do("}\n", nil)
	sw.Do("return inUse, notInUse\n}\n\n", nil)
}

// emitInUse emits inUse_<Type>, which appends to (and returns) the in-use slice
// every feature gate whose guarded field is set, over all the type's fields. One
// function per type covers every gate (the apiserver calls it once per request).
// The slice is threaded through recursion accumulator-style.
func (g *genFeatureGate) emitInUse(tn *typeNode, fields []*childNode, sw *generator.SnippetWriter) {
	t := tn.valueType
	sw.Do("func inUse_$.name$(obj *$.t|raw$, inUse []string) []string {\n", generator.Args{"name": t.Name.Name, "t": t})
	sw.Do("if obj == nil {\nreturn inUse\n}\n", nil)
	for _, ch := range fields {
		if ds := ch.fieldValidations.FeatureGateSpec; ds != nil {
			g.emitInUseLeaf(ch, ds.Gates, sw)
			continue
		}
		if ch.node != nil && g.gatesForNode(ch.node).all.Len() > 0 {
			g.emitRecurseInUse(ch, sw)
		}
	}
	sw.Do("return inUse\n}\n\n", nil)
}

func (g *genFeatureGate) emitClearLeaf(t *types.Type, ch *childNode, sw *generator.SnippetWriter) {
	switch util.NativeType(ch.childType).Kind {
	case types.Pointer, types.Slice, types.Map:
		sw.Do("obj.$.f$ = nil\n", generator.Args{"f": ch.name})
	case types.Builtin:
		sw.Do("obj.$.f$ = $.zero$\n", generator.Args{"f": ch.name, "zero": builtinZeroLiteral(util.NativeType(ch.childType))})
	default:
		klog.Fatalf("featuregate-gen: %s.%s: drop is not supported on fields of kind %s (supported: pointer/slice/map/builtin)", t.Name.Name, ch.name, ch.childType.Kind)
	}
}

// emitInUseLeaf emits, for a directly gated field, a check that appends the
// field's gates to the in-use slice when the field is set.
func (g *genFeatureGate) emitInUseLeaf(ch *childNode, gates []string, sw *generator.SnippetWriter) {
	nt := util.NativeType(ch.childType)
	insert := generator.Args{"f": ch.name, "gates": insertArgs(gates)}
	switch nt.Kind {
	case types.Pointer:
		sw.Do("if obj.$.f$ != nil {\ninUse = append(inUse, $.gates$)\n}\n", insert)
	case types.Slice, types.Map:
		sw.Do("if len(obj.$.f$) > 0 {\ninUse = append(inUse, $.gates$)\n}\n", insert)
	case types.Builtin:
		if nt.Name.Name == "bool" {
			sw.Do("if obj.$.f$ {\ninUse = append(inUse, $.gates$)\n}\n", insert)
		} else {
			sw.Do("if obj.$.f$ != $.zero$ {\ninUse = append(inUse, $.gates$)\n}\n", generator.Args{"f": ch.name, "gates": insertArgs(gates), "zero": builtinZeroLiteral(nt)})
		}
	}
}

// insertArgs renders gate names as a quoted, comma-separated argument list.
func insertArgs(gates []string) string {
	quoted := make([]string, len(gates))
	for i, gate := range gates {
		quoted[i] = strconv.Quote(gate)
	}
	return strings.Join(quoted, ", ")
}

// emitRecurseDrop descends into a field whose type transitively contains drop
// leaves, via the shared dropDisabled_<gate>_<SubType>.
func (g *genFeatureGate) emitRecurseDrop(t *types.Type, ch *childNode, gate string, sw *generator.SnippetWriter) {
	nt := util.NativeType(ch.childType)
	switch nt.Kind {
	case types.Struct:
		sw.Do("dropDisabled_$.g$_$.elem$(&obj.$.f$)\n", generator.Args{"g": gate, "elem": ch.node.valueType.Name.Name, "f": ch.name})
	case types.Pointer:
		sw.Do("if obj.$.f$ != nil {\ndropDisabled_$.g$_$.elem$(obj.$.f$)\n}\n", generator.Args{"g": gate, "elem": ch.node.valueType.Name.Name, "f": ch.name})
	case types.Slice:
		elem := ch.node.resolveElemNode()
		if elem == nil {
			klog.Fatalf("featuregate-gen: %s.%s: cannot resolve slice element type for gate %s", t.Name.Name, ch.name, gate)
		}
		if util.NativeType(nt.Elem).Kind == types.Pointer {
			sw.Do("for i := range obj.$.f$ {\nif obj.$.f$[i] != nil {\ndropDisabled_$.g$_$.elem$(obj.$.f$[i])\n}\n}\n", generator.Args{"g": gate, "elem": elem.valueType.Name.Name, "f": ch.name})
		} else {
			sw.Do("for i := range obj.$.f$ {\ndropDisabled_$.g$_$.elem$(&obj.$.f$[i])\n}\n", generator.Args{"g": gate, "elem": elem.valueType.Name.Name, "f": ch.name})
		}
	default:
		klog.Fatalf("featuregate-gen: %s.%s: cannot recurse into %s for gate %s (map-of-struct recursion is not yet supported)", t.Name.Name, ch.name, ch.childType.Kind, gate)
	}
}

// emitRecurseInUse descends into a field whose type transitively contains gated
// leaves, accumulating their gates into the in-use slice.
func (g *genFeatureGate) emitRecurseInUse(ch *childNode, sw *generator.SnippetWriter) {
	nt := util.NativeType(ch.childType)
	switch nt.Kind {
	case types.Struct:
		sw.Do("inUse = inUse_$.elem$(&obj.$.f$, inUse)\n", generator.Args{"elem": ch.node.valueType.Name.Name, "f": ch.name})
	case types.Pointer:
		sw.Do("if obj.$.f$ != nil {\ninUse = inUse_$.elem$(obj.$.f$, inUse)\n}\n", generator.Args{"elem": ch.node.valueType.Name.Name, "f": ch.name})
	case types.Slice:
		elem := ch.node.resolveElemNode()
		if util.NativeType(nt.Elem).Kind == types.Pointer {
			sw.Do("for i := range obj.$.f$ {\nif obj.$.f$[i] != nil {\ninUse = inUse_$.elem$(obj.$.f$[i], inUse)\n}\n}\n", generator.Args{"elem": elem.valueType.Name.Name, "f": ch.name})
		} else {
			sw.Do("for i := range obj.$.f$ {\ninUse = inUse_$.elem$(&obj.$.f$[i], inUse)\n}\n", generator.Args{"elem": elem.valueType.Name.Name, "f": ch.name})
		}
	}
}

// partitionRootFields splits a root type's fields into spec scope (everything
// except the field named Status) and status scope.
func partitionRootFields(tn *typeNode) (spec, status []*childNode) {
	for _, ch := range tn.fields {
		if ch.name == "Status" {
			status = append(status, ch)
		} else {
			spec = append(spec, ch)
		}
	}
	return spec, status
}

// dropGatesByScope returns the drop gates reachable from a root's spec fields and
// from its status field, respectively.
func (g *genFeatureGate) dropGatesByScope(tn *typeNode) (spec, status sets.Set[string]) {
	specFields, statusFields := partitionRootFields(tn)
	return g.dropGatesViaFields(specFields), g.dropGatesViaFields(statusFields)
}

func (g *genFeatureGate) dropGatesViaFields(fields []*childNode) sets.Set[string] {
	result := sets.New[string]()
	for _, ch := range fields {
		if ds := ch.fieldValidations.FeatureGateSpec; ds != nil && ds.Drop {
			result.Insert(ds.Gates...)
		}
		if ch.node != nil {
			result.Insert(g.gatesForNode(ch.node).drop.UnsortedList()...)
		}
	}
	return result
}

// gatesForNode returns the feature gates reachable from tn (all guarded gates and
// the droppable subset), walking the discovered graph (alias-safe, cycle-safe).
func (g *genFeatureGate) gatesForNode(tn *typeNode) gateSet {
	return g.gatesForNodeRec(tn, map[*typeNode]bool{})
}

func (g *genFeatureGate) gatesForNodeRec(tn *typeNode, stack map[*typeNode]bool) gateSet {
	if tn == nil {
		return gateSet{all: sets.New[string](), drop: sets.New[string]()}
	}
	if cached, ok := g.gatesCache[tn]; ok {
		return cached
	}
	if stack[tn] {
		return gateSet{all: sets.New[string](), drop: sets.New[string]()} // cycle: don't cache the partial result
	}
	stack[tn] = true
	defer delete(stack, tn)

	result := gateSet{all: sets.New[string](), drop: sets.New[string]()}
	merge := func(child gateSet) {
		result.all.Insert(child.all.UnsortedList()...)
		result.drop.Insert(child.drop.UnsortedList()...)
	}
	switch tn.valueType.Kind {
	case types.Struct:
		for _, ch := range tn.fields {
			if ds := ch.fieldValidations.FeatureGateSpec; ds != nil {
				result.all.Insert(ds.Gates...)
				if ds.Drop {
					result.drop.Insert(ds.Gates...)
				}
			}
			merge(g.gatesForNodeRec(ch.node, stack))
		}
	case types.Slice:
		merge(g.gatesForNodeRec(tn.resolveElemNode(), stack))
	case types.Map:
		merge(g.gatesForNodeRec(tn.resolveElemNode(), stack))
		merge(g.gatesForNodeRec(tn.resolveKeyNode(), stack))
	case types.Alias:
		if tn.underlying != nil {
			merge(g.gatesForNodeRec(tn.underlying.node, stack))
		}
	}
	g.gatesCache[tn] = result
	return result
}

// isDropLeaf reports whether the field is a direct drop leaf for the gate.
func isDropLeaf(ch *childNode, gate string) bool {
	ds := ch.fieldValidations.FeatureGateSpec
	return ds != nil && ds.Drop && slices.Contains(ds.Gates, gate)
}

func builtinZeroLiteral(nt *types.Type) string {
	switch nt.Name.Name {
	case "bool":
		return "false"
	case "string":
		return `""`
	default:
		// All numeric builtins (int*, uint*, float*, byte, rune).
		return "0"
	}
}

func sortedList(s sets.Set[string]) []string {
	out := s.UnsortedList()
	sort.Strings(out)
	return out
}
