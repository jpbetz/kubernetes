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

package patch

import (
	"context"
	"errors"
	"fmt"
	"sigs.k8s.io/structured-merge-diff/v4/fieldpath"
	"sigs.k8s.io/structured-merge-diff/v4/schema"
	"sigs.k8s.io/structured-merge-diff/v4/typed"
	"sigs.k8s.io/structured-merge-diff/v4/value"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/managedfields"
	plugincel "k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/cel/mutation/common"
)

// NewApplyConfigurationPatcher creates a patcher that performs an applyConfiguration mutation.
func NewApplyConfigurationPatcher(expressionEvaluator plugincel.Evaluator) Patcher {
	return &applyConfigPatcher{expressionEvaluator: expressionEvaluator}
}

type applyConfigPatcher struct {
	expressionEvaluator plugincel.Evaluator
}

func (e *applyConfigPatcher) Patch(ctx context.Context, r Request, runtimeCELCostBudget int64) (runtime.Object, error) {
	admissionRequest := plugincel.CreateAdmissionRequest(
		r.VersionedAttributes.Attributes,
		metav1.GroupVersionResource(r.MatchedResource),
		metav1.GroupVersionKind(r.VersionedAttributes.VersionedKind))

	compileErrors := e.expressionEvaluator.CompilationErrors()
	if len(compileErrors) > 0 {
		return nil, errors.Join(compileErrors...)
	}
	eval, _, err := e.expressionEvaluator.ForInput(ctx, r.VersionedAttributes, admissionRequest, r.OptionalVariables, r.Namespace, runtimeCELCostBudget)
	if err != nil {
		return nil, err
	}
	if eval.Error != nil {
		return nil, eval.Error
	}
	v := eval.EvalResult

	objVal, ok := v.(*common.ObjectVal)
	if !ok {
		// Should not happen since the compiler type checks return types of expressions when expressions are validated.
		return nil, fmt.Errorf("unsupported return type from ApplyConfiguration expression: %v", v.Type())
	}
	err = objVal.CheckTypeNamesMatchFieldPathNames()
	if err != nil {
		return nil, fmt.Errorf("type mismatch: %w", err)
	}

	value, ok := objVal.Value().(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid return type: %T", v)
	}

	patchObject := unstructured.Unstructured{Object: value}
	patchObject.SetGroupVersionKind(r.VersionedAttributes.VersionedObject.GetObjectKind().GroupVersionKind())
	patched, err := ApplyStructuredMergeDiff(r.TypeConverter, r.VersionedAttributes.VersionedObject, &patchObject)
	if err != nil {
		return nil, fmt.Errorf("error applying patch: %w", err)
	}
	return patched, nil
}

// ApplyStructuredMergeDiff applies a structured merge diff to an object and returns a copy of the object
// with the patch applied.
func ApplyStructuredMergeDiff(
	typeConverter managedfields.TypeConverter,
	originalObject runtime.Object,
	patch *unstructured.Unstructured,
) (runtime.Object, error) {
	if patch.GroupVersionKind() != originalObject.GetObjectKind().GroupVersionKind() {
		return nil, fmt.Errorf("patch (%v) and original object (%v) are not of the same gvk", patch.GroupVersionKind().String(), originalObject.GetObjectKind().GroupVersionKind().String())
	} else if typeConverter == nil {
		return nil, fmt.Errorf("type converter must not be nil")
	}

	patchObjTyped, err := typeConverter.ObjectToTyped(patch)
	if err != nil {
		return nil, fmt.Errorf("failed to convert patch object to typed object: %w", err)
	}

	err = validatePatch(patchObjTyped)
	if err != nil {
		return nil, fmt.Errorf("invalid ApplyConfiguration: %w", err)
	}

	liveObjTyped, err := typeConverter.ObjectToTyped(originalObject)
	if err != nil {
		return nil, fmt.Errorf("failed to convert original object to typed object: %w", err)
	}

	newObjTyped, err := liveObjTyped.Merge(patchObjTyped)
	if err != nil {
		return nil, fmt.Errorf("failed to merge patch: %w", err)
	}

	// Our mutating admission policy sets the fields but does not track ownership.
	// Newly introduced fields in the patch won't be tracked by a field manager
	// (so if the original object is updated again but the mutating policy is
	// not active, the fields will be dropped).
	//
	// This necessarily means that changes to an object by a mutating policy
	// are only preserved if the policy was active at the time of the change.
	// (If the policy is not active, the changes may be dropped.)

	newObj, err := typeConverter.TypedToObject(newObjTyped)
	if err != nil {
		return nil, fmt.Errorf("failed to convert typed object to object: %w", err)
	}

	return newObj, nil
}

// validatePatch searches an apply configuration for any arrays, maps or structs elements that are atomic and returns
// and error if any are found.
func validatePatch(v *typed.TypedValue) error {
	atomics := findAtomics(nil, v.Schema(), v.TypeRef(), v.AsValue())
	if len(atomics) > 0 {
		return fmt.Errorf("may not mutate atomic arrays, maps or structs: %v", strings.Join(atomics, ", "))
	}
	return nil
}

// findAtomics returns field paths for any atomic arrays, maps or structs found when traversing the given value.
func findAtomics(path []fieldpath.PathElement, s *schema.Schema, tr schema.TypeRef, v value.Value) (atomics []string) {
	if a, ok := s.Resolve(tr); ok { // Validation pass happens before this and checks that all schemas can be resolved
		if v.IsMap() && a.Map != nil {
			if a.Map.ElementRelationship == schema.Atomic {
				atomics = append(atomics, pathString(path))
			}
			v.AsMap().Iterate(func(key string, val value.Value) bool {
				pe := fieldpath.PathElement{FieldName: &key}
				if sf, ok := a.Map.FindField(key); ok {
					tr = sf.Type
					atomics = append(atomics, findAtomics(append(path, pe), s, tr, val)...)
				}
				return true
			})
		}
		if v.IsList() && a.List != nil {
			if a.List.ElementRelationship == schema.Atomic {
				atomics = append(atomics, pathString(path))
			}
			list := v.AsList()
			for i := 0; i < list.Length(); i++ {
				pe := fieldpath.PathElement{Index: &i}
				atomics = append(atomics, findAtomics(append(path, pe), s, a.List.ElementType, list.At(i))...)
			}
		}
	}
	return atomics
}

func pathString(path []fieldpath.PathElement) string {
	sb := strings.Builder{}
	for _, p := range path {
		sb.WriteString(p.String())
	}
	return sb.String()
}
