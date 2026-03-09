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

// Package analysis provides static analysis of Go source code to discover
// which fields of API types are accessed by consumer packages. This is used
// by transform-gen to automatically determine which fields to retain in
// informer cache transforms, eliminating the need for a hand-written config file.
package analysis

import (
	"fmt"
	"go/ast"
	"go/types"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
	"k8s.io/klog/v2"
)

// FieldUsage maps "group/version" -> typeName -> []fieldPaths (JSON names).
// This is the same shape as config.Config.
type FieldUsage map[string]map[string][]string

// AnalyzeFieldUsage loads the given scan packages and discovers which fields
// of types from apiPackagePrefix are accessed. It returns a FieldUsage map
// using JSON field names (matching the config.Config format).
//
// scanPatterns are Go package patterns (e.g., "k8s.io/kubernetes/pkg/scheduler/...").
// apiPackagePrefix is the import path prefix for API types (e.g., "k8s.io/api").
func AnalyzeFieldUsage(scanPatterns []string, apiPackagePrefix string) (FieldUsage, error) {
	cfg := &packages.Config{
		Mode: packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax |
			packages.NeedName | packages.NeedImports | packages.NeedDeps,
	}

	klog.V(2).Infof("Loading packages matching %v", scanPatterns)
	pkgs, err := packages.Load(cfg, scanPatterns...)
	if err != nil {
		return nil, fmt.Errorf("loading packages: %w", err)
	}

	// Check for package loading errors.
	var errs []error
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			errs = append(errs, fmt.Errorf("package %s: %s", pkg.PkgPath, e.Msg))
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("package errors: %v", errs)
	}

	// Collect all field accesses on API types.
	collector := &fieldCollector{
		apiPrefix: apiPackagePrefix,
		accessed:  map[typeKey]map[string]bool{},
	}

	for _, pkg := range pkgs {
		collector.analyzePackage(pkg)
	}

	return collector.toFieldUsage(), nil
}

// typeKey identifies an API type by its package path and name.
type typeKey struct {
	pkg  string // full package path, e.g., "k8s.io/api/apps/v1"
	name string // type name, e.g., "ReplicaSet"
}

// fieldCollector walks ASTs to find field accesses on API types.
type fieldCollector struct {
	apiPrefix string
	accessed  map[typeKey]map[string]bool
}

// analyzePackage processes all files in a package looking for field accesses.
// It builds a set of parent SelectorExprs first so that only leaf (longest-chain)
// accesses are recorded.
func (c *fieldCollector) analyzePackage(pkg *packages.Package) {
	for _, file := range pkg.Syntax {
		// First pass: collect all SelectorExprs that are the .X of another
		// field-access SelectorExpr. These are intermediate and should be skipped.
		intermediate := map[ast.Node]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// If this selector is a field access...
			if selection, ok := pkg.TypesInfo.Selections[sel]; ok && selection.Kind() == types.FieldVal {
				// ...then its receiver (sel.X) is intermediate if it's also a field-access selector.
				if innerSel, ok := sel.X.(*ast.SelectorExpr); ok {
					if innerSelection, ok := pkg.TypesInfo.Selections[innerSel]; ok && innerSelection.Kind() == types.FieldVal {
						intermediate[innerSel] = true
					}
				}
			}
			return true
		})

		// Second pass: only analyze leaf selector expressions.
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if intermediate[sel] {
				return true
			}
			c.analyzeSelectorExpr(pkg, sel)
			return true
		})
	}
}

// analyzeSelectorExpr checks if a selector expression accesses a field on an API type.
// It walks the selector chain to build the full field path.
func (c *fieldCollector) analyzeSelectorExpr(pkg *packages.Package, sel *ast.SelectorExpr) {
	// Get the selection info for this selector expression.
	selection, ok := pkg.TypesInfo.Selections[sel]
	if !ok {
		return
	}

	// We only care about field accesses, not method calls.
	if selection.Kind() != types.FieldVal {
		return
	}

	// Walk the selector chain to build the full path and find the root type.
	chain := c.buildSelectorChain(pkg, sel)
	if chain == nil {
		return
	}

	// Check if the root type is from an API package.
	rootType := chain.rootType
	named := namedType(rootType)
	if named == nil {
		return
	}

	obj := named.Obj()
	if obj.Pkg() == nil {
		return
	}
	pkgPath := obj.Pkg().Path()
	if !strings.HasPrefix(pkgPath, c.apiPrefix) {
		return
	}

	// Only track types that have TypeMeta (i.e., top-level API types).
	if !hasTypeMeta(named) {
		return
	}

	key := typeKey{pkg: pkgPath, name: obj.Name()}

	// Convert the Go field path to a JSON field path, resolving embedded types.
	jsonPath, skip := c.goPathToJSONPath(named, chain.fields)
	if skip || jsonPath == "" {
		return
	}

	if c.accessed[key] == nil {
		c.accessed[key] = map[string]bool{}
	}
	c.accessed[key][jsonPath] = true
	klog.V(4).Infof("Found field access: %s.%s -> %s", key.name, strings.Join(chain.fields, "."), jsonPath)
}

// selectorChain represents a chain of field accesses like obj.Spec.Selector.
type selectorChain struct {
	rootType types.Type // The type of the root variable (e.g., *appsv1.ReplicaSet)
	fields   []string   // Go field names in order (e.g., ["Spec", "Selector"])
}

