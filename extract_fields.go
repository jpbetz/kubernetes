package main

import (
	"encoding/csv"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// APIKind represents an entry from the api_groups_and_kinds.csv file
type APIKind struct {
	APIGroup   string
	Kind       string
	ImportPath string
}

// FieldInfo represents information about a field in a Kind
type FieldInfo struct {
	APIGroup   string
	Kind       string
	Field      string
	Type       string
	JSONTag    string
	Optional   string
	StructTags string
}

func main() {
	// Read the CSV file with API groups and kinds
	kinds, err := readAPIKindsCSV("data/api_groups_and_kinds.csv")
	if err != nil {
		fmt.Printf("Error reading CSV: %v\n", err)
		os.Exit(1)
	}

	// Create output file
	outputFile, err := os.Create("data/api_fields.csv")
	if err != nil {
		fmt.Printf("Error creating output file: %v\n", err)
		os.Exit(1)
	}
	defer outputFile.Close()

	// Write CSV header
	writer := csv.NewWriter(outputFile)
	defer writer.Flush()

	header := []string{"APIGroup", "Kind", "Field", "Type", "JSONTag", "Optional", "StructTags"}
	if err := writer.Write(header); err != nil {
		fmt.Printf("Error writing header: %v\n", err)
		os.Exit(1)
	}

	// Process each Kind
	for _, kind := range kinds {
		fmt.Printf("Processing %s.%s from %s\n", kind.APIGroup, kind.Kind, kind.ImportPath)
		fields, err := extractFieldsForKind(kind)
		if err != nil {
			fmt.Printf("  Error extracting fields: %v\n", err)
			continue
		}

		// Write fields to CSV
		for _, field := range fields {
			record := []string{
				field.APIGroup,
				field.Kind,
				field.Field,
				field.Type,
				field.JSONTag,
				field.Optional,
				field.StructTags,
			}
			if err := writer.Write(record); err != nil {
				fmt.Printf("Error writing record: %v\n", err)
				continue
			}
		}
	}

	fmt.Println("Field extraction complete. Output saved to data/api_fields.csv")
}

func readAPIKindsCSV(filename string) ([]APIKind, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	// Skip header
	var kinds []APIKind
	for i, record := range records {
		if i == 0 {
			continue
		}
		if len(record) >= 3 {
			kinds = append(kinds, APIKind{
				APIGroup:   record[0],
				Kind:       record[1],
				ImportPath: record[2],
			})
		}
	}
	return kinds, nil
}

func extractFieldsForKind(kind APIKind) ([]FieldInfo, error) {
	var fields []FieldInfo

	// Convert import path format (if needed)
	importPath := strings.ReplaceAll(kind.ImportPath, "\\", "/")

	// Determine source file locations
	var sourceFiles []string

	// Try different common locations for finding the struct definition
	if strings.HasPrefix(importPath, "k8s.io/kubernetes") {
		// For internal API definitions
		basePath := strings.TrimPrefix(importPath, "k8s.io/kubernetes/")

		// Try both internal and versioned API paths
		internalPath := filepath.Join(basePath, "types.go")
		sourceFiles = append(sourceFiles, internalPath)

		// For versioned APIs, try each version
		paths, _ := filepath.Glob(filepath.Join(filepath.Dir(basePath), "*/types.go"))
		sourceFiles = append(sourceFiles, paths...)
	} else if strings.HasPrefix(importPath, "k8s.io/api") {
		// For versioned APIs in the staging area
		basePath := strings.TrimPrefix(importPath, "k8s.io/api/")
		stagingPath := filepath.Join("staging/src/k8s.io/api", basePath, "types.go")
		sourceFiles = append(sourceFiles, stagingPath)
	}

	for _, sourceFile := range sourceFiles {
		fmt.Printf("  Checking file: %s\n", sourceFile)

		// Parse the source file
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, sourceFile, nil, parser.ParseComments)
		if err != nil {
			fmt.Printf("  Error parsing file: %v\n", err)
			continue
		}

		// Look for the type declaration that matches the Kind
		for _, decl := range node.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}

			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || typeSpec.Name.Name != kind.Kind {
					continue
				}

				// Found the matching type declaration
				fmt.Printf("  Found %s in %s\n", kind.Kind, sourceFile)

				// Extract fields from struct
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					fmt.Printf("  %s is not a struct type\n", kind.Kind)
					continue
				}

				// Process each field in the struct
				for _, field := range structType.Fields.List {
					fieldNames := make([]string, len(field.Names))
					for i, name := range field.Names {
						fieldNames[i] = name.Name
					}

					// Handle embedded fields (like TypeMeta, ObjectMeta)
					if len(fieldNames) == 0 && field.Tag == nil {
						// This is likely an embedded field, use the type name
						switch expr := field.Type.(type) {
						case *ast.Ident:
							fieldNames = append(fieldNames, expr.Name)
						case *ast.SelectorExpr:
							fieldNames = append(fieldNames, expr.Sel.Name)
						}
					}

					for _, fieldName := range fieldNames {
						fieldInfo := FieldInfo{
							APIGroup: kind.APIGroup,
							Kind:     kind.Kind,
							Field:    fieldName,
							Type:     formatType(field.Type),
							Optional: isOptional(field.Type),
						}

						// Extract JSON tag and other struct tags
						if field.Tag != nil {
							tag := strings.Trim(field.Tag.Value, "`")
							fieldInfo.StructTags = tag

							// Extract JSON tag
							jsonTag := extractTagValue(tag, "json")
							fieldInfo.JSONTag = jsonTag
						}

						fields = append(fields, fieldInfo)
					}
				}
			}
		}
	}

	return fields, nil
}

func formatType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + formatType(t.X)
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + formatType(t.Elt)
		}
		return "[N]" + formatType(t.Elt)
	case *ast.MapType:
		return "map[" + formatType(t.Key) + "]" + formatType(t.Value)
	case *ast.SelectorExpr:
		return formatType(t.X) + "." + t.Sel.Name
	case *ast.InterfaceType:
		return "interface{}"
	default:
		return fmt.Sprintf("unknown(%T)", expr)
	}
}

func isOptional(expr ast.Expr) string {
	// Check if the type is a pointer
	_, isPointer := expr.(*ast.StarExpr)
	if isPointer {
		return "true"
	}
	return "false"
}

func extractTagValue(tag, key string) string {
	key = key + ":"
	for _, part := range strings.Split(tag, " ") {
		if strings.HasPrefix(part, key) {
			value := strings.TrimPrefix(part, key)
			value = strings.Trim(value, "\"")
			return value
		}
	}
	return ""
}
