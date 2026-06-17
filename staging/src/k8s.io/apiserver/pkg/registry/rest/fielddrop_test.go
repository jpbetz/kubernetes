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

package rest

import (
	"context"
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/operation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
)

// TestFeatureGateOptionsAndDrop covers the gate-backed-option assembly
// (declarativeRequestConfig over InUseFeaturesForObject) and the convert -> drop ->
// write-back round-trip (DropFieldsDeclaratively), with the gate's option driving
// both. A stub registration stands in for the generated code; RestartPolicy stands
// in for a gated field guarded by a real feature gate.
func TestFeatureGateOptionsAndDrop(t *testing.T) {
	internalGV := schema.GroupVersion{Group: "", Version: runtime.APIVersionInternal}
	v1GV := schema.GroupVersion{Group: "", Version: "v1"}
	gate := string(features.ManifestBasedAdmissionControlConfig)

	ctx := genericapirequest.WithRequestInfo(context.Background(), &genericapirequest.RequestInfo{APIGroup: "", APIVersion: "v1"})

	run := func(t *testing.T, gateOn bool, old *Pod) *Pod {
		featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.ManifestBasedAdmissionControlConfig, gateOn)
		dropper := DeclarativeFieldDropper{Scheme: newPodFeatureGateScheme(t, gate)}
		obj := &Pod{RestartPolicy: "Always"}
		var oldObj runtime.Object
		opType := operation.Create
		if old != nil {
			oldObj = old
			opType = operation.Update
		}
		config, err := declarativeRequestConfig(ctx, dropper, obj, oldObj)
		if err != nil {
			t.Fatal(err)
		}
		dropper.DropFieldsDeclaratively(ctx, obj, oldObj, opType, config)
		return obj
	}

	t.Run("gate off, not in use: option absent, field dropped", func(t *testing.T) {
		if got := run(t, false, nil); got.RestartPolicy != "" {
			t.Fatalf("expected dropped, got %q", got.RestartPolicy)
		}
	})
	t.Run("gate on: option present, field kept", func(t *testing.T) {
		if got := run(t, true, nil); got.RestartPolicy != "Always" {
			t.Fatalf("expected kept, got %q", got.RestartPolicy)
		}
	})
	t.Run("gate off, in use: ratcheted, field kept", func(t *testing.T) {
		if got := run(t, false, &Pod{RestartPolicy: "Always"}); got.RestartPolicy != "Always" {
			t.Fatalf("expected kept (ratchet), got %q", got.RestartPolicy)
		}
	})

	t.Run("no feature-gate info registered: no options, no drop", func(t *testing.T) {
		scheme := runtime.NewScheme()
		scheme.AddKnownTypes(internalGV, &Pod{})
		scheme.AddKnownTypes(v1GV, &v1.Pod{})
		dropper := DeclarativeFieldDropper{Scheme: scheme}
		obj := &Pod{RestartPolicy: "Always"}
		inUse, notInUse, err := dropper.InUseFeaturesForObject(ctx, obj, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(inUse) != 0 || len(notInUse) != 0 {
			t.Fatalf("expected no features, got inUse=%v notInUse=%v", inUse, notInUse)
		}
		dropper.DropFieldsDeclaratively(ctx, obj, nil, operation.Create, DeclarativeRequestConfig{})
		if obj.RestartPolicy != "Always" {
			t.Fatalf("expected no-op, got %q", obj.RestartPolicy)
		}
	})
}

