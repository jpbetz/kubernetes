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

	"k8s.io/apimachinery/pkg/api/operation"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

// DeclarativeDropFieldsStrategy defines how a strategy may opt-in to declarative field dropping.
type DeclarativeDropFieldsStrategy interface {
	// DropFieldsDeclaratively clears +k8s:FeatureGate(drop:true)=<feature-name> tagged fields
	// of obj unless feature is active for obj.
	DropFieldsDeclaratively(ctx context.Context, obj, oldObj runtime.Object, opType operation.Type, config DeclarativeRequestConfig)
}

// DeclarativeFieldDropper is an implementation of DeclarativeDropFieldsStrategy that
// provides a convenient way for a strategy to opt-in to declarative field dropping.
//
// For example:
//
//		type podStrategy struct {
//		  rest.DeclarativeFieldDropper
//		  names.NameGenerator
//		}
//	    var Strategy = podStrategy{rest.DeclarativeFieldDropper{Scheme: legacyscheme.Scheme}, names.SimpleNameGenerator}
//
// Once a strategy opts-in this way, any generated declarative field dropping code is run automatically.
type DeclarativeFieldDropper struct {
	*runtime.Scheme
}

// InUseFeaturesForObject returns obj's feature gates partitioned into inUse (a gated
// field is set in oldObj, so the gate is kept for ratcheting) and notInUse (the rest).
// oldObj is nil on create.
func (d DeclarativeFieldDropper) InUseFeaturesForObject(ctx context.Context, obj, oldObj runtime.Object) (inUse, notInUse []string, err error) {
	if d.Scheme == nil {
		return nil, nil, nil
	}
	gv, _, err := requestInfo(ctx, nil)
	if err != nil {
		return nil, nil, nil
	}
	gvks, _, err := d.Scheme.ObjectKinds(obj)
	if err != nil || len(gvks) == 0 {
		return nil, nil, nil
	}
	gvk := gv.WithKind(gvks[0].Kind)
	if !d.Scheme.HasFeatureGateInfo(gvk) {
		return nil, nil, nil
	}
	var versionedOld runtime.Object
	if oldObj != nil {
		versionedOld, err = d.Scheme.ConvertToVersion(oldObj, gv)
		if err != nil {
			return nil, nil, fmt.Errorf("converting old object to %s for feature-gate detection: %w", gv, err)
		}
	}
	inUse, notInUse = d.Scheme.FeatureGatesInUse(gvk, versionedOld)
	return inUse, notInUse, nil
}

func (d DeclarativeFieldDropper) DropFieldsDeclaratively(ctx context.Context, obj, _ runtime.Object, opType operation.Type, config DeclarativeRequestConfig) {
	if d.Scheme == nil {
		return
	}
	logger := klog.FromContext(ctx)
	gv, subresources, err := requestInfo(ctx, config.SubresourceGVKMapper)
	if err != nil {
		logger.Error(err, "skipping declarative field dropping: cannot determine request version")
		return
	}
	// Skip the conversion round-trip when nothing is registered for this version.
	if gvks, _, err := d.Scheme.ObjectKinds(obj); err != nil || len(gvks) == 0 || !d.Scheme.HasFeatureGateInfo(gv.WithKind(gvks[0].Kind)) {
		return
	}
	versionedObj, err := d.Scheme.ConvertToVersion(obj, gv)
	if err != nil {
		logger.Error(err, "skipping declarative field dropping: convert to versioned failed")
		return
	}
	op := operation.Operation{Type: opType, Request: operation.Request{Subresources: subresources}, Options: config.Options}
	if !d.Scheme.DropFields(op, versionedObj) {
		return
	}
	if err := d.Scheme.Convert(versionedObj, obj, nil); err != nil {
		logger.Error(err, "declarative field dropping: convert back to internal failed")
	}
}
