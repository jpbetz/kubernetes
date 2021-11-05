/*
Copyright 2021 The Kubernetes Authors.

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

package cel

import (
	"fmt"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"math"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"

	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
)

// TestValidationExpressions tests CEL integration with custom resource values and OpenAPIv3.
func TestValidationExpressions(t *testing.T) {
	tests := []struct {
		name   string
		schema *schema.Structural
		obj    map[string]interface{}
		valid  []string
		errors map[string]string // rule -> string that error message must contain
	}{
		// tests where val1 and val2 are equal but val3 is different
		// equality, comparisons and type specific functions
		{name: "integers",
			obj:    objs(math.MaxInt64, math.MaxInt64, math.MaxInt32, math.MaxInt32, math.MaxInt64, math.MaxInt64),
			schema: schemas(integerType, integerType, int32Type, int32Type, int64Type, int64Type, int64Type),
			valid: []string{
				ValsEqualThemselvesAndDataLiteral("val1", "val2", fmt.Sprintf("%d", math.MaxInt64)),
				ValsEqualThemselvesAndDataLiteral("val3", "val4", fmt.Sprintf("%d", math.MaxInt32)),
				ValsEqualThemselvesAndDataLiteral("val5", "val6", fmt.Sprintf("%d", math.MaxInt64)),
				"val1 == val6", // integer with no format is the same as int64
				"type(val1) == int",
				fmt.Sprintf("val3 + 1 == %d + 1", math.MaxInt32), // CEL integers are 64 bit
				"type(val3) == int",
				"type(val5) == int",
			},
			errors: map[string]string{
				"val1 + 1 == 0": "integer overflow",
				"val5 + 1 == 0": "integer overflow",
			},
		},
		{name: "numbers",
			obj:    objs(math.MaxFloat64, math.MaxFloat64, math.MaxFloat32, math.MaxFloat32, math.MaxFloat64, math.MaxFloat64),
			schema: schemas(numberType, numberType, floatType, floatType, doubleType, doubleType),
			valid: []string{
				ValsEqualThemselvesAndDataLiteral("val1", "val2", fmt.Sprintf("%f", math.MaxFloat64)),
				ValsEqualThemselvesAndDataLiteral("val3", "val4", fmt.Sprintf("%f", math.MaxFloat32)),
				ValsEqualThemselvesAndDataLiteral("val5", "val6", fmt.Sprintf("%f", math.MaxFloat64)),
				"val1 == val6", // number with no format is the same as float64
				"type(val1) == double",
				"type(val3) == double",
				"type(val5) == double",
			},
		},
		{name: "unicode strings",
			obj:    objs("rook takes 👑", "rook takes 👑"),
			schema: schemas(stringType, stringType),
			valid: []string{
				ValsEqualThemselvesAndDataLiteral("val1", "val2", "'rook takes 👑'"),
				"val1.startsWith('rook')",
				"!val1.startsWith('knight')",
				"val1.contains('takes')",
				"!val1.contains('gives')",
				"val1.endsWith('👑')",
				"!val1.endsWith('pawn')",
				"val1.matches('^[^0-9]*$')",
				"!val1.matches('^[0-9]*$')",
				"type(val1) == string",
				"size(val1) == 12",
			},
		},
		{name: "escaped strings",
			obj:    objs("l1\nl2", "l1\nl2"),
			schema: schemas(stringType, stringType),
			valid:  []string{ValsEqualThemselvesAndDataLiteral("val1", "val2", "'l1\\nl2'")},
		},
		{name: "bytes",
			obj:    objs("QUI=", "QUI="),
			schema: schemas(byteType, byteType),
			valid: []string{
				"val1 == val2",
				"val1 == 'QUI='",
				"type(val1) == string",
				"size(val1) == 4",
			},
		},
		{name: "binary",
			obj:    objs([]byte("\x41\x42"), []byte("AB")),
			schema: schemas(binaryType, binaryType),
			valid: []string{
				"val1 == val2",
				"val1 == b'AB'",
				"val1 == bytes('AB')",
				"val1 == b'\\x41\\x42'",
				"type(val1) == bytes",
				"size(val1) == 2",
			},
		},
		{name: "booleans",
			obj:    objs(true, true, false, false),
			schema: schemas(booleanType, booleanType, booleanType, booleanType),
			valid: []string{
				ValsEqualThemselvesAndDataLiteral("val1", "val2", "true"),
				ValsEqualThemselvesAndDataLiteral("val3", "val4", "false"),
				"val1 != val4",
				"type(val1) == bool",
			},
		},
		{name: "durations",
			obj:    objs("1h2m3s4ms", "1h2m3s4ms"),
			schema: schemas(durationType, durationType),
			valid: []string{
				"val1 == duration('1h2m3s4ms')",
				"val1 == duration('1h2m') + duration('3s4ms')",
				"val1.getHours() == 1",
				"val1.getMinutes() == 62",
				"val1.getSeconds() == 3723",
				"val1.getMilliseconds() == 3723004",
				"type(val1) == google.protobuf.Duration",
			},
		},
		{name: "dates",
			obj:    objs("1997-07-16", "1997-07-16"),
			schema: schemas(dateType, dateType),
			valid: []string{
				"val1.getDate() == 16",
				"val1.getMonth() == 06", // zero based indexing
				"val1.getFullYear() == 1997",
				"type(val1) == google.protobuf.Timestamp",
			},
		},
		{name: "datetimes",
			obj:    objs("2011-08-18T19:03:37.010000000+01:00", "2011-08-18T19:03:37.010000000+01:00"),
			schema: schemas(dateTimeType, dateTimeType),
			valid: []string{
				"val1 == timestamp('2011-08-18T19:03:37.010+01:00')",
				"val1 == timestamp('2011-08-18T00:00:00.000+01:00') + duration('19h3m37s10ms')",
				"val1.getDate('01:00') == 18",
				"val1.getMonth('01:00') == 7", // zero based indexing
				"val1.getFullYear('01:00') == 2011",
				"val1.getHours('01:00') == 19",
				"val1.getMinutes('01:00') == 03",
				"val1.getSeconds('01:00') == 37",
				"val1.getMilliseconds('01:00') == 10",
				"val1.getHours('UTC') == 18", // TZ in string is 1hr off of UTC
				"type(val1) == google.protobuf.Timestamp",
			},
		},

		// 1997-07-16T19:20:30+01:00
		{name: "conversions",
			obj:    objs(int64(10), 10.0, 10.49, 10.5, true, "10", []byte("10")),
			schema: schemas(integerType, numberType, numberType, numberType, booleanType, stringType, binaryType),
			valid: []string{
				"int(val2) == val1",
				"int(val3) == val1",
				"int(val4) == val1",
				"int(val6) == val1",
				"double(val1) == val2",
				"double(val6) == val2",
				"bytes(val6) == val7",
				"string(val1) == val6",
				"string(val2) == '10'",
				"string(val3) == '10.49'",
				"string(val4) == '10.5'",
				"string(val5) == 'true'",
				"string(val7) == val6",
			},
		},
		{name: "lists",
			obj:    objs([]interface{}{1, 2, 3}, []interface{}{1, 2, 3}),
			schema: schemas(listType(&integerType), listType(&integerType)),
			valid: []string{
				ValsEqualThemselvesAndDataLiteral("val1", "val2", "[1, 2, 3]"),
				"1 in val1",
				"val2[0] in val1",
				"!(0 in val1)",
			},
		},
		{name: "listSets",
			obj:    objs([]interface{}{"a", "b", "c"}, []interface{}{"a", "c", "b"}),
			schema: schemas(listSetType(&stringType), listSetType(&stringType)),
			valid: []string{
				// equal even though order is different
				"val1 == ['c', 'b', 'a']",
				"val1 == val2",
				"'a' in val1",
				"val2[0] in val1",
				"!('x' in val1)",
			},
		},
		{name: "listMaps",
			obj: map[string]interface{}{
				"objs": []interface{}{
					[]interface{}{
						map[string]interface{}{"k": "a", "v": '1'},
						map[string]interface{}{"k": "b", "v": '2'},
					},
					[]interface{}{
						map[string]interface{}{"k": "b", "v": '2'},
						map[string]interface{}{"k": "a", "v": '1'},
					},
				},
			},
			schema: objectType(map[string]schema.Structural{
				"objs": listType(&kvListMapType),
			}),
			valid: []string{
				"objs[0] == objs[1]", // equal even though order is different
			},
			errors: map[string]string{
				"objs[0] == {'k': 'a', 'v': '1'}": "no matching overload for '_==_'", // objects cannot be compared against a data literal map
			},
		},
		{name: "maps",
			obj:    objs(map[string]interface{}{"k1": "a", "k2": 'b'}, map[string]interface{}{"k2": 'b', "k1": "a"}),
			schema: schemas(mapType(&stringType), mapType(&stringType)),
			valid: []string{
				"val1 == val2", // equal even though order is different
				"'k1' in val1",
				"!('k3' in val1)",
			},
		},
		{name: "objects",
			obj: map[string]interface{}{
				"objs": []interface{}{
					map[string]interface{}{"f1": "a", "f2": "b"},
					map[string]interface{}{"f1": "a", "f2": "b"},
				},
			},
			schema: objectType(map[string]schema.Structural{
				"objs": listType(objectType(map[string]schema.Structural{
					"f1": stringType,
					"f2": stringType,
				})),
			}),
			valid: []string{
				"objs[0] == objs[1]",
			},
			errors: map[string]string{
				"objs[0] == {'f1': 'a', 'f2': 'b'}": "no matching overload for '_==_'", // objects cannot be compared against a data literal map
			},
		},

		{name: "object access",
			obj: map[string]interface{}{
				"a": map[string]interface{}{
					"b": 1,
					"d": 2,
				},
				"a1": map[string]interface{}{
					"b1": map[string]interface{}{
						"c1": 4,
					},
				},
				"a3": map[string]interface{}{},
			},
			schema: objectType(map[string]schema.Structural{
				"a": *objectType(map[string]schema.Structural{
					"b": integerType,
					"c": integerType,
					"d": required(integerType),
					"e": withDefault(100, integerType),
				}),
				"a1": *objectType(map[string]schema.Structural{
					"b1": *objectType(map[string]schema.Structural{
						"c1": integerType,
					}),
					"d2": *objectType(map[string]schema.Structural{
						"e2": integerType,
					}),
				}),
				"a3": *objectType(map[string]schema.Structural{
					"b3": withDefault(map[string]interface{}{"c3": 101}, *objectType(map[string]schema.Structural{
						"c3": integerType,
					})),
				}),
			}),
			// https://github.com/google/cel-spec/blob/master/doc/langdef.md#field-selection
			valid: []string{
				"has(a.b)",
				"!has(a.c)",
				"has(a.d)",
				//"has(a.e)", // TODO: is defaulting before validation?
				"has(a1.b1.c1)",
				"!(has(a1.d2) && has(a1.d2.e2))", // must check intermediate optional fields (see below no such key error for d2)
				"!has(a1.d2)",
				// "has(a3.b3.c3)", // TODO: is defaulting before validation?
			},
			errors: map[string]string{
				"has(a.z)":      "undefined field 'z'",          // may not reference undefined fields, not even in has
				"a['b'] == 1":   "matching overload for '_[_]'", // only allowed on maps, not objects
				"has(a1.d2.e2)": "no such key: d2",              // has only checks last element in path, when d2 is absent in value, this is an error
			},
		},
		{name: "map access",
			obj: map[string]interface{}{
				"val": map[string]interface{}{
					"b": 1,
					"d": 2,
				},
			},
			schema: objectType(map[string]schema.Structural{
				"val": mapType(&integerType),
			}),
			valid: []string{
				"!has(val.a)",
				"has(val.b)",
				"!has(val.c)",
				"has(val.d)",
				"val.all(k, val[k] > 0)",
				"val.exists(k, val[k] > 1)",
				"val.exists_one(k, val[k] == 2)",
				"!val.exists(k, val[k] > 2)",
				"!val.exists_one(k, val[k] > 0)",
				"size(val) == 2",
				"val.map(k, val[k]).exists(v, v == 1)",
				"size(val.filter(k, val[k] > 1)) == 1",
			},
			errors: map[string]string{
				"val['c'] == 1": "no such key: c",
			},
		},
		{name: "listMap access",
			obj: map[string]interface{}{
				"listMap": []interface{}{
					map[string]interface{}{"k": "a1", "v": "b1"},
					map[string]interface{}{"k": "a2", "v": "b2"},
					map[string]interface{}{"k": "a3", "v": "b3", "v2": "z"},
				},
			},
			schema: objectType(map[string]schema.Structural{
				"listMap": listMapType([]string{"k"}, objectType(map[string]schema.Structural{
					"k":  stringType,
					"v":  stringType,
					"v2": stringType,
				})),
			}),
			valid: []string{
				"has(listMap[0].v)",
				"listMap.all(m, m.k.startsWith('a'))",
				"listMap.all(m, !has(m.v2) || m.v2 == 'z')",
				"listMap.exists(m, m.k.endsWith('1'))",
				"listMap.exists_one(m, m.k == 'a3')",
				"!listMap.all(m, m.k.endsWith('1'))",
				"!listMap.exists(m, m.v == 'x')",
				"!listMap.exists_one(m, m.k.startsWith('a'))",
				"size(listMap.filter(m, m.k == 'a1')) == 1",
				"listMap.exists(m, m.k == 'a1' && m.v == 'b1')",
				"listMap.map(m, m.v).exists(v, v == 'b1')",
			},
		},
		{name: "list access",
			obj: map[string]interface{}{
				"array": []interface{}{1, 1, 2, 2, 3, 3, 4, 5},
			},
			schema: objectType(map[string]schema.Structural{
				"array": listType(&integerType),
			}),
			valid: []string{
				"array.all(e, e > 0)",
				"array.exists(e, e > 2)",
				"array.exists_one(e, e > 4)",
				"!array.all(e, e < 2)",
				"!array.exists(e, e < 0)",
				"!array.exists_one(e, e == 2)",
				"array.all(e, e < 100)",
				"size(array.filter(e, e%2 == 0)) == 3",
				"array.map(e, e * 20).filter(e, e > 50).exists(e, e == 60)",
				"size(array) == 8",
			},
		},
		{name: "listSet access",
			obj: map[string]interface{}{
				"set": []interface{}{1, 2, 3, 4, 5},
			},
			schema: objectType(map[string]schema.Structural{
				"set": listType(&integerType),
			}),
			valid: []string{
				"set.all(e, e > 0)",
				"set.exists(e, e > 3)",
				"set.exists_one(e, e == 3)",
				"!set.all(e, e < 3)",
				"!set.exists(e, e < 0)",
				"!set.exists_one(e, e > 3)",
				"set.all(e, e < 10)",
				"size(set.filter(e, e%2 == 0)) == 2",
				"set.map(e, e * 20).filter(e, e > 50).exists_one(e, e == 60)",
				"size(set) == 5",
			},
		},

		// Kubernetes special types
		{name: "embedded object",
			obj: map[string]interface{}{
				"embedded": map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Pod",
					"metadata": map[string]interface{}{
						"name":         "foo",
						"generateName": "pickItForMe",
						"namespace":    "xyz",
					},
					"spec": map[string]interface{}{
						"field1": "a",
					},
				},
			},
			schema: objectType(map[string]schema.Structural{
				"embedded":    embeddedType(),
				"maxReplicas": integerType,
			}),
			valid: []string{
				"embedded.kind == 'Pod'",
				"embedded.apiVersion == 'v1'",
				"embedded.metadata.name == 'foo'",
				"embedded.metadata.generateName == 'pickItForMe'",
			},
			errors: map[string]string{
				"has(embedded.metadata.namespace)": "undefined field 'namespace'",
				"has(embedded.spec)":               "undefined field 'spec'",
			},
		},
		{name: "string in intOrString",
			obj: map[string]interface{}{
				"something": map[string]interface{}{
					"strVal": "100m",
				},
			},
			schema: objectType(map[string]schema.Structural{
				"something": intOrStringType(),
			}),
			valid: []string{"has(something.strVal) && something.strVal == '100m'"},
		},
		{name: "int in intOrString",
			obj: map[string]interface{}{
				"something": map[string]interface{}{
					"intVal": 1,
				},
			},
			schema: objectType(map[string]schema.Structural{
				"something": intOrStringType(),
			}),
			valid: []string{"has(something.intVal) && something.intVal == 1"},
		},
		{name: "unknown",
			obj: map[string]interface{}{
				"unknown": map[string]interface{}{
					"field1": "a",
					"field2": "b",
				},
			},
			schema: objectType(map[string]schema.Structural{
				"unknown": unknownType(),
			}),
			errors: map[string]string{
				"has(self.field1)": "undefined field 'field1'",
			},
		},
		{name: "known and unknown fields",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"minReplicas": 1,
					"maxReplicas": 2,
					"field1":      "a",
				},
			},
			schema: &schema.Structural{
				Generic: schema.Generic{
					Type: "object",
				},
				Properties: map[string]schema.Structural{
					"spec": {
						Generic: schema.Generic{Type: "object"},
						Extensions: schema.Extensions{
							XPreserveUnknownFields: true,
						},
						Properties: map[string]schema.Structural{
							"minReplicas": {
								Generic: schema.Generic{
									Type: "integer",
								},
							},
							"maxReplicas": {
								Generic: schema.Generic{
									Type: "integer",
								},
							},
						},
					},
				},
			},
			valid: []string{"spec.minReplicas < spec.maxReplicas"},
			errors: map[string]string{
				"has(spec.field1)": "undefined field 'field1'",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, validRule := range tt.valid {
				s := withRule(*tt.schema, validRule)
				celValidator := NewValidator(&s)
				errs := celValidator.Validate(field.NewPath("root"), &s, tt.obj)
				for _, err := range errs {
					t.Errorf("unexpected error: %v", err)
				}
			}
			for rule, expectErrToContain := range tt.errors {
				s := withRule(*tt.schema, rule)
				celValidator := NewValidator(&s)
				errs := celValidator.Validate(field.NewPath("root"), &s, tt.obj)
				if len(errs) == 0 {
					t.Error("expected validation errors but got none")
				}
				for _, err := range errs {
					if err.Type != field.ErrorTypeInvalid || !strings.Contains(err.Error(), expectErrToContain) {
						t.Errorf("expected error to contain '%s', but got: %v", expectErrToContain, err)
					}
				}
			}

		})
	}
}

func primitiveType(typ, format string) schema.Structural {
	result := schema.Structural{
		Generic: schema.Generic{
			Type: typ,
		},
	}
	if len(format) != 0 {
		result.ValueValidation = &schema.ValueValidation{
			Format: format,
		}
	}
	return result
}

var (
	integerType = primitiveType("integer", "")
	int32Type   = primitiveType("integer", "int32")
	int64Type   = primitiveType("integer", "int64")
	numberType  = primitiveType("number", "")
	floatType   = primitiveType("number", "float")
	doubleType  = primitiveType("number", "double")
	stringType  = primitiveType("string", "")
	byteType    = primitiveType("string", "byte")
	binaryType  = primitiveType("string", "binary")
	booleanType = primitiveType("boolean", "")

	durationType = primitiveType("string", "duration")
	dateType     = primitiveType("string", "date")
	dateTimeType = primitiveType("string", "date-time")
)

func listType(items *schema.Structural) schema.Structural {
	return arrayType("atomic", nil, items)
}

func listSetType(items *schema.Structural) schema.Structural {
	return arrayType("set", nil, items)
}

func listMapType(keys []string, items *schema.Structural) schema.Structural {
	return arrayType("map", keys, items)
}

func arrayType(listType string, keys []string, items *schema.Structural) schema.Structural {
	result := schema.Structural{
		Generic: schema.Generic{
			Type: "array",
		},
		Extensions: schema.Extensions{
			XListType: &listType,
		},
		Items: items,
	}
	if len(keys) > 0 && listType == "map" {
		result.Extensions.XListMapKeys = keys
	}
	return result
}

func ValsEqualThemselvesAndDataLiteral(val1, val2 string, dataLiteral string) string {
	return fmt.Sprintf("%s == %s && %s == %s && %s == %s", val1, dataLiteral, dataLiteral, val1, val1, val2)
}

func objs(val ...interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(val))
	for i, v := range val {
		result[fmt.Sprintf("val%d", i+1)] = v
	}
	return result
}

func schemas(valSchema ...schema.Structural) *schema.Structural {
	result := make(map[string]schema.Structural, len(valSchema))
	for i, v := range valSchema {
		result[fmt.Sprintf("val%d", i+1)] = v
	}
	return objectType(result)
}

func objectType(props map[string]schema.Structural) *schema.Structural {
	result := schema.Structural{
		Generic: schema.Generic{
			Type: "object",
		},
		Properties: props,
	}
	return &result
}

func mapType(valSchema *schema.Structural) schema.Structural {
	result := schema.Structural{
		Generic: schema.Generic{
			Type:                 "object",
			AdditionalProperties: &schema.StructuralOrBool{Bool: true, Structural: valSchema},
		},
	}
	return result
}

func intOrStringType() schema.Structural {
	return schema.Structural{
		Generic: schema.Generic{Type: "object"},
		Extensions: schema.Extensions{
			XIntOrString: true,
		},
	}
}

func embeddedType() schema.Structural {
	return schema.Structural{
		Generic: schema.Generic{Type: "object"},
		Extensions: schema.Extensions{
			XEmbeddedResource: true,
		},
	}
}

func unknownType() schema.Structural {
	return schema.Structural{
		Generic: schema.Generic{Type: "object"},
		Extensions: schema.Extensions{
			XPreserveUnknownFields: true,
		},
	}
}

func withRule(s schema.Structural, rule string) schema.Structural {
	s.Extensions.XValidations = apiextensions.ValidationRules{
		{
			Rule: rule,
		},
	}
	return s
}

func withDefault(dflt interface{}, s schema.Structural) schema.Structural {
	s.Generic.Default = schema.JSON{Object: dflt}
	return s
}

func required(s schema.Structural) schema.Structural {
	s.Generic.Nullable = true
	return s
}

var (
	kvListMapType = listMapType([]string{"k"}, objectType(map[string]schema.Structural{"k": stringType, "v": stringType}))
)