// newPodFeatureGateScheme builds a scheme with the internal/v1 Pod test types,
// their conversions, and feature-gate support that guards v1.Pod.Spec.RestartPolicy
// behind the named gate (in use when RestartPolicy is set in the old object).
func newPodFeatureGateScheme(t *testing.T, gate string) *runtime.Scheme {
	t.Helper()
	internalGV := schema.GroupVersion{Group: "", Version: runtime.APIVersionInternal}
	v1GV := schema.GroupVersion{Group: "", Version: "v1"}
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(internalGV, &Pod{})
	scheme.AddKnownTypes(v1GV, &v1.Pod{})
	if err := scheme.AddConversionFunc(&Pod{}, &v1.Pod{}, func(a, b interface{}, scope conversion.Scope) error {
		in, out := a.(*Pod), b.(*v1.Pod)
		out.ObjectMeta = in.ObjectMeta
		out.Spec.RestartPolicy = v1.RestartPolicy(in.RestartPolicy)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := scheme.AddConversionFunc(&v1.Pod{}, &Pod{}, func(a, b interface{}, scope conversion.Scope) error {
		in, out := a.(*v1.Pod), b.(*Pod)
		out.ObjectMeta = in.ObjectMeta
		out.RestartPolicy = string(in.Spec.RestartPolicy)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	scheme.AddFeatureGateFuncs(&v1.Pod{},
		func(oldObject interface{}) (inUse, notInUse []string) {
			if oldObject != nil && oldObject.(*v1.Pod).Spec.RestartPolicy != "" {
				return []string{gate}, nil
			}
			return nil, []string{gate}
		},
		func(op operation.Operation, object interface{}) {
			if !op.HasOption(gate) {
				object.(*v1.Pod).Spec.RestartPolicy = ""
			}
		})
	return scheme
}

// stubCreateUpdate supplies no-op RESTCreateStrategy/RESTUpdateStrategy methods so
// a test type can be driven through BeforeCreate/BeforeUpdate. The embedding type
// supplies runtime.ObjectTyper.
type stubCreateUpdate struct{}

func (stubCreateUpdate) NamespaceScoped() bool                                    { return false }
func (stubCreateUpdate) GenerateName(base string) string                          { return base }
func (stubCreateUpdate) PrepareForCreate(context.Context, runtime.Object)         {}
func (stubCreateUpdate) Validate(context.Context, runtime.Object) field.ErrorList { return nil }
func (stubCreateUpdate) WarningsOnCreate(context.Context, runtime.Object) []string {
	return nil
}
func (stubCreateUpdate) Canonicalize(runtime.Object)                                      {}
func (stubCreateUpdate) AllowCreateOnUpdate(context.Context) bool                         { return false }
func (stubCreateUpdate) PrepareForUpdate(context.Context, runtime.Object, runtime.Object) {}
func (stubCreateUpdate) ValidateUpdate(context.Context, runtime.Object, runtime.Object) field.ErrorList {
	return nil
}
func (stubCreateUpdate) WarningsOnUpdate(context.Context, runtime.Object, runtime.Object) []string {
	return nil
}
func (stubCreateUpdate) AllowUnconditionalUpdate(context.Context) bool { return true }

// dropOnlyStrategy opts into declarative field dropping but NOT declarative
// validation: it implements DeclarativeDropFieldsStrategy and InUseFeatureDetector
// (via the DeclarativeFieldDropper) but deliberately does not implement
// ValidateDeclaratively. It does not embed the dropper (whose promoted Scheme
// methods would collide with the stub), delegating ObjectTyper explicitly instead.
type dropOnlyStrategy struct {
	stubCreateUpdate
	scheme  *runtime.Scheme
	dropper DeclarativeFieldDropper
}

func (s dropOnlyStrategy) ObjectKinds(obj runtime.Object) ([]schema.GroupVersionKind, bool, error) {
	return s.scheme.ObjectKinds(obj)
}
func (s dropOnlyStrategy) Recognizes(gvk schema.GroupVersionKind) bool {
	return s.scheme.Recognizes(gvk)
}
func (s dropOnlyStrategy) InUseFeaturesForObject(ctx context.Context, obj, oldObj runtime.Object) (inUse, notInUse []string, err error) {
	return s.dropper.InUseFeaturesForObject(ctx, obj, oldObj)
}
func (s dropOnlyStrategy) DropFieldsDeclaratively(ctx context.Context, obj, oldObj runtime.Object, opType operation.Type, config DeclarativeRequestConfig) {
	s.dropper.DropFieldsDeclaratively(ctx, obj, oldObj, opType, config)
}

// fullStrategy implements both declarative validation (via the embedded
// DeclarativeValidation) and dropping plus in-use detection (via the
// DeclarativeFieldDropper), alongside the create/update strategy stubs.
type fullStrategy struct {
	stubCreateUpdate
	DeclarativeValidation
	dropper DeclarativeFieldDropper
}

func (s fullStrategy) InUseFeaturesForObject(ctx context.Context, obj, oldObj runtime.Object) (inUse, notInUse []string, err error) {
	return s.dropper.InUseFeaturesForObject(ctx, obj, oldObj)
}
func (s fullStrategy) DropFieldsDeclaratively(ctx context.Context, obj, oldObj runtime.Object, opType operation.Type, config DeclarativeRequestConfig) {
	s.dropper.DropFieldsDeclaratively(ctx, obj, oldObj, opType, config)
}

var (
	_ RESTCreateStrategy            = dropOnlyStrategy{}
	_ RESTUpdateStrategy            = dropOnlyStrategy{}
	_ DeclarativeDropFieldsStrategy = dropOnlyStrategy{}
	_ InUseFeatureDetector          = dropOnlyStrategy{}
	_ RESTCreateStrategy            = fullStrategy{}
	_ DeclarativeValidationStrategy = fullStrategy{}
	_ DeclarativeDropFieldsStrategy = fullStrategy{}
	_ InUseFeatureDetector          = fullStrategy{}
)

// TestBeforeCreateUpdateDropWithoutValidation verifies that a strategy which
// implements declarative field dropping but not declarative validation still
// receives the computed gate-backed options. BeforeCreate/BeforeUpdate must read
// the config from the configurer, not from the validation strategy; before the fix
// dvConfig was zero-valued for such a strategy and every gated field was dropped
// regardless of gate state.
func TestBeforeCreateUpdateDropWithoutValidation(t *testing.T) {
	gate := string(features.ManifestBasedAdmissionControlConfig)
	scheme := newPodFeatureGateScheme(t, gate)
	strategy := dropOnlyStrategy{scheme: scheme, dropper: DeclarativeFieldDropper{Scheme: scheme}}

	if _, ok := interface{}(strategy).(DeclarativeDropFieldsStrategy); !ok {
		t.Fatal("dropOnlyStrategy must implement DeclarativeDropFieldsStrategy")
	}
	if _, ok := interface{}(strategy).(DeclarativeValidationStrategy); ok {
		t.Fatal("dropOnlyStrategy must NOT implement DeclarativeValidationStrategy")
	}

	ctx := genericapirequest.WithRequestInfo(context.Background(), &genericapirequest.RequestInfo{APIGroup: "", APIVersion: "v1"})
	ctx = genericapirequest.WithNamespace(ctx, metav1.NamespaceNone)
	newPod := func() *Pod {
		return &Pod{ObjectMeta: metav1.ObjectMeta{Name: "testpod", UID: "uid-1"}, RestartPolicy: "Always"}
	}

	t.Run("create gate on keeps field", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.ManifestBasedAdmissionControlConfig, true)
		obj := newPod()
		if err := BeforeCreate(strategy, ctx, obj); err != nil {
			t.Fatal(err)
		}
		if obj.RestartPolicy != "Always" {
			t.Fatalf("RestartPolicy = %q, want kept", obj.RestartPolicy)
		}
	})
	t.Run("create gate off drops field", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.ManifestBasedAdmissionControlConfig, false)
		obj := newPod()
		if err := BeforeCreate(strategy, ctx, obj); err != nil {
			t.Fatal(err)
		}
		if obj.RestartPolicy != "" {
			t.Fatalf("RestartPolicy = %q, want dropped", obj.RestartPolicy)
		}
	})
	t.Run("update gate off, not in use, drops field", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.ManifestBasedAdmissionControlConfig, false)
		obj, old := newPod(), newPod()
		obj.ResourceVersion, old.ResourceVersion = "1", "1"
		old.RestartPolicy = ""
		if err := BeforeUpdate(strategy, ctx, obj, old); err != nil {
			t.Fatal(err)
		}
		if obj.RestartPolicy != "" {
			t.Fatalf("RestartPolicy = %q, want dropped", obj.RestartPolicy)
		}
	})
	t.Run("update gate off, in use, keeps field (ratchet)", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.ManifestBasedAdmissionControlConfig, false)
		obj, old := newPod(), newPod() // old has RestartPolicy set -> in use
		obj.ResourceVersion, old.ResourceVersion = "1", "1"
		if err := BeforeUpdate(strategy, ctx, obj, old); err != nil {
			t.Fatal(err)
		}
		if obj.RestartPolicy != "Always" {
			t.Fatalf("RestartPolicy = %q, want kept (ratchet)", obj.RestartPolicy)
		}
	})
}

