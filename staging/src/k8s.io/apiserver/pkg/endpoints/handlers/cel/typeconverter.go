/*
Copyright 2022 The Kubernetes Authors.

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
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ExpressionRuntime compiles and runs CEL expressions.
type ExpressionRuntime interface {
	Eval(program cel.Program, object runtime.Object) (ref.Val, error)
	Compile(rule string, kind schema.GroupVersionKind) (cel.Program, error)
}