// buildSelectorChain walks a selector expression chain and returns the root type
// and field names. Returns nil if the chain can't be resolved.
func (c *fieldCollector) buildSelectorChain(pkg *packages.Package, sel *ast.SelectorExpr) *selectorChain {
	var fields []string
	var expr ast.Expr = sel

	for {
		s, ok := expr.(*ast.SelectorExpr)
		if !ok {
			break
		}

		// Check if this is a field access.
		selection, ok := pkg.TypesInfo.Selections[s]
		if !ok || selection.Kind() != types.FieldVal {
			// Not a field access (could be a package qualifier or method).
			break
		}

		fields = append([]string{s.Sel.Name}, fields...)
		expr = s.X
	}

	if len(fields) == 0 {
		return nil
	}

	// Get the type of the root expression.
	rootType := pkg.TypesInfo.TypeOf(expr)
	if rootType == nil {
		return nil
	}

	return &selectorChain{
		rootType: rootType,
		fields:   fields,
	}
}

// goPathToJSONPath converts a Go field path (e.g., ["Spec", "Selector"]) to a
// JSON field path (e.g., "spec.selector") using struct tags.
//
// Returns the JSON path and a skip flag. skip=true means this access should be
// ignored (e.g., because it goes through ObjectMeta/TypeMeta which are auto-injected).
func (c *fieldCollector) goPathToJSONPath(named *types.Named, goFields []string) (string, bool) {
	var jsonParts []string
	currentType := named.Underlying()

	for i, fieldName := range goFields {
		st, ok := structType(currentType)
		if !ok {
			return "", true
		}

		result := findFieldInStruct(st, fieldName)
		if result.field == nil {
			return "", true
		}

		// If the field was found through an embedding (not directly on the struct),
		// and it's the first field in the chain, check if it's ObjectMeta/TypeMeta.
		// Those are auto-injected by the generator and don't need to be in config.
		if result.throughEmbedding != "" && i == 0 {
			if result.throughEmbedding == "ObjectMeta" || result.throughEmbedding == "TypeMeta" {
				return "", true
			}
		}

		if result.jsonName != "" {
			jsonParts = append(jsonParts, result.jsonName)
		}
		// If jsonName is empty, it's an inline embedded field — skip it in the path.

		currentType = result.nextType
	}

	if len(jsonParts) == 0 {
		return "", true
	}

	return strings.Join(jsonParts, "."), false
}

// fieldLookupResult is the result of looking up a field in a struct.
type fieldLookupResult struct {
	field            *types.Var
	jsonName         string
	nextType         types.Type
	throughEmbedding string // non-empty if found through an embedded field (the embedded type's name)
}

// findFieldInStruct looks up a field by Go name in a struct type, handling
// embedded types. Returns the field, its JSON name, its type, and whether
// it was found through an embedding.
func findFieldInStruct(st *types.Struct, name string) fieldLookupResult {
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if f.Name() == name {
			jsonName := jsonFieldName(st.Tag(i), f.Name())
			return fieldLookupResult{
				field:    f,
				jsonName: jsonName,
				nextType: deref(f.Type()),
			}
		}
		// Check embedded structs.
		if f.Embedded() {
			embeddedSt, ok := structType(deref(f.Type()))
			if ok {
				result := findFieldInStruct(embeddedSt, name)
				if result.field != nil {
					// Record that we found this through an embedding.
					if result.throughEmbedding == "" {
						result.throughEmbedding = f.Name()
					}
					return result
				}
			}
		}
	}
	return fieldLookupResult{}
}

// jsonFieldName extracts the JSON field name from a struct tag.
// For inline fields, returns empty string.
func jsonFieldName(tag, goName string) string {
	jsonTag := reflect.StructTag(tag).Get("json")
	if jsonTag == "" || jsonTag == "-" {
		return goName
	}
	parts := strings.SplitN(jsonTag, ",", 2)
	name := parts[0]
	opts := ""
	if len(parts) > 1 {
		opts = parts[1]
	}

	// Inline embedded fields don't contribute to the JSON path.
	if name == "" && strings.Contains(opts, "inline") {
		return ""
	}

	if name == "" {
		return goName
	}
	return name
}

// toFieldUsage converts the collected field accesses to a FieldUsage map.
func (c *fieldCollector) toFieldUsage() FieldUsage {
	result := FieldUsage{}

	for key, paths := range c.accessed {
		// Extract group/version from package path by stripping the API prefix.
		gv := strings.TrimPrefix(key.pkg, c.apiPrefix)
		gv = strings.TrimPrefix(gv, "/")

		if result[gv] == nil {
			result[gv] = map[string][]string{}
		}

		// Convert set to sorted slice.
		var fieldPaths []string
		for p := range paths {
			fieldPaths = append(fieldPaths, p)
		}
		sort.Strings(fieldPaths)

		result[gv][key.name] = fieldPaths
	}

	return result
}

// namedType returns the underlying named type, dereferencing pointers.
func namedType(t types.Type) *types.Named {
	t = derefType(t)
	if named, ok := t.(*types.Named); ok {
		return named
	}
	return nil
}

// derefType strips pointer wrappers from a type.
func derefType(t types.Type) types.Type {
	for {
		ptr, ok := t.(*types.Pointer)
		if !ok {
			return t
		}
		t = ptr.Elem()
	}
}

// deref strips pointer wrappers from a type.
func deref(t types.Type) types.Type {
	return derefType(t)
}

// structType extracts the *types.Struct from a type, handling named types.
func structType(t types.Type) (*types.Struct, bool) {
	t = derefType(t)
	switch v := t.(type) {
	case *types.Struct:
		return v, true
	case *types.Named:
		return structType(v.Underlying())
	default:
		return nil, false
	}
}

// hasTypeMeta checks if a named type has an embedded TypeMeta field.
func hasTypeMeta(named *types.Named) bool {
	st, ok := structType(named)
	if !ok {
		return false
	}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if f.Embedded() && f.Name() == "TypeMeta" {
			return true
		}
	}
	return false
}
