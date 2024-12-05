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

package static

import (
	"fmt"
	celtypes "github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"reflect"
	"strings"

	"k8s.io/apiserver/pkg/cel"
	"k8s.io/apiserver/pkg/cel/common"
	"k8s.io/apiserver/pkg/cel/openapi"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

// ObjectType is the implementation of the Object type for use when compiling
// CEL expressions with schema information about the object.
// This is to provide CEL expressions with access to Object{} types constructors.
type ObjectType struct {
	objectType *types.Type
	Schema     *spec.Schema
}

// NewObjectType creates a ObjectType by the given field name.
func NewObjectType(name string, schema *spec.Schema) *ObjectType {
	return &ObjectType{
		objectType: types.NewObjectType(name),
		Schema:     schema,
	}
}

func (r *ObjectType) Type() *types.Type {
	return r.objectType
}

func (r *ObjectType) HasTrait(trait int) bool {
	return r.objectType.HasTrait(trait)
}

func (r *ObjectType) TypeName() string {
	return r.objectType.TypeName()
}

// Val creates Objects from CEL data "Object{}" and "Object.<fieldName>{}" data literals..
func (r *ObjectType) Val(fields map[string]ref.Val) ref.Val {
	// Convert the ref.Val values evaluated by CEL to create a new object into unstructured
	unstructured := map[string]any{}
	for k, v := range fields {
		unstructured[k] = toUnstructured(v)
	}
	// Create a replacement ref.Val that is typed according to this object's schema.
	// This enables schema aware runtime type checking.
	return common.UnstructuredToValWithTypeNames(unstructured, &openapi.Schema{Schema: r.Schema}, r.Namer())
}

// Field looks up the field by name.
// This uses the objects schema to provide compile time type checking
func (r *ObjectType) Field(name string) (*types.FieldType, bool) {
	fieldProp, ok := r.Schema.Properties[name]
	if !ok {
		return nil, false
	}
	declType := openapi.SchemaDeclType(&fieldProp, false)

	return &types.FieldType{
		Type: toCELType(r.Namer().ForField(name), declType),
		IsSet: func(target any) bool {
			if m, ok := target.(map[string]any); ok {
				_, isSet := m[name]
				return isSet
			}
			return false
		},
		GetFrom: func(target any) (any, error) {
			if m, ok := target.(map[string]any); ok {
				return m[name], nil
			}
			return nil, fmt.Errorf("cannot get field %q", name)
		},
	}, true
}

// FIXME: tests

// toCELType creates CEL Types to match the given DeclType, but assigns names using the given namer.
func toCELType(namer common.TypeNamer, declType *cel.DeclType) *celtypes.Type {
	if declType.IsObject() {
		return types.NewObjectType(namer.Type().TypeName())
	}
	if declType.IsList() {
		return types.NewListType(toCELType(namer, declType.ElemType))
	}
	if declType.IsMap() {
		return types.NewMapType(types.StringType, toCELType(namer, declType.ElemType))
	}
	return declType.CelType()
}

func (r *ObjectType) FieldNames() ([]string, bool) {
	return nil, true // Field names are not known for dynamic types. All field names are allowed.
}

func (r *ObjectType) Namer() common.TypeNamer {
	return NewObjectTypeNamer(r.objectType.TypeName())
}

// FIXME: tests

// ResolveSchemaForObjectTypePath find the schema at the given path of ObjectType field names.
// An object type schema is returned, or false if one cannot be found.  Any intermediate maps or lists
// are skipped during traversal.
func ResolveSchemaForObjectTypePath(objectTypeFieldNames []string, schema *spec.Schema) (*spec.Schema, bool) {
	schema = nextPropertiesSchema(schema)
	for i := 0; i < len(objectTypeFieldNames); {
		part := objectTypeFieldNames[i]
		if schema != nil {
			propSchema, ok := schema.Properties[part]
			if !ok {
				return nil, false
			}
			schema = &propSchema
			schema = nextPropertiesSchema(schema)
			i++
		}
	}
	return schema, true
}

func nextPropertiesSchema(schema *spec.Schema) *spec.Schema {
	for len(schema.Properties) == 0 {
		if schema.AdditionalProperties != nil && schema.AdditionalProperties.Schema != nil {
			schema = schema.AdditionalProperties.Schema
		} else if schema.Items != nil && schema.Items.Schema != nil {
			schema = schema.Items.Schema
		} else {
			return nil
		}
	}
	return schema
}

// FIXME: tests

// toUnstructured transitively converts a ref.Val suitable for use as a unstructured map.
func toUnstructured(r ref.Val) any {
	switch r.Type() {
	case types.MapType: // objects and maps
		m := map[string]any{}
		mapper := r.(traits.Mapper)
		for iter := mapper.Iterator(); iter.HasNext() == types.True; {
			k := iter.Next()
			v, ok := mapper.Find(k)
			if !ok {
				return fmt.Errorf("unexpected error converting map to unstructured: key %v missing", k)
			}
			nativeKey, err := k.ConvertToNative(reflect.TypeOf(""))
			if err != nil {
				return fmt.Errorf("unexpected error converting map to unstructured: key %v string conversion error: %w", err)
			}
			stringKey, ok := nativeKey.(string)
			if !ok {
				return fmt.Errorf("unexpected error converting map to unstructured: key %v not a string", k)
			}
			m[stringKey] = toUnstructured(v)
		}
		return m
	case types.ListType: // lists
		var l []any
		for iter := r.(traits.Iterable).Iterator(); iter.HasNext() == types.True; {
			v := iter.Next()
			l = append(l, toUnstructured(v))
		}
		return l
	default:
		return r.Value() // all scalars
	}
}

// FIXME: tests

// ObjectTypeNamer names object types in CEL using the "Object.<fieldname>" scheme.
type ObjectTypeNamer []string

func NewObjectTypeNamer(name string) ObjectTypeNamer {
	parts := strings.Split(name, ".")
	if len(parts) >= 1 && parts[0] == "Object" {
		return parts
	}
	return nil
}

func (o ObjectTypeNamer) Type() *types.Type {
	return types.NewObjectType(o.String())
}

func (o ObjectTypeNamer) ForField(name string) common.TypeNamer {
	return append(o, name)
}

func (o ObjectTypeNamer) String() string {
	return strings.Join(o, ".")
}

// Path returns the field path parts of the Object type name.
// The leading "Object" identifier is not included.
func (o ObjectTypeNamer) Path() []string {
	if len(o) <= 1 {
		return []string{}
	}
	return o[1:]
}
