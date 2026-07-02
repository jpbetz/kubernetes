/*
Copyright 2026 The Kubernetes Authors.

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

package fieldmanager_test

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/managedfields/managedfieldstest"
)

// exemplarPod builds a Pod with the "Exemplar Pod Shape" from
// kubernetes/kubernetes#138415: 10 annotations, 10 labels, 2 init + 2 regular
// containers, each container with 10 env vars and 10 volumeMounts, plus 10 pod
// volumes. The point is structural complexity, not byte size: this many fields
// produces a large managed-fields tree (which roughly doubles the object) that
// is re-encoded on every write.
func exemplarPod() map[string]interface{} {
	annotations := map[string]interface{}{}
	labels := map[string]interface{}{}
	for i := 0; i < 10; i++ {
		annotations[fmt.Sprintf("example.com/annotation-%d", i)] = "value"
		labels[fmt.Sprintf("label-%d", i)] = "value"
	}
	volumes := make([]interface{}, 0, 10)
	mounts := make([]interface{}, 0, 10)
	for i := 0; i < 10; i++ {
		volumes = append(volumes, map[string]interface{}{
			"name":     fmt.Sprintf("vol-%d", i),
			"emptyDir": map[string]interface{}{},
		})
		mounts = append(mounts, map[string]interface{}{
			"name":      fmt.Sprintf("vol-%d", i),
			"mountPath": fmt.Sprintf("/mnt/vol-%d", i),
		})
	}
	container := func(name string) map[string]interface{} {
		env := make([]interface{}, 0, 10)
		for i := 0; i < 10; i++ {
			env = append(env, map[string]interface{}{
				"name":  fmt.Sprintf("ENV_%d", i),
				"value": "v",
			})
		}
		return map[string]interface{}{
			"name":         name,
			"image":        "registry.example.com/app:v1",
			"env":          env,
			"volumeMounts": mounts,
		}
	}
	return map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]interface{}{
			"name":        "exemplar",
			"namespace":   "default",
			"labels":      labels,
			"annotations": annotations,
		},
		"spec": map[string]interface{}{
			"initContainers": []interface{}{container("init-0"), container("init-1")},
			"containers":     []interface{}{container("main-0"), container("main-1")},
			"volumes":        volumes,
		},
	}
}

// BenchmarkApplyExemplarPod measures a single server-side apply on an exemplar
// pod that is jointly owned by a growing number of field managers. Every write
// re-encodes each manager's FieldsV1; the "skip re-encoding unchanged managers"
// optimization reuses all but the applying manager, so its benefit grows with
// the number of co-managers (a realistic object has several: kubectl, kubelet,
// controllers, operators).
func BenchmarkApplyExemplarPod(b *testing.B) {
	for _, managers := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("managers=%d", managers), func(b *testing.B) {
			f := managedfieldstest.NewTestFieldManager(fakeTypeConverter, schema.FromAPIVersionAndKind("v1", "Pod"))
			apply := &unstructured.Unstructured{Object: exemplarPod()}
			// Establish `managers` co-owners of the full spec. Identical values
			// mean shared ownership with no conflict.
			for i := 0; i < managers; i++ {
				if err := f.Apply(apply, fmt.Sprintf("applier-%d", i), false); err != nil {
					b.Fatalf("setup apply %d: %v", i, err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				// Idempotent re-apply by one manager: re-encodes every manager's
				// FieldsV1, reusing the (managers-1) unchanged ones.
				if err := f.Apply(apply, "applier-0", false); err != nil {
					b.Fatalf("apply: %v", err)
				}
			}
		})
	}
}
