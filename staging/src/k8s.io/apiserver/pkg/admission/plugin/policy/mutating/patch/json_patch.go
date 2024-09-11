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
	json2 "encoding/json"
	"errors"
	"fmt"
	"github.com/go-openapi/jsonpointer"
	"google.golang.org/protobuf/types/known/structpb"
	jsonpatch "gopkg.in/evanphx/json-patch.v4"
	"reflect"
	"strconv"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/api/admissionregistration/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	plugincel "k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/cel/mutation/common"
	"k8s.io/kube-openapi/pkg/validation/spec"
	pointer "k8s.io/utils/ptr"
)

// NewJSONPatcher creates a patcher that performs a JSON Patch mutation.
func NewJSONPatcher(ops []JSONPatchOp) Patcher {
	return jsonPatcher(ops)
}

// JSONPatchOp represent an operation in a JSON Patch with a value that
// is computed from a CEL expression evaluator.
type JSONPatchOp struct {
	Patch          v1alpha1.JSONPatchOperation
	ValueEvaluator *plugincel.Evaluator
	PathEvaluator  *plugincel.Evaluator
	FromEvaluator  *plugincel.Evaluator
}

type jsonPatcher []JSONPatchOp

func (e jsonPatcher) Patch(ctx context.Context, r Request, runtimeCELCostBudget int64) (runtime.Object, error) {

	admissionRequest := plugincel.CreateAdmissionRequest(
		r.VersionedAttributes.Attributes,
		metav1.GroupVersionResource(r.MatchedResource),
		metav1.GroupVersionKind(r.VersionedAttributes.VersionedKind))

	for _, op := range e {
		if op.ValueEvaluator != nil {
			valueEvaluator := *op.ValueEvaluator
			compileErrors := valueEvaluator.CompilationErrors()
			if len(compileErrors) > 0 {
				return nil, errors.Join(compileErrors...)
			}
		}
	}

	remainingBudget := runtimeCELCostBudget
	patchObj, err := func() (jsonpatch.Patch, error) {
		result := jsonpatch.Patch{}
		for _, op := range e {
			var err error
			resultOp := jsonpatch.Operation{}
			var path, from jsonpointer.Pointer
			resultOp["op"] = pointer.To(json2.RawMessage(strconv.Quote(string(op.Patch.Op))))
			if op.PathEvaluator != nil {
				path, remainingBudget, err = e.evaluatePathExpression(*op.PathEvaluator, remainingBudget, ctx, r, admissionRequest)
				if err != nil {
					return nil, err
				}
				resultOp["path"] = pointer.To(json2.RawMessage(strconv.Quote(path.String())))
			} else {
				// path is required for all operations
				return nil, fmt.Errorf("internal error in patch operation: missing compiled pathExpression")
			}
			if op.FromEvaluator != nil {
				from, remainingBudget, err = e.evaluatePathExpression(*op.FromEvaluator, remainingBudget, ctx, r, admissionRequest)
				if err != nil {
					return nil, err
				}
				resultOp["from"] = pointer.To(json2.RawMessage(strconv.Quote(from.String())))
			}
			if op.ValueEvaluator != nil {
				var patchBytes []byte
				patchBytes, remainingBudget, err = e.evaluateValueExpression(op, remainingBudget, ctx, r, admissionRequest, path)
				if err != nil {
					return nil, err
				}
				resultOp["value"] = pointer.To[json2.RawMessage](patchBytes)
			}
			result = append(result, resultOp)
		}
		return result, nil
	}()

	if err != nil {
		return nil, err
	}

	o := r.ObjectInterfaces
	jsonSerializer := json.NewSerializerWithOptions(json.DefaultMetaFactory, o.GetObjectCreater(), o.GetObjectTyper(), json.SerializerOptions{Pretty: false, Strict: true})
	objJS, err := runtime.Encode(jsonSerializer, r.VersionedAttributes.VersionedObject)
	if err != nil {
		return nil, fmt.Errorf("failed to create JSON patch: %w", err)
	}
	patchedJS, err := patchObj.Apply(objJS)
	if err != nil {
		if errors.Is(err, jsonpatch.ErrTestFailed) {
			// If a json patch fails a test operation, the patch must not be applied
			return r.VersionedAttributes.VersionedObject, nil
		}
		return nil, fmt.Errorf("JSONPath: %w", err)
	}

	var newVersionedObject runtime.Object
	if _, ok := r.VersionedAttributes.VersionedObject.(*unstructured.Unstructured); ok {
		newVersionedObject = &unstructured.Unstructured{}
	} else {
		newVersionedObject, err = o.GetObjectCreater().New(r.VersionedAttributes.VersionedKind)
		if err != nil {
			return nil, apierrors.NewInternalError(err)
		}
	}

	if newVersionedObject, _, err = jsonSerializer.Decode(patchedJS, nil, newVersionedObject); err != nil {
		return nil, apierrors.NewInternalError(err)
	}

	return newVersionedObject, nil
}

