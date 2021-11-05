// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    https://www.apache.org/licenses/LICENSE2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package model

import (
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
)

// SchemaDeclTypes constructs a top-down set of DeclType instances whose name is derived from the root
// type name provided on the call, if not set to a custom type.
func SchemaDeclTypes(s *schema.Structural, maybeRootType string) (*DeclType, map[string]*DeclType) {
	root := SchemaDeclType(s).MaybeAssignTypeName(maybeRootType)
	types := FieldTypeMap(maybeRootType, root)
	return root, types
}

// SchemaDeclType returns the CEL Policy Templates type name associated with the schema element.
func SchemaDeclType(s *schema.Structural) *DeclType {
	declType, found := openAPISchemaTypes[s.Type]
	if !found {
		return nil
	}
	if s.XIntOrString {
		return intOrStringType
	}
	if s.XEmbeddedResource {
		return embeddedType
	}
	// We ignore XPreserveUnknownFields since we don't support validation rules on data that we don't have schema
	// information for.
	switch declType.TypeName() {
	case ListType.TypeName():
		return NewListType(SchemaDeclType(s.Items))
	case MapType.TypeName():
		if s.AdditionalProperties != nil && s.AdditionalProperties.Structural != nil {
			return NewMapType(StringType, SchemaDeclType(s.AdditionalProperties.Structural))
		}
		fields := make(map[string]*DeclField, len(s.Properties))

		required := map[string]bool{}
		if s.ValueValidation != nil {
			for _, f := range s.ValueValidation.Required {
				required[f] = true
			}
		}
		for name, prop := range s.Properties {
			var enumValues []interface{}
			if prop.ValueValidation != nil {
				for _, e := range prop.ValueValidation.Enum {
					enumValues = append(enumValues, e.Object)
				}
			}
			fields[Escape(name)] = &DeclField{
				Name:         Escape(name),
				Required:     required[name],
				Type:         SchemaDeclType(&prop),
				defaultValue: prop.Default.Object,
				enumValues:   enumValues, // Enum values are represented as strings in CEL
			}
		}
		return NewObjectType("object", fields)
	case StringType.TypeName():
		if s.ValueValidation != nil {
			switch s.ValueValidation.Format {
			case "byte":
				return StringType // OpenAPIv3 byte format represents base64 encoded string
			case "binary":
				return BytesType
			case "duration":
				return DurationType
			case "date", "date-time":
				return TimestampType
			}
		}
	}
	return declType
}

var (
	openAPISchemaTypes = map[string]*DeclType{
		"boolean":         BoolType,
		"number":          DoubleType,
		"integer":         IntType,
		"null":            NullType,
		"string":          StringType,
		"array":           ListType,
		"object":          MapType,
	}

	intOrStringType = NewObjectType("intOrString", map[string]*DeclField{
		"strVal": {Name: "strVal", Type: StringType},
		"intVal": {Name: "intVal", Type: IntType},
	})

	embeddedType = NewObjectType("embedded", map[string]*DeclField{
		"kind":       {Name: "kind", Type: StringType},
		"apiVersion": {Name: "apiVersion", Type: StringType},
		"metadata": {Name: "metadata", Type: NewObjectType("metadata", map[string]*DeclField{
			"name":         {Name: "name", Type: StringType},
			"generateName": {Name: "generateName", Type: StringType},
		})},
	})
)
