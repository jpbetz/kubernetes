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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/featuregate"
)

// DeclarativeStrategyConfigurer supplies a strategy's per-request declarative
// configuration.
type DeclarativeStrategyConfigurer interface {
	// DeclarativeRequestConfig configures declarative validation for a single request.
	DeclarativeRequestConfig(ctx context.Context, obj, oldObj runtime.Object) DeclarativeRequestConfig
}

// InUseFeatureDetector partitions an object's feature gates by whether the gated
// field is already in use, so a caller can ratchet in-use features independently of
// whether their gate is enabled.
type InUseFeatureDetector interface {
	// InUseFeaturesForObject returns obj's feature gates partitioned into inUse (a
	// gated field is set in oldObj) and notInUse (the rest). oldObj is nil on create.
	InUseFeaturesForObject(ctx context.Context, obj, oldObj runtime.Object) (inUse, notInUse []string, err error)
}

// DeclarativeRequestConfig holds configuration for a single declarative request,
// shared by declarative validation and feature-gate field dropping. Strategies
// customize it by implementing DeclarativeStrategyConfigurer.
type DeclarativeRequestConfig struct {
	// Options contains validation options that declarative validation tags
	// expect. Options is expected to contain feature gate names for features
	// that are active for this object. Active features are features active for
	// the request object or features already in-use by the request object.
	Options []string

	// NormalizationRules are applied to field paths when comparing
	// handwritten and declarative validation errors.
	NormalizationRules []field.NormalizationRule

	// SubresourceGVKMapper maps a subresource request to the GVK of the
	// subresource type for polymorphic subresources like /scale.
	SubresourceGVKMapper GroupVersionKindProvider

	// ShortCircuitMismatch allows a short-circuit declarative validation error for a field
	// to match with any handwritten validation error on its subfields.
	ShortCircuitMismatch bool
}

// declarativeRequestConfig returns the strategy's declarative request config with
// its active features merged into Options as a set (no duplicates). A feature is
// active when its gated field is already in use or its feature gate is enabled.
func declarativeRequestConfig(ctx context.Context, strategy any, obj, oldObj runtime.Object) (DeclarativeRequestConfig, error) {
	var config DeclarativeRequestConfig
	if cfg, ok := strategy.(DeclarativeStrategyConfigurer); ok {
		config = cfg.DeclarativeRequestConfig(ctx, obj, oldObj)
	}
	detector, ok := strategy.(InUseFeatureDetector)
	if !ok {
		return config, nil
	}
	inUse, notInUse, err := detector.InUseFeaturesForObject(ctx, obj, oldObj)
	if err != nil {
		return config, err
	}
	options := sets.New(config.Options...).Insert(inUse...)
	for _, gate := range notInUse {
		if utilfeature.DefaultFeatureGate.Enabled(featuregate.Feature(gate)) {
			options.Insert(gate)
		}
	}
	config.Options = sets.List(options)
	return config, nil
}
