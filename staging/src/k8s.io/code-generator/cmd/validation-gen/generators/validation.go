/*
Copyright 2024 The Kubernetes Authors.

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
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"reflect"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/code-generator/cmd/validation-gen/args"
	"k8s.io/gengo/v2"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/namer"
	"k8s.io/gengo/v2/types"
	"k8s.io/klog/v2"
)

// These are the comment tags that carry parameters for defaulter generation.
const (
	tagName          = "k8s:validation-gen"
	inputTagName     = "k8s:validation-gen-input"
	maxLengthTagName = "k8s:validation:maxLength"
)

func extractTag(comments []string) []string {
	return gengo.ExtractCommentTags("+", comments)[tagName]
}

func extractInputTag(comments []string) []string {
	return gengo.ExtractCommentTags("+", comments)[inputTagName]
}

func extractValueValidationsTags(comments []string) *validations {
	maxLengths := gengo.ExtractCommentTags("+", comments)[maxLengthTagName]
	var maxLength *int
	for _, l := range maxLengths {
		if i, err := strconv.Atoi(l); err == nil { // TODO: Add error handling
			if maxLength == nil || *maxLength < i {
				maxLength = &i
			}
		}
	}
	return &validations{MaxLength: maxLength}
}

func checkTag(comments []string, require ...string) bool {
	values := gengo.ExtractCommentTags("+", comments)[tagName]
	if len(require) == 0 {
		return len(values) == 1 && values[0] == ""
	}
	return reflect.DeepEqual(values, require)
}

func validationFnNamer() *namer.NameStrategy {
	return &namer.NameStrategy{
		Prefix: "Validate_",
		Join: func(pre string, in []string, post string) string {
			return pre + strings.Join(in, "_") + post
		},
	}
}

// NameSystems returns the name system used by the generators in this package.
func NameSystems() namer.NameSystems {
	return namer.NameSystems{
		"public":             namer.NewPublicNamer(1),
		"raw":                namer.NewRawNamer("", nil),
		"objectvalidationfn": validationFnNamer(),
	}
}

// DefaultNameSystem returns the default name system for ordering the types to be
// processed by the generators in this package.
func DefaultNameSystem() string {
	return "public"
}

// typeValidations holds the declared validation functions for a given type (all typeValidations functions
// are expected to be func(1))
type typeValidations struct {
	// object is the validation function that invokes all validation functions and also
	// invokes all child validations. May be nil if the object validation has not yet been generated.
	object *types.Type
	// validations is the validations
	validations *validations
}

// TODO: decide how to structure all this, maybe a slice of different typeValidations?
type validations struct {
	MaxLength *int
}

func (v *validations) IsEmpty() bool {
	if v == nil {
		return true
	}
	return v.MaxLength == nil
}

type validationFuncMap map[*types.Type]typeValidations

func GetTargets(context *generator.Context, args *args.Args) []generator.Target {
	boilerplate, err := gengo.GoBoilerplate(args.GoHeaderFile, gengo.StdBuildTag, gengo.StdGeneratedBy)
	if err != nil {
		klog.Fatalf("Failed loading boilerplate: %v", err)
	}

	var targets []generator.Target

	buffer := &bytes.Buffer{}
	sw := generator.NewSnippetWriter(buffer, context, "$", "$")

	// First load other "input" packages.  We do this as a single call because
	// it is MUCH faster.
	inputPkgs := make([]string, 0, len(context.Inputs))
	pkgToInput := map[string]string{}
	for _, i := range context.Inputs {
		klog.V(5).Infof("considering pkg %q", i)

		pkg := context.Universe[i]

		// if the types are not in the same package where the validation functions are to be generated
		inputTags := extractInputTag(pkg.Comments)
		if len(inputTags) > 1 {
			panic(fmt.Sprintf("there may only be one input tag, got %#v", inputTags))
		}
		if len(inputTags) == 1 {
			inputPath := inputTags[0]
			if strings.HasPrefix(inputPath, "./") || strings.HasPrefix(inputPath, "../") {
				// this is a relative dir, which will not work under gomodules.
				// join with the local package path, but warn
				klog.Warningf("relative path %s=%s will not work under gomodule mode; use full package path (as used by 'import') instead", inputTagName, inputPath)
				inputPath = path.Join(pkg.Path, inputTags[0])
			}

			klog.V(5).Infof("  input pkg %v", inputPath)
			inputPkgs = append(inputPkgs, inputPath)
			pkgToInput[i] = inputPath
		} else {
			pkgToInput[i] = i
		}
	}

	// Make sure explicit peer-packages are added.
	var peerPkgs []string
	for _, pkg := range args.ExtraPeerDirs {
		// In case someone specifies a peer as a path into vendor, convert
		// it to its "real" package path.
		if i := strings.Index(pkg, "/vendor/"); i != -1 {
			pkg = pkg[i+len("/vendor/"):]
		}
		peerPkgs = append(peerPkgs, pkg)
	}
	if expanded, err := context.FindPackages(peerPkgs...); err != nil {
		klog.Fatalf("cannot find peer packages: %v", err)
	} else {
		peerPkgs = expanded // now in fully canonical form
	}
	inputPkgs = append(inputPkgs, peerPkgs...)

	if len(inputPkgs) > 0 {
		if _, err := context.LoadPackages(inputPkgs...); err != nil {
			klog.Fatalf("cannot load packages: %v", err)
		}
	}
	// update context.Order to the latest context.Universe
	orderer := namer.Orderer{Namer: namer.NewPublicNamer(1)}
	context.Order = orderer.OrderUniverse(context.Universe)

	for _, i := range context.Inputs {
		pkg := context.Universe[i]

		// typesPkg is where the types that need validation are defined.
		// Sometimes it is different from pkg. For example, kubernetes core/v1
		// types are defined in k8s.io/api/core/v1, while the pkg which holds
		// defaulter code is at k/k/pkg/api/v1.
		typesPkg := pkg

		typesWith := extractTag(pkg.Comments)
		shouldCreateObjectValidationFn := func(t *types.Type) bool {
			// opt-out
			if checkTag(t.SecondClosestCommentLines, "false") {
				return false
			}
			// opt-in
			if checkTag(t.SecondClosestCommentLines, "true") {
				return true
			}
			// For every k8s:validation-gen tag at the package level, interpret the value as a
			// field name (like TypeMeta, ListMeta, ObjectMeta) and trigger validation generation
			// for any type with any of the matching field names. Provides a more useful package
			// level validation than global (because we only need typeValidations on a subset of objects -
			// usually those with TypeMeta).
			if t.Kind == types.Struct && len(typesWith) > 0 {
				for _, field := range t.Members {
					for _, s := range typesWith {
						if field.Name == s {
							return true
						}
					}
				}
			}
			return false
		}

		// Find the right input pkg, which might not be this one.
		inputPath := pkgToInput[i]
		typesPkg = context.Universe[inputPath]

		newValidations := validationFuncMap{}
		for _, t := range typesPkg.Types {
			if !shouldCreateObjectValidationFn(t) {
				continue
			}
			if namer.IsPrivateGoName(t.Name.Name) {
				// We won't be able to validate to a private type.
				klog.V(5).Infof("  found a type %v, but it is a private name", t)
				continue
			}

			// create a synthetic type we can use during generation
			newValidations[t] = typeValidations{}
		}

		// This only generate validations for objects that actually have defined validations.
		// This prevents empty validations from being registered.
		for {
			promoted := 0
			for t, d := range newValidations {
				if d.object != nil {
					continue
				}
				if node := newCallTreeForType(newValidations).build(t, true); node != nil {
					args := validationArgsFromType(t)
					sw.Do("$.inType|objectdefaultfn$", args)
					newValidations[t] = typeValidations{
						object: &types.Type{
							Name: types.Name{
								Package: pkg.Path,
								Name:    buffer.String(),
							},
							Kind: types.Func,
						},
					}
					buffer.Reset()
					promoted++
				}
			}
			if promoted != 0 {
				continue
			}

			// prune any types that were not used
			for t, d := range newValidations {
				if d.object == nil {
					klog.V(6).Infof("did not generate typeValidations for %s because no child typeValidations were registered", t.Name)
					delete(newValidations, t)
				}
			}
			break
		}

		if len(newValidations) == 0 {
			klog.V(5).Infof("no typeValidations in package %s", pkg.Name)
		}

		targets = append(targets,
			&generator.SimpleTarget{
				PkgName:       path.Base(pkg.Path),
				PkgPath:       pkg.Path,
				PkgDir:        pkg.Dir, // output pkg is the same as the input
				HeaderComment: boilerplate,

				FilterFunc: func(c *generator.Context, t *types.Type) bool {
					return t.Name.Package == typesPkg.Path
				},

				GeneratorsFunc: func(c *generator.Context) (generators []generator.Generator) {
					return []generator.Generator{
						NewGenValidations(args.OutputFile, typesPkg.Path, pkg.Path, newValidations, peerPkgs),
					}
				},
			})
	}
	return targets
}

// callTreeForType contains fields necessary to build a tree for types.
type callTreeForType struct {
	newValidations         validationFuncMap
	currentlyBuildingTypes map[*types.Type]bool
}

func newCallTreeForType(newValidations validationFuncMap) *callTreeForType {
	return &callTreeForType{
		newValidations:         newValidations,
		currentlyBuildingTypes: make(map[*types.Type]bool),
	}
}

// resolveType follows pointers and aliases of `t` until reaching the first
// non-pointer type in `t's` hierarchy
func resolveTypeAndDepth(t *types.Type) (*types.Type, int) {
	var prev *types.Type
	depth := 0
	for prev != t {
		prev = t
		if t.Kind == types.Alias {
			t = t.Underlying
		} else if t.Kind == types.Pointer {
			t = t.Elem
			depth += 1
		}
	}
	return t, depth
}

// getNestedValidations returns the first validation when resolving alias types
func getNestedValidations(t *types.Type) *validations {
	var prev *types.Type
	for prev != t {
		prev = t
		valueValidations := extractValueValidationsTags(t.CommentLines)
		if !valueValidations.IsEmpty() {
			return valueValidations
		}
		if t.Kind == types.Alias {
			t = t.Underlying
		} else if t.Kind == types.Pointer {
			t = t.Elem
		}
	}
	return &validations{}
}

func populateValidations(node *callNode, t *types.Type, tags string, commentLines []string, commentPackage string) *callNode {
	valueValidations := extractValueValidationsTags(commentLines)

	baseT, depth := resolveTypeAndDepth(t)
	if depth > 0 && valueValidations.IsEmpty() {
		valueValidations = getNestedValidations(t)
	}

	if valueValidations.IsEmpty() {
		return node
	}

	// callNodes are not automatically generated for primitive types. Generate one if the callNode does not exist
	if node == nil {
		node = &callNode{}
	}

	node.validationIsPrimitive = baseT.IsPrimitive()
	node.validationType = baseT
	node.validationTopLevelType = t

	node.validations = valueValidations
	return node
}

// build creates a tree of paths to fields (based on how they would be accessed in Go - pointer, elem,
// slice, or key) and the functions that should be invoked on each field. An in-order traversal of the resulting tree
// can be used to generate a Go function that invokes each nested function on the appropriate type. The return
// value may be nil if there are no functions to call on type or the type is a primitive (Validations can only be
// invoked on structs today). When root is true this function will not use a newValidations should
// contain all validations.
func (c *callTreeForType) build(t *types.Type, root bool) *callNode {
	parent := &callNode{}

	if _, exists := c.newValidations[t]; !root && exists {
		return nil
	}

	if root {
		// the root node is always a pointer
		parent.elem = true
	}

	// if the type already exists, don't build the tree for it and don't generate anything.
	// This is used to avoid recursion for nested recursive types.
	if c.currentlyBuildingTypes[t] {
		return nil
	}
	// if type doesn't exist, mark it as existing
	c.currentlyBuildingTypes[t] = true

	defer func() {
		// The type will now acts as a parent, not a nested recursive type.
		// We can now build the tree for it safely.
		c.currentlyBuildingTypes[t] = false
	}()

	switch t.Kind {
	case types.Pointer:
		if child := c.build(t.Elem, false); child != nil {
			child.elem = true
			parent.validationType = t
			parent.children = append(parent.children, *child)
		}
	case types.Slice, types.Array:
		if child := c.build(t.Elem, false); child != nil {
			child.index = true
			if t.Elem.Kind == types.Pointer {
				child.elem = true
			}
			parent.validationType = t
			parent.children = append(parent.children, *child)
		} else if member := populateValidations(nil, t.Elem, "", t.Elem.CommentLines, t.Elem.Name.Package); member != nil {
			member.index = true
			parent.validationType = t
			parent.children = append(parent.children, *member)
		}
	case types.Map:
		if child := c.build(t.Elem, false); child != nil {
			child.key = true
			parent.validationType = t
			parent.children = append(parent.children, *child)
		} else if member := populateValidations(nil, t.Elem, "", t.Elem.CommentLines, t.Elem.Name.Package); member != nil {
			member.key = true
			parent.validationType = t
			parent.children = append(parent.children, *member)
		}

	case types.Struct:
		for _, field := range t.Members {
			name := field.Name
			if len(name) == 0 {
				if field.Type.Kind == types.Pointer {
					name = field.Type.Elem.Name.Name
				} else {
					name = field.Type.Name.Name
				}
			}
			if child := c.build(field.Type, false); child != nil {
				child.field = name
				populateValidations(child, field.Type, field.Tags, field.CommentLines, field.Type.Name.Package)
				parent.validationType = t
				parent.children = append(parent.children, *child)
			} else if member := populateValidations(nil, field.Type, field.Tags, field.CommentLines, t.Name.Package); member != nil {
				member.field = name
				parent.validationType = t
				parent.children = append(parent.children, *member)
			}
		}
	case types.Alias:
		if child := c.build(t.Underlying, false); child != nil {
			parent.children = append(parent.children, *child)
		}
	}
	if len(parent.children) == 0 && parent.validations.IsEmpty() {
		return nil
	}
	return parent
}

const (
	runtimePackagePath = "k8s.io/apimachinery/pkg/runtime"
)

// genValidations produces a file with a autogenerated conversions.
type genValidations struct {
	generator.GoGenerator
	typesPackage   string
	outputPackage  string
	peerPackages   []string
	newValidations validationFuncMap
	imports        namer.ImportTracker
	typesForInit   []*types.Type
}

func NewGenValidations(outputFilename, typesPackage, outputPackage string, newValidations validationFuncMap, peerPkgs []string) generator.Generator {
	return &genValidations{
		GoGenerator: generator.GoGenerator{
			OutputFilename: outputFilename,
		},
		typesPackage:   typesPackage,
		outputPackage:  outputPackage,
		peerPackages:   peerPkgs,
		newValidations: newValidations,
		imports:        generator.NewImportTrackerForPackage(outputPackage),
		typesForInit:   make([]*types.Type, 0),
	}
}

func (g *genValidations) Namers(c *generator.Context) namer.NameSystems {
	// Have the raw namer for this file track what it imports.
	return namer.NameSystems{
		"raw": namer.NewRawNamer(g.outputPackage, g.imports),
	}
}

func (g *genValidations) isOtherPackage(pkg string) bool {
	if pkg == g.outputPackage {
		return false
	}
	if strings.HasSuffix(pkg, `"`+g.outputPackage+`"`) {
		return false
	}
	return true
}

func (g *genValidations) Filter(c *generator.Context, t *types.Type) bool {
	validations, ok := g.newValidations[t]
	if !ok || validations.object == nil {
		return false
	}
	g.typesForInit = append(g.typesForInit, t)
	return true
}

func (g *genValidations) Imports(c *generator.Context) (imports []string) {
	var importLines []string
	for _, singleImport := range g.imports.ImportLines() {
		if g.isOtherPackage(singleImport) {
			importLines = append(importLines, singleImport)
		}
	}
	return importLines
}

func (g *genValidations) Init(c *generator.Context, w io.Writer) error {
	//sw := generator.NewSnippetWriter(w, c, "$", "$")
	//
	//scheme := c.Universe.Type(types.Name{Package: runtimePackagePath, Name: "Scheme"})
	//schemePtr := &types.Type{
	//	Kind: types.Pointer,
	//	Elem: scheme,
	//}
	//sw.Do("// RegisterValidations adds validation functions to the given scheme.\n", nil)
	//sw.Do("// Public to allow building arbitrary schemes.\n", nil)
	//sw.Do("// All generated validaitons are covering - they call all nested typeValidations.\n", nil)
	//sw.Do("func RegisterValidations(scheme $.|raw$) error {\n", schemePtr)
	//for _, t := range g.typesForInit {
	//	args := validationArgsFromType(t)
	//	sw.Do("// scheme.AddTypeValidationFunc(&$.inType|raw${}, func(obj interface{}) { $.inType|objectvalidationfn$(obj.(*$.inType|raw$)) })\n", args)
	//}
	//sw.Do("return nil\n", nil)
	//sw.Do("}\n\n", nil)
	//return sw.Error()
	return nil
}

func (g *genValidations) GenerateType(c *generator.Context, t *types.Type, w io.Writer) error {
	if _, ok := g.newValidations[t]; !ok {
		return nil
	}

	klog.V(5).Infof("generating for type %v", t)

	callTree := newCallTreeForType(g.newValidations).build(t, true)
	if callTree == nil {
		klog.V(5).Infof("  no validations defined")
		return nil
	}
	var errs []error
	visited := sets.New[*types.Type]()
	callTree.VisitInOrder(func(ancestors []*callNode, current *callNode) {
		if visited.Has(current.validationType) {
			return
		}
		visited.Insert(current.validationType)

		sw := generator.NewSnippetWriter(w, c, "$", "$")
		if current.validationType != nil && current.validationType.Kind == types.Struct {
			g.generateValidations(c, current.validationType, current, sw)
			if err := sw.Error(); err != nil {
				errs = append(errs, err)
			}
		}
	})
	return errors.Join(errs...)
}

func validationArgsFromType(inType *types.Type) generator.Args {
	return generator.Args{
		"inType": inType,
	}
}

func (g *genValidations) generateValidations(c *generator.Context, inType *types.Type, callTree *callNode, sw *generator.SnippetWriter) {
	validationArgsFromType(inType)
	args := generator.Args{
		"inType":    inType,
		"errorList": c.Universe.Type(types.Name{Package: "k8s.io/apimachinery/pkg/util/validation/field", Name: "ErrorList"}),
		"fieldPath": c.Universe.Type(types.Name{Package: "k8s.io/apimachinery/pkg/util/validation/field", Name: "Path"}),
	}

	sw.Do("func $.inType|objectvalidationfn$(in *$.inType|raw$, fldPath *$.fieldPath|raw$) (errs $.errorList|raw$) {\n", args)
	callTree.WriteMethod(c, "in", 0, nil, sw)
	sw.Do("return errs\n", nil)
	sw.Do("}\n\n", nil)
}

// callNode represents an entry in a tree of Go type accessors - the path from the root to a leaf represents
// how in Go code an access would be performed. For example, if a validation function exists on a container
// lifecycle hook, to invoke that defaulter correctly would require this Go code:
//
//	for i := range pod.Spec.Containers {
//	  o := &pod.Spec.Containers[i]
//	  if o.LifecycleHook != nil {
//	    errs = append(errs, Validate_LifecycleHook(o.LifecycleHook, fieldPath)...)
//	  }
//	}
//
// That would be represented by a call tree like:
//
//	callNode
//	  field: "Spec"
//	  children:
//	  - field: "Containers"
//	    children:
//	    - index: true
//	      children:
//	      - field: "LifecycleHook"
//	        elem: true
//	        call:
//	        - Validate_LifecycleHook
//
// which we can traverse to build that Go struct (you must call the field Spec, then Containers, then range over
// that field, then check whether the LifecycleHook field is nil, before calling Validate_LifecycleHook on
// the pointer to that field).
type callNode struct {
	// field is the name of the Go member to access
	field string
	// key is true if this is a map and we must range over the key and values
	key bool
	// index is true if this is a slice and we must range over the slice values
	index bool
	// elem is true if the previous elements refer to a pointer (typically just field)
	elem bool

	// children is the child call nodes that must also be traversed
	children []callNode

	// validations is the validations for the node
	validations *validations

	// validationIsPrimitive tracks if the field is a primitive.
	validationIsPrimitive bool

	// validationType is the transitive underlying/element type of the node.
	// The provided default value literal or reference is expected to be
	// convertible to this type.
	//
	// e.g:
	//	node type = *string 			-> 	defaultType = string
	//	node type = StringPointerAlias 	-> 	defaultType = string
	// Only populated if validationIsPrimitive is true
	validationType *types.Type

	// validationTopLevelType is the final type the value should resolve to
	// This is in constrast with default type, which resolves aliases and pointers.
	validationTopLevelType *types.Type
}

// CallNodeVisitorFunc is a function for visiting a call tree. ancestors is the list of all parents
// of this node to the root of the tree - will be empty at the root.
type CallNodeVisitorFunc func(ancestors []*callNode, node *callNode)

func (n *callNode) VisitInOrder(fn CallNodeVisitorFunc) {
	n.visitInOrder(nil, fn)
}

func (n *callNode) visitInOrder(ancestors []*callNode, fn CallNodeVisitorFunc) {
	fn(ancestors, n)
	ancestors = append(ancestors, n)
	for i := range n.children {
		n.children[i].visitInOrder(ancestors, fn)
	}
}

var (
	indexVariables = "ijklmnop"
	localVariables = "abcdefgh"
)

// varsForDepth creates temporary variables guaranteed to be unique within lexical Go scopes
// of this depth in a function. It uses canonical Go loop variables for the first 7 levels
// and then resorts to uglier prefixes.
func varsForDepth(depth int) (index, local string) {
	if depth > len(indexVariables) {
		index = fmt.Sprintf("i%d", depth)
	} else {
		index = indexVariables[depth : depth+1]
	}
	if depth > len(localVariables) {
		local = fmt.Sprintf("local%d", depth)
	} else {
		local = localVariables[depth : depth+1]
	}
	return
}

// writeCalls generates a list of function calls based on the calls field for the provided variable
// name and pointer.
func (n *callNode) writeCall(varName string, isVarPointer bool, sw *generator.SnippetWriter) {
	accessor := varName
	if !isVarPointer {
		accessor = "&" + accessor
	}
	if n.validationType != nil && !n.validationIsPrimitive {
		sw.Do("errs = append(errs, $.fn|objectvalidationfn$($.var$, fldPath)...)\n", generator.Args{
			"fn":  n.validationType,
			"var": accessor,
		})
	}
}

func (n *callNode) writeValidations(c *generator.Context, varName string, index string, isVarPointer bool, sw *generator.SnippetWriter) {
	if n.validations.IsEmpty() {
		return
	}
	args := generator.Args{
		"typeValidations": n.validations,
		"varName":         varName,
		"index":           index,
		"varTopType":      n.validationTopLevelType,
		"invalid":         c.Universe.Type(types.Name{Package: "k8s.io/apimachinery/pkg/util/validation/field", Name: "Invalid"}),
	}

	if n.validations.MaxLength != nil {
		// If default value is a literal then it can be assigned via var stmt
		sw.Do("if len($.varName$) > $.typeValidations.MaxLength$ { errs = append(errs, $.invalid|raw$(fldPath.Child(\"reference\"), $.varName$, \"must not be longer than 128 characters\"))}\n", args)
	}
}

// WriteMethod performs an in-order traversal of the calltree, generating loops and if blocks as necessary
// to correctly turn the call tree into a method body that invokes all calls on all child nodes of the call tree.
// Depth is used to generate local variables at the proper depth.
func (n *callNode) WriteMethod(c *generator.Context, varName string, depth int, ancestors []*callNode, sw *generator.SnippetWriter) {
	// if len(n.call) > 0 {
	// 	sw.Do(fmt.Sprintf("// %s\n", callPath(append(ancestors, n)).String()), nil)
	// }

	if depth > 0 && len(n.field) > 0 { // TODO: How to check for root?
		varName = varName + "." + n.field
	}

	index, local := varsForDepth(depth)
	vars := generator.Args{
		"index": index,
		"local": local,
		"var":   varName,
	}

	isPointer := n.elem && !n.index
	if isPointer && len(ancestors) > 0 {
		sw.Do("if $.var$ != nil {\n", vars)
	}

	switch {
	case n.index:
		sw.Do("for $.index$ := range $.var$ {\n", vars)
		if n.elem {
			sw.Do("$.local$ := $.var$[$.index$]\n", vars)
		} else {
			sw.Do("$.local$ := &$.var$[$.index$]\n", vars)
		}

		n.writeValidations(c, varName, index, isPointer, sw)
		for _, child := range n.children {
			_, childLocal := varsForDepth(depth + 1)
			child.writeCall(childLocal, isPointer, sw)
			//n.children[i].WriteMethod(local, depth+1, append(ancestors, n), sw)
		}
		sw.Do("}\n", nil)
	case n.key:
		if !n.validations.IsEmpty() {
			// Map keys are typed and cannot share the same index variable as arrays and other maps
			index = index + "_" + ancestors[len(ancestors)-1].field
			vars["index"] = index
			sw.Do("for $.index$ := range $.var$ {\n", vars)
			n.writeValidations(c, varName, index, isPointer, sw)
			sw.Do("}\n", nil)
		}
	default:
		n.writeValidations(c, varName, index, isPointer, sw)
		for _, child := range n.children {
			childVarName := varName + "." + child.field
			isPointer := child.elem && !child.index
			child.writeCall(childVarName, isPointer, sw)
			child.writeValidations(c, childVarName, index, isPointer, sw)
			//child.WriteMethod(varName, depth, append(ancestors, n), sw)
		}
	}

	if isPointer && len(ancestors) > 0 {
		sw.Do("}\n", nil)
	}
}

type callPath []*callNode

// String prints a representation of a callPath that roughly approximates what a Go accessor
// would look like. Used for debugging only.
func (path callPath) String() string {
	if len(path) == 0 {
		return "<none>"
	}
	var parts []string
	for _, p := range path {
		last := len(parts) - 1
		switch {
		case p.elem:
			if len(parts) > 0 {
				parts[last] = "*" + parts[last]
			} else {
				parts = append(parts, "*")
			}
		case p.index:
			if len(parts) > 0 {
				parts[last] += "[i]"
			} else {
				parts = append(parts, "[i]")
			}
		case p.key:
			if len(parts) > 0 {
				parts[last] += "[key]"
			} else {
				parts = append(parts, "[key]")
			}
		default:
			if len(p.field) > 0 {
				parts = append(parts, p.field)
			} else {
				parts = append(parts, "<root>")
			}
		}
	}
	var calls []string
	// TODO: include validations?
	if len(calls) == 0 {
		calls = append(calls, "<none>")
	}

	return strings.Join(parts, ".") + " calls " + strings.Join(calls, ", ")
}
