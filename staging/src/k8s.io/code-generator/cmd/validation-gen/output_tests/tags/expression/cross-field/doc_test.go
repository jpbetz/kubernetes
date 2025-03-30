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

package cross_field

import (
	"context"
	"k8s.io/apimachinery/pkg/api/operation"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

func Test(t *testing.T) {
	st := localSchemeBuilder.Test(t)

	st.Value(&Root{Struct: Struct{S: "x", I: 10}}).ExpectValid()

	st.Value(&Root{Struct: Struct{S: "xyz", I: 2}}).ExpectInvalid(
		field.Invalid(field.NewPath("struct"), Struct{S: "xyz", I: 2, B: false, F: 0}, "the length of s (3) must be less than i (2)"),
	)
}

// 3019 ns/op (cost enabled), 556.0 ns/op (cost disabled)
// Note that disabling cost disables CEL tracking and avoids per-invocation factory initialization:
// https://github.com/kubernetes/kubernetes/blob/047e4c8e56b5c6a0466e4f1f1fdeca7e9a8de3d6/vendor/github.com/google/cel-go/cel/program.go#L225
func BenchmarkExpression(b *testing.B) {
	obj := Struct{S: "x", I: 10}

	// force compile and then reset to ignore compilation cost
	Validate_Struct(context.Background(), operation.Operation{Type: operation.Create}, nil, &obj, nil)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		Validate_Struct(context.Background(), operation.Operation{Type: operation.Create}, nil, &obj, nil)
	}
}

// 52.13 ns/op
func BenchmarkNative(b *testing.B) {
	obj := Struct{S: "x", I: 10}
	for i := 0; i < b.N; i++ {
		Validate_Struct_Native(context.Background(), operation.Operation{Type: operation.Create}, nil, &obj, nil)
	}
}

func Validate_Struct_Native(ctx context.Context, op operation.Operation, fldPath *field.Path, obj, oldObj *Struct) (errs field.ErrorList) {
	if len(obj.S) < obj.I {
		errs = field.ErrorList{field.Invalid(nil, obj, "expression returned false")}
	}
	return errs
}