// TestDropIndependentOfDeclarativeValidationGate verifies that feature-gate field
// dropping is governed by the field's own gate, not by the declarative-validation
// migration machinery: a field drops (or is kept) the same way regardless of the
// DeclarativeValidationBeta gate. (DeclarativeValidation itself is GA and locked on
// at this version, so only the beta migration gate is toggled here.)
func TestDropIndependentOfDeclarativeValidationGate(t *testing.T) {
	gate := string(features.ManifestBasedAdmissionControlConfig)
	scheme := newPodFeatureGateScheme(t, gate)
	strategy := fullStrategy{DeclarativeValidation: DeclarativeValidation{Scheme: scheme}, dropper: DeclarativeFieldDropper{Scheme: scheme}}

	ctx := genericapirequest.WithRequestInfo(context.Background(), &genericapirequest.RequestInfo{APIGroup: "", APIVersion: "v1"})
	ctx = genericapirequest.WithNamespace(ctx, metav1.NamespaceNone)
	newPod := func() *Pod {
		return &Pod{ObjectMeta: metav1.ObjectMeta{Name: "testpod", UID: "uid-1"}, RestartPolicy: "Always"}
	}

	for _, betaOn := range []bool{false, true} {
		t.Run(fmt.Sprintf("declarativeValidationBeta=%v: field gate off drops", betaOn), func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeclarativeValidationBeta, betaOn)
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.ManifestBasedAdmissionControlConfig, false)
			obj := newPod()
			if err := BeforeCreate(strategy, ctx, obj); err != nil {
				t.Fatal(err)
			}
			if obj.RestartPolicy != "" {
				t.Fatalf("RestartPolicy = %q, want dropped regardless of DeclarativeValidationBeta=%v", obj.RestartPolicy, betaOn)
			}
		})
		t.Run(fmt.Sprintf("declarativeValidationBeta=%v: field gate on keeps", betaOn), func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeclarativeValidationBeta, betaOn)
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.ManifestBasedAdmissionControlConfig, true)
			obj := newPod()
			if err := BeforeCreate(strategy, ctx, obj); err != nil {
				t.Fatal(err)
			}
			if obj.RestartPolicy != "Always" {
				t.Fatalf("RestartPolicy = %q, want kept regardless of DeclarativeValidationBeta=%v", obj.RestartPolicy, betaOn)
			}
		})
	}
}
