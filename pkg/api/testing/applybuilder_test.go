/*
Copyright 2017 The Kubernetes Authors.

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

package testing

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	fuzz "github.com/google/gofuzz"
	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/typebuilders"
	v1mf "k8s.io/client-go/typebuilders/core/v1"

	"k8s.io/kubernetes/pkg/api/legacyscheme"
	api "k8s.io/kubernetes/pkg/apis/core"
)

func doRoundTripBuilders(t *testing.T, internalVersion schema.GroupVersion, externalVersion schema.GroupVersion, kind string, builder runtime.ApplyConfiguration) {
	// We do fuzzing on the internal version of the object, and only then
	// convert to the external version. This is because custom fuzzing
	// function are only supported for internal objects.
	internalObj, err := legacyscheme.Scheme.New(internalVersion.WithKind(kind))
	if err != nil {
		t.Fatalf("Couldn't create internal object %v: %v", kind, err)
	}
	seed := rand.Int63()
	fuzzer.FuzzerFor(FuzzerFuncs, rand.NewSource(seed), legacyscheme.Codecs).
		// We are explicitly overwriting custom fuzzing functions, to ensure
		// that InitContainers and their statuses are not generated. This is
		// because in this test we are simply doing json operations, in which
		// those disappear.
		Funcs(
			func(s *api.PodSpec, c fuzz.Continue) {
				c.FuzzNoCustom(s)
				s.InitContainers = nil
			},
			func(s *api.PodStatus, c fuzz.Continue) {
				c.FuzzNoCustom(s)
				s.InitContainerStatuses = nil
			},
		).Fuzz(internalObj)

	item, err := legacyscheme.Scheme.New(externalVersion.WithKind(kind))
	if err != nil {
		t.Fatalf("Couldn't create external object %v: %v", kind, err)
	}
	if err := legacyscheme.Scheme.Convert(internalObj, item, nil); err != nil {
		t.Fatalf("Conversion for %v failed: %v", kind, err)
	}
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(item)
	if err != nil {
		t.Errorf("ToUnstructured failed: %v", err)
		return
	}
	err = builder.FromUnstructured(u)
	if err != nil {
		t.Errorf("ApplyBuilder.FromUnstructured failed: %v", err)
		return
	}
	rtObj := reflect.New(reflect.TypeOf(item).Elem()).Interface().(runtime.Object)
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(builder.ToUnstructured().(map[string]interface{}), rtObj)
	if err != nil {
		t.Errorf("FromUnstructured failed: %v", err)
		return
	}
	if !apiequality.Semantic.DeepEqual(item, rtObj) {
		t.Errorf("Object changed, diff: %v", cmp.Diff(item, rtObj))
	}
}

// TestRoundTripBuilders converts a each known object type to to the apply builder for that object
// type, then converts it back to the object type and verifies it is unchanged.
func TestRoundTripBuilders(t *testing.T) {
	for gvk := range legacyscheme.Scheme.AllKnownTypes() {
		if nonRoundTrippableTypes.Has(gvk.Kind) {
			continue
		}
		if gvk.Version == runtime.APIVersionInternal {
			continue
		}
		builder := typebuilders.ForKind(gvk)
		if builder == nil {
			t.Logf("Skipping: %s", gvk)
			continue // TODO: how do we know the right ones were skipped?
		}

		t.Run(gvk.String(), func(t *testing.T) {
			for i := 0; i < 50; i++ {
				doRoundTripBuilders(t, schema.GroupVersion{Group: gvk.Group, Version: runtime.APIVersionInternal}, gvk.GroupVersion(), gvk.Kind, builder)
				if t.Failed() {
					break
				}
			}
		})
	}
}

func BenchmarkBuildersFromUnstructured(b *testing.B) {
	items := benchmarkItems(b)
	convertor := runtime.DefaultUnstructuredConverter
	unstr := make([]map[string]interface{}, len(items))
	for i := range items {
		item, err := convertor.ToUnstructured(&items[i])
		if err != nil || item == nil {
			b.Fatalf("unexpected error: %v", err)
		}
		unstr = append(unstr, item)
	}
	size := len(items)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		builder := v1mf.Pod()
		if err := builder.FromUnstructured(unstr[i%size]); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
	b.StopTimer()
}

func BenchmarkBuildersToUnstructured(b *testing.B) {
	items := benchmarkItems(b)
	convertor := runtime.DefaultUnstructuredConverter
	builders := make([]*v1mf.PodApplyConfiguration, len(items))
	for i := range items {
		item, err := convertor.ToUnstructured(&items[i])
		if err != nil || item == nil {
			b.Fatalf("unexpected error: %v", err)
		}
		builder := v1mf.Pod()
		if err := builder.FromUnstructured(item); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		builders[i] = builder
	}
	b.ResetTimer()
	size := len(items)
	for i := 0; i < b.N; i++ {
		builder := builders[i%size]
		builder.ToUnstructured()
	}
	b.StopTimer()
}