func (e jsonPatcher) evaluateValueExpression(op JSONPatchOp, remainingBudget int64, ctx context.Context, r Request, admissionRequest *admissionv1.AdmissionRequest, path jsonpointer.Pointer) ([]byte, int64, error) {
	valueEvaluator := *op.ValueEvaluator
	var err error
	var eval plugincel.EvaluationResult
	eval, remainingBudget, err = valueEvaluator.ForInput(ctx, nil, r.VersionedAttributes, admissionRequest, r.OptionalVariables, r.Namespace, remainingBudget)
	if err != nil {
		return nil, -1, err
	}
	if eval.Error != nil {
		return nil, -1, eval.Error
	}
	refVal := eval.EvalResult
	if objVal, ok := refVal.(*common.ObjectVal); ok {
		schema, err := r.ObjectSchema()
		if err != nil {
			return nil, -1, err
		}
		if schema != nil {
			if objectTypeName := objectTypeNameAtPath(path, schema); len(objectTypeName) > 0 {
				if objVal.Type().TypeName() != objectTypeName {
					return nil, -1, fmt.Errorf("type mismatch: path %s points to type %s but valueExpression evaluates to type %s", path.String(), objectTypeName, objVal.Type().TypeName())
				}
			}
		}
		err = objVal.CheckTypeNamesMatchFieldPathNames()
		if err != nil {
			return nil, -1, fmt.Errorf("type mismatch: %w", err)
		}
	}

	// CEL data literals representing arbitrary JSON values can be serialized to JSON for use in
	// JSON Patch if first converted to pb.Value.
	v, err := refVal.ConvertToNative(reflect.TypeOf(&structpb.Value{}))
	if err != nil {
		return nil, -1, fmt.Errorf("JSONPath valueExpression evaluated to a type that could not marshal to JSON: %w", err)
	}
	b, err := json2.Marshal(v)
	if err != nil {
		return nil, -1, fmt.Errorf("JSONPath valueExpression evaluated to a type that could not marshal to JSON: %w", err)
	}
	return b, remainingBudget, nil
}

func (e jsonPatcher) evaluatePathExpression(pathEvaluator plugincel.Evaluator, remainingBudget int64, ctx context.Context, r Request, admissionRequest *admissionv1.AdmissionRequest) (jsonpointer.Pointer, int64, error) {
	var err error
	var eval plugincel.EvaluationResult
	eval, remainingBudget, err = pathEvaluator.ForInput(ctx, nil, r.VersionedAttributes, admissionRequest, r.OptionalVariables, r.Namespace, remainingBudget)
	if err != nil {
		return jsonpointer.Pointer{}, -1, err
	}
	if eval.Error != nil {
		return jsonpointer.Pointer{}, -1, eval.Error
	}
	s, ok := eval.EvalResult.Value().(string)
	if !ok {
		// Should not happen since result type is checked by type checker as string
		return jsonpointer.Pointer{}, -1, fmt.Errorf("evaluated to %T but expected string", eval.EvalResult.Value())
	}
	jsonPointer, err := jsonpointer.New(s)
	if err != nil {
		return jsonpointer.Pointer{}, -1, fmt.Errorf("failed to parse as JSON Pointer: %w", err)
	}
	return jsonPointer, remainingBudget, nil
}

func objectTypeNameAtPath(path jsonpointer.Pointer, schema *spec.Schema) string {
	sb := strings.Builder{}
	sb.WriteString("Object")
	for _, token := range path.DecodedTokens() {
		if len(schema.Properties) > 0 {
			if child, ok := schema.Properties[token]; ok {
				schema = &child
				sb.WriteString(".")
				sb.WriteString(token)
			} else {
				// TODO
			}
		} else if schema.AdditionalProperties != nil && schema.AdditionalProperties.Schema != nil {
			// Ignore the token. It's a map key, but we don't need to know more than that.
			schema = schema.AdditionalProperties.Schema

		} else if schema.Items != nil && schema.Items.Schema != nil {
			// Ignore the token. It's a map index (or a JSON pointer '-'), but we don't need to know more than that.
			schema = schema.Items.Schema
		} else {
			// Schemas are allowed to have none of these things.
			return ""
		}
	}
	return sb.String()
}
