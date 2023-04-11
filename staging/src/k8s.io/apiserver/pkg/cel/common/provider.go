/*
Copyright 2023 The Kubernetes Authors.

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

package common

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// NewOpenAPITypeProvider returns an Open API Schema-based type-system which is CEL compatible.
func NewOpenAPITypeProvider(
	rootTypes ...*DeclType) (*OpenAPITypeProvider, error) {
	// Note, if the schema indicates that it's actually based on another proto
	// then prefer the proto definition. For expressions in the proto, a new field
	// annotation will be needed to indicate the expected environment and type of
	// the expression.
	allTypes, err := allTypesForDecl(rootTypes)
	if err != nil {
		return nil, err
	}
	return &OpenAPITypeProvider{
		registeredTypes: allTypes,
	}, nil
}

// OpenAPITypeProvider extends the CEL ref.TypeProvider interface and provides an Open API Schema-based
// type-system.
type OpenAPITypeProvider struct {
	registeredTypes map[string]*DeclType
	typeProvider    ref.TypeProvider
	typeAdapter     ref.TypeAdapter
}

func (rt *OpenAPITypeProvider) EnumValue(enumName string) ref.Val {
	return rt.typeProvider.EnumValue(enumName)
}

func (rt *OpenAPITypeProvider) FindIdent(identName string) (ref.Val, bool) {
	return rt.typeProvider.FindIdent(identName)
}

// EnvOptions returns a set of cel.EnvOption values which includes the declaration set
// as well as a custom ref.TypeProvider.
//
// Note, the standard declaration set includes 'rule' which is defined as the top-level rule-schema
// type if one is configured.
//
// If the OpenAPITypeProvider value is nil, an empty []cel.EnvOption set is returned.
func (rt *OpenAPITypeProvider) EnvOptions(tp ref.TypeProvider) ([]cel.EnvOption, error) {
	if rt == nil {
		return []cel.EnvOption{}, nil
	}
	rtWithTypes, err := rt.WithTypeProvider(tp)
	if err != nil {
		return nil, err
	}
	return []cel.EnvOption{
		cel.CustomTypeProvider(rtWithTypes),
		cel.CustomTypeAdapter(rtWithTypes),
	}, nil
}

// WithTypeProvider returns a new OpenAPITypeProvider that sets the given TypeProvider
// If the original OpenAPITypeProvider is nil, the returned OpenAPITypeProvider is still nil.
func (rt *OpenAPITypeProvider) WithTypeProvider(tp ref.TypeProvider) (*OpenAPITypeProvider, error) {
	if rt == nil {
		return nil, nil
	}
	var ta ref.TypeAdapter = types.DefaultTypeAdapter
	tpa, ok := tp.(ref.TypeAdapter)
	if ok {
		ta = tpa
	}
	rtWithTypes := &OpenAPITypeProvider{
		typeProvider:    tp,
		typeAdapter:     ta,
		registeredTypes: rt.registeredTypes,
	}
	for name, declType := range rt.registeredTypes {
		tpType, found := tp.FindType(name)
		expT, err := declType.ExprType()
		if err != nil {
			return nil, fmt.Errorf("fail to get cel type: %s", err)
		}
		if found && !proto.Equal(tpType, expT) {
			return nil, fmt.Errorf(
				"type %s definition differs between CEL environment and rule", name)
		}
	}
	return rtWithTypes, nil
}

// FindType attempts to resolve the typeName provided from the rule's rule-schema, or if not
// from the embedded ref.TypeProvider.
//
// FindType overrides the default type-finding behavior of the embedded TypeProvider.
//
// Note, when the type name is based on the Open API Schema, the name will reflect the object path
// where the type definition appears.
func (rt *OpenAPITypeProvider) FindType(typeName string) (*exprpb.Type, bool) {
	if rt == nil {
		return nil, false
	}
	declType, found := rt.findDeclType(typeName)
	if found {
		expT, err := declType.ExprType()
		if err != nil {
			return expT, false
		}
		return &exprpb.Type{
			TypeKind: &exprpb.Type_Type{
				Type: expT}}, true
	}
	return rt.typeProvider.FindType(typeName)
}

// FindDeclType returns the CPT type description which can be mapped to a CEL type.
func (rt *OpenAPITypeProvider) FindDeclType(typeName string) (*DeclType, bool) {
	if rt == nil {
		return nil, false
	}
	return rt.findDeclType(typeName)
}

// FindFieldType returns a field type given a type name and field name, if found.
//
// Note, the type name for an Open API Schema type is likely to be its qualified object path.
// If, in the future an object instance rather than a type name were provided, the field
// resolution might more accurately reflect the expected type model. However, in this case
// concessions were made to align with the existing CEL interfaces.
func (rt *OpenAPITypeProvider) FindFieldType(typeName, fieldName string) (*ref.FieldType, bool) {
	st, found := rt.findDeclType(typeName)
	if !found {
		return rt.typeProvider.FindFieldType(typeName, fieldName)
	}

	f, found := st.Fields[fieldName]
	if found {
		ft := f.Type
		expT, err := ft.ExprType()
		if err != nil {
			return nil, false
		}
		return &ref.FieldType{
			Type: expT,
		}, true
	}
	// This could be a dynamic map.
	if st.IsMap() {
		et := st.ElemType
		expT, err := et.ExprType()
		if err != nil {
			return nil, false
		}
		return &ref.FieldType{
			Type: expT,
		}, true
	}
	return nil, false
}

// NativeToValue is an implementation of the ref.TypeAdapater interface which supports conversion
// of rule values to CEL ref.Val instances.
func (rt *OpenAPITypeProvider) NativeToValue(val interface{}) ref.Val {
	return rt.typeAdapter.NativeToValue(val)
}

func valueToUnstructured(o ref.Val) any {
	switch t := o.Value().(type) {
	case map[ref.Val]ref.Val:
		result := make(map[string]any, len(t))
		for k, v := range t {
			result[k.Value().(string)] = valueToUnstructured(v)
		}
		return result
	case []ref.Val:
		result := make([]any, len(t))
		for i, e := range t {
			result[i] = valueToUnstructured(e)
		}
		return result
	default:
		return t
	}
}

func (rt *OpenAPITypeProvider) NewValue(typeName string, fields map[string]ref.Val) ref.Val {
	declType, found := rt.findDeclType(typeName)
	if found && declType.schema != nil {
		obj := make(map[string]any, len(fields))
		for k, v := range fields {
			obj[k] = valueToUnstructured(v)
		}
		return UnstructuredToVal(obj, declType.schema)
	}
	return rt.typeProvider.NewValue(typeName, fields)
}

// TypeNames returns the list of type names declared within the OpenAPITypeProvider object.
func (rt *OpenAPITypeProvider) TypeNames() []string {
	typeNames := make([]string, len(rt.registeredTypes))
	i := 0
	for name := range rt.registeredTypes {
		typeNames[i] = name
		i++
	}
	return typeNames
}

func (rt *OpenAPITypeProvider) findDeclType(typeName string) (*DeclType, bool) {
	declType, found := rt.registeredTypes[typeName]
	if found {
		return declType, true
	}
	declType = findScalar(typeName)
	return declType, declType != nil
}

func findScalar(typename string) *DeclType {
	switch typename {
	case BoolType.TypeName():
		return BoolType
	case BytesType.TypeName():
		return BytesType
	case DoubleType.TypeName():
		return DoubleType
	case DurationType.TypeName():
		return DurationType
	case IntType.TypeName():
		return IntType
	case NullType.TypeName():
		return NullType
	case StringType.TypeName():
		return StringType
	case TimestampType.TypeName():
		return TimestampType
	case UintType.TypeName():
		return UintType
	case ListType.TypeName():
		return ListType
	case MapType.TypeName():
		return MapType
	default:
		return nil
	}
}

func allTypesForDecl(declTypes []*DeclType) (map[string]*DeclType, error) {
	if declTypes == nil {
		return nil, nil
	}
	allTypes := map[string]*DeclType{}
	for _, declType := range declTypes {
		for k, t := range FieldTypeMap(declType.TypeName(), declType) {
			allTypes[k] = t
		}
	}

	return allTypes, nil
}
