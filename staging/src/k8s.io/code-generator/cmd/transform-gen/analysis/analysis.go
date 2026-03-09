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
	"go/token"
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

// varAlias records that a variable holds a value derived from a field access
// chain on an API type. For example, `spec := rs.Spec` produces an alias
// with apiType=ReplicaSet, goPath=["Spec"].
type varAlias struct {
	apiType *types.Named // root API type (e.g., *appsv1.ReplicaSet)
	goPath  []string     // Go field names from root (e.g., ["Spec"])
}

// fieldCollector walks ASTs to find field accesses on API types.
type fieldCollector struct {
	apiPrefix string
	accessed  map[typeKey]map[string]bool
}

// analyzePackage processes all files in a package looking for field accesses.
func (c *fieldCollector) analyzePackage(pkg *packages.Package) {
	for _, file := range pkg.Syntax {
		c.analyzeFile(pkg, file)
	}
}

// analyzeFile processes a single file. It finds all function bodies, builds
// variable alias maps per function, then scans for field accesses.
func (c *fieldCollector) analyzeFile(pkg *packages.Package, file *ast.File) {
	// Process each function/method in the file.
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Body == nil {
			continue
		}
		c.analyzeFunc(pkg, funcDecl)
	}
}

// analyzeFunc processes a single function: builds variable aliases, identifies
// intermediate selector expressions, then records leaf field accesses.
func (c *fieldCollector) analyzeFunc(pkg *packages.Package, fn *ast.FuncDecl) {
	// Phase 1: Build variable alias map and collect alias source expressions
	// (RHS expressions that should not be recorded as field accesses themselves).
	aliases, aliasSources := c.buildAliasMap(pkg, fn.Body)

	// Phase 2: Find selector expressions that should be skipped:
	// - Intermediate selectors (the .X of a longer field chain)
	// - Alias source expressions (RHS of := that created an alias)
	skip := map[ast.Node]bool{}
	for node := range aliasSources {
		skip[node] = true
		// Also mark all sub-selector-expressions within alias sources.
		ast.Inspect(node, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				skip[sel] = true
			}
			return true
		})
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selection, ok := pkg.TypesInfo.Selections[sel]; ok && selection.Kind() == types.FieldVal {
			if innerSel, ok := sel.X.(*ast.SelectorExpr); ok {
				if innerSelection, ok := pkg.TypesInfo.Selections[innerSel]; ok && innerSelection.Kind() == types.FieldVal {
					skip[innerSel] = true
				}
			}
		}
		return true
	})

	// Phase 3: Record leaf field accesses, resolving through aliases.
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if skip[sel] {
			return true
		}
		c.analyzeSelectorExpr(pkg, sel, aliases)
		return true
	})
}

// buildAliasMap scans a function body and builds a map from variable objects
// to their API type field path aliases. It also returns a set of AST expressions
// that are alias sources — these should not be recorded as field accesses since
// they only exist to feed into the alias.
//
// It handles:
//   - Short variable declarations: spec := rs.Spec
//   - Range value variables: for _, c := range pod.Spec.Containers
//   - Multi-hop aliases: sel := spec.Selector (where spec is already an alias)
//   - Index expressions: vol := &volumes[i] (where volumes is an alias)
func (c *fieldCollector) buildAliasMap(pkg *packages.Package, body *ast.BlockStmt) (map[types.Object]*varAlias, map[ast.Node]bool) {
	aliases := map[types.Object]*varAlias{}
	aliasSources := map[ast.Node]bool{}

	// We may need multiple passes to resolve multi-hop aliases
	// (e.g., spec := rs.Spec; sel := spec.Selector).
	// In practice, 3 passes is more than enough.
	for pass := 0; pass < 3; pass++ {
		changed := false

		ast.Inspect(body, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.AssignStmt:
				if s.Tok != token.DEFINE {
					return true
				}
				for i, lhs := range s.Lhs {
					if i >= len(s.Rhs) {
						break
					}
					ident, ok := lhs.(*ast.Ident)
					if !ok {
						continue
					}
					obj := pkg.TypesInfo.Defs[ident]
					if obj == nil {
						continue
					}
					if _, exists := aliases[obj]; exists {
						continue
					}
					if alias := c.resolveExprToAlias(pkg, s.Rhs[i], aliases); alias != nil {
						aliases[obj] = alias
						aliasSources[s.Rhs[i]] = true
						changed = true
					}
				}

			case *ast.RangeStmt:
				// Track the value variable of range statements.
				// for _, c := range pod.Spec.Containers → c aliases Spec.Containers elements
				if s.Value == nil {
					return true
				}
				ident, ok := s.Value.(*ast.Ident)
				if !ok {
					return true
				}
				obj := pkg.TypesInfo.Defs[ident]
				if obj == nil {
					return true
				}
				if _, exists := aliases[obj]; exists {
					return true
				}
				if alias := c.resolveExprToAlias(pkg, s.X, aliases); alias != nil {
					// The range value is an element of the collection,
					// but we keep the same path since the config handles
					// slice traversal via [] notation.
					aliases[obj] = alias
					aliasSources[s.X] = true
					changed = true
				}
			}
			return true
		})

		if !changed {
			break
		}
	}

	return aliases, aliasSources
}

