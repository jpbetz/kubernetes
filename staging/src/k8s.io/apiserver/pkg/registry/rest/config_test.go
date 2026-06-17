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
	"errors"
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
)

type configurerStub struct{ options []string }

func (c configurerStub) DeclarativeRequestConfig(context.Context, runtime.Object, runtime.Object) DeclarativeRequestConfig {
	return DeclarativeRequestConfig{Options: c.options}
}

type detectorStub struct {
	inUse    []string
	notInUse []string
	err      error
}

func (d detectorStub) InUseFeaturesForObject(context.Context, runtime.Object, runtime.Object) (inUse, notInUse []string, err error) {
	return d.inUse, d.notInUse, d.err
}

type configurerDetectorStub struct {
	configurerStub
	detectorStub
}

func TestDeclarativeRequestConfig(t *testing.T) {
	gate := features.ManifestBasedAdmissionControlConfig
	cases := []struct {
		name     string
		strategy any
		gateSet  bool
		gateOn   bool
		want     []string
		wantErr  bool
	}{{
		name:     "configurer options and in-use features merged as a sorted set",
		strategy: configurerDetectorStub{configurerStub{[]string{"B", "A"}}, detectorStub{inUse: []string{"A", "C"}}},
		want:     []string{"A", "B", "C"},
	}, {
		name:     "not-in-use feature included when its gate is enabled",
		strategy: detectorStub{notInUse: []string{string(gate)}},
		gateSet:  true,
		gateOn:   true,
		want:     []string{string(gate)},
	}, {
		name:     "not-in-use feature excluded when its gate is disabled",
		strategy: detectorStub{notInUse: []string{string(gate)}},
		gateSet:  true,
		gateOn:   false,
		want:     nil,
	}, {
		name:     "configurer only, options left as-is",
		strategy: configurerStub{[]string{"B", "A"}},
		want:     []string{"B", "A"},
	}, {
		name:     "neither interface, no options",
		strategy: struct{}{},
		want:     nil,
	}, {
		name:     "detector error is returned",
		strategy: detectorStub{err: errors.New("boom")},
		wantErr:  true,
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.gateSet {
				featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, gate, tc.gateOn)
			}
			config, err := declarativeRequestConfig(context.Background(), tc.strategy, nil, nil)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(config.Options, tc.want) {
				t.Errorf("Options = %v, want %v", config.Options, tc.want)
			}
		})
	}
}