// resolveExprToAlias attempts to resolve an expression to an API type alias.
// It handles field access chains, index expressions, and unary & operators.
func (c *fieldCollector) resolveExprToAlias(pkg *packages.Package, expr ast.Expr, aliases map[types.Object]*varAlias) *varAlias {
	// Strip parentheses.
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			break
		}
		expr = paren.X
	}

	// Handle &x (address-of).
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		return c.resolveExprToAlias(pkg, unary.X, aliases)
	}

	// Handle x[i] (index expression) — for volumes[i], track as volumes.
	if idx, ok := expr.(*ast.IndexExpr); ok {
		return c.resolveExprToAlias(pkg, idx.X, aliases)
	}

	// Try to resolve as a field access chain.
	chain := c.buildSelectorChain(pkg, expr, aliases)
	if chain == nil {
		// Not a field chain. Check if it's a simple variable with an existing alias.
		if ident, ok := expr.(*ast.Ident); ok {
			obj := pkg.TypesInfo.Uses[ident]
			if obj != nil {
				if alias, ok := aliases[obj]; ok {
					return alias
				}
			}
		}
		return nil
	}

	return &varAlias{
		apiType: chain.apiType,
		goPath:  chain.fields,
	}
}

// resolvedChain represents a fully resolved field access chain rooted at an API type.
type resolvedChain struct {
	apiType *types.Named // The root API type
	fields  []string     // Go field names from the API type root
}

// buildSelectorChain walks a selector expression chain and resolves it to an
// API type root with field path. It resolves through variable aliases.
// Returns nil if the chain doesn't trace back to an API type.
func (c *fieldCollector) buildSelectorChain(pkg *packages.Package, expr ast.Expr, aliases map[types.Object]*varAlias) *resolvedChain {
	var fields []string
	current := expr

	for {
		sel, ok := current.(*ast.SelectorExpr)
		if !ok {
			break
		}

		selection, ok := pkg.TypesInfo.Selections[sel]
		if !ok || selection.Kind() != types.FieldVal {
			break
		}

		fields = append([]string{sel.Sel.Name}, fields...)
		current = sel.X
	}

	if len(fields) == 0 && !isIdent(current) {
		return nil
	}

	// Resolve the root expression.
	// Case 1: root is an identifier — check its type or aliases.
	if ident, ok := current.(*ast.Ident); ok {
		obj := pkg.TypesInfo.Uses[ident]
		if obj == nil {
			return nil
		}

		// Check aliases first.
		if alias, ok := aliases[obj]; ok {
			return &resolvedChain{
				apiType: alias.apiType,
				fields:  append(append([]string{}, alias.goPath...), fields...),
			}
		}

		// Check if the variable's type is an API type.
		varType := obj.Type()
		named := namedType(varType)
		if named == nil {
			return nil
		}
		namedObj := named.Obj()
		if namedObj.Pkg() == nil {
			return nil
		}
		if !strings.HasPrefix(namedObj.Pkg().Path(), c.apiPrefix) {
			return nil
		}
		if !hasTypeMeta(named) {
			return nil
		}

		return &resolvedChain{
			apiType: named,
			fields:  fields,
		}
	}

	return nil
}

// analyzeSelectorExpr checks if a selector expression accesses a field on an API type,
// resolving through variable aliases.
func (c *fieldCollector) analyzeSelectorExpr(pkg *packages.Package, sel *ast.SelectorExpr, aliases map[types.Object]*varAlias) {
	selection, ok := pkg.TypesInfo.Selections[sel]
	if !ok {
		return
	}
	if selection.Kind() != types.FieldVal {
		return
	}

	// Build the full chain, resolving through aliases.
	chain := c.buildSelectorChain(pkg, sel, aliases)
	if chain == nil {
		return
	}

	key := typeKey{
		pkg:  chain.apiType.Obj().Pkg().Path(),
		name: chain.apiType.Obj().Name(),
	}

	// Convert Go field path to JSON field path.
	jsonPath, skip := goPathToJSONPath(chain.apiType, chain.fields)
	if skip || jsonPath == "" {
		return
	}

	if c.accessed[key] == nil {
		c.accessed[key] = map[string]bool{}
	}
	c.accessed[key][jsonPath] = true
	klog.V(4).Infof("Found field access: %s.%s -> %s", key.name, strings.Join(chain.fields, "."), jsonPath)
}

// goPathToJSONPath converts a Go field path (e.g., ["Spec", "Selector"]) to a
// JSON field path (e.g., "spec.selector") using struct tags.
//
// Returns the JSON path and a skip flag. skip=true means this access should be
// ignored (e.g., because it goes through ObjectMeta/TypeMeta which are auto-injected).
func goPathToJSONPath(named *types.Named, goFields []string) (string, bool) {
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
// embedded types.
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
		gv := strings.TrimPrefix(key.pkg, c.apiPrefix)
		gv = strings.TrimPrefix(gv, "/")

		if result[gv] == nil {
			result[gv] = map[string][]string{}
		}

		var fieldPaths []string
		for p := range paths {
			fieldPaths = append(fieldPaths, p)
		}
		sort.Strings(fieldPaths)

		result[gv][key.name] = fieldPaths
	}

	return result
}

// isIdent returns true if the expression is an identifier.
func isIdent(expr ast.Expr) bool {
	_, ok := expr.(*ast.Ident)
	return ok
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
// It also dereferences slices and maps to their element types,
// since field access on a range variable goes through the element.
func deref(t types.Type) types.Type {
	t = derefType(t)
	switch v := t.(type) {
	case *types.Slice:
		return derefType(v.Elem())
	case *types.Map:
		return derefType(v.Elem())
	case *types.Array:
		return derefType(v.Elem())
	}
	return t
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
