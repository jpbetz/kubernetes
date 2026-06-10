/*
Copyright The Kubernetes Authors.

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

package v1_test

import (
	"encoding/json"
	"fmt"
	"runtime"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var benchmarkPayloads = []string{
	// Small managed fields payload
	`{"f:metadata":{"f:labels":{"f:app":{}},"f:annotations":{"f:revision":{}}},"f:spec":{"f:replicas":{},"f:template":{"f:metadata":{"f:labels":{"f:app":{}}},"f:spec":{"f:containers":{"k:{\"name\":\"nginx\"}":{".":{},"f:image":{},"f:name":{}}}}}}}`,
	// Larger, deeply nested valid JSON payload (representing complex managedFields)
	`{"f:metadata":{"f:annotations":{".":{},"f:kubectl.kubernetes.io/last-applied-configuration":{}},"f:labels":{".":{},"f:app.kubernetes.io/name":{}}},"f:spec":{"f:replicas":{},"f:selector":{},"f:template":{"f:metadata":{"f:labels":{".":{},"f:app.kubernetes.io/name":{}}},"f:spec":{"f:containers":{"k:{\"name\":\"app\"}":{".":{},"f:image":{},"f:name":{},"f:ports":{".":{},"k:{\"containerPort\":8080,\"protocol\":\"TCP\"}":{".":{},"f:containerPort":{},"f:protocol":{}}},"f:resources":{".":{},"f:limits":{".":{},"f:cpu":{},"f:memory":{}},"f:requests":{".":{},"f:cpu":{},"f:memory":{}}}}}}}}}`,
}

// BenchmarkDecodeDuplicate measures the allocation and speed of unmarshaling
// exactly identical payloads repeatedly, which is the most common case for
// heavily replicated resources (DaemonSets, ReplicaSets) in the API server.
func BenchmarkDecodeDuplicate(b *testing.B) {
	for _, payload := range benchmarkPayloads {
		b.Run(fmt.Sprintf("Size%d", len(payload)), func(b *testing.B) {
			rawJSON := []byte(payload)
			b.ResetTimer()
			b.ReportAllocs()
			var retained []metav1.FieldsV1
			for j := 0; j < b.N; j++ {
				var f metav1.FieldsV1
				if err := json.Unmarshal(rawJSON, &f); err != nil {
					b.Fatal(err)
				}
				retained = append(retained, f)
			}
			_ = retained
		})
	}
}

// BenchmarkDecodeUnique measures the allocation and speed of unmarshaling
// completely unique payloads to measure the worst-case interning overhead.
func BenchmarkDecodeUnique(b *testing.B) {
	for _, payload := range benchmarkPayloads {
		b.Run(fmt.Sprintf("Size%d", len(payload)), func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			var retained []metav1.FieldsV1
			for j := 0; j < b.N; j++ {
				// Inject the iteration counter to guarantee the string is unique
				novelPayload := fmt.Appendf(nil, `{"f:iter":%d,%s`, j, payload[1:])
				var f metav1.FieldsV1
				if err := json.Unmarshal(novelPayload, &f); err != nil {
					b.Fatal(err)
				}
				retained = append(retained, f)
			}
			_ = retained
		})
	}
}

// BenchmarkParallelDecode tests parallel deserialization of duplicate strings
// to verify lock contention behavior in highly concurrent environments.
func BenchmarkParallelDecode(b *testing.B) {
	for _, payload := range benchmarkPayloads {
		b.Run(fmt.Sprintf("Size%d", len(payload)), func(b *testing.B) {
			rawJSON := []byte(payload)
			b.ResetTimer()
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					var f metav1.FieldsV1
					if err := json.Unmarshal(rawJSON, &f); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

// BenchmarkSiblingPodsResidentBytes measures the live-heap cost of retaining
// managed fields for many sibling pods (e.g. all pods of one ReplicaSet): the
// spec entry payload is identical across pods while the kubelet status entry
// payload is unique per pod. The retained-B/pod metric is the GC-surviving
// heap delta per pod; run with and without the fieldsv1string build tag to
// compare interned vs per-object storage.
func BenchmarkSiblingPodsResidentBytes(b *testing.B) {
	const numPods = 1000
	specPayload := []byte(benchmarkPayloads[1])
	statusPayloads := make([][]byte, numPods)
	for i := range statusPayloads {
		statusPayloads[i] = fmt.Appendf(nil,
			`{"f:status":{"f:conditions":{"k:{\"type\":\"Ready\"}":{".":{},"f:lastTransitionTime":{},"f:status":{},"f:type":{}}},"f:containerStatuses":{},"f:hostIP":{},"f:phase":{},"f:podIP":{},"f:podIPs":{".":{},"k:{\"ip\":\"10.%d.%d.%d\"}":{".":{},"f:ip":{}}},"f:startTime":{}}}`,
			i>>16&0xff, i>>8&0xff, i&0xff)
	}

	type podManagedFields struct {
		spec   metav1.FieldsV1
		status metav1.FieldsV1
	}
	build := func() []podManagedFields {
		pods := make([]podManagedFields, numPods)
		for i := range pods {
			if err := json.Unmarshal(specPayload, &pods[i].spec); err != nil {
				b.Fatal(err)
			}
			if err := json.Unmarshal(statusPayloads[i], &pods[i].status); err != nil {
				b.Fatal(err)
			}
		}
		return pods
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	retained := build()
	runtime.GC()
	runtime.ReadMemStats(&after)
	retainedPerPod := float64(int64(after.HeapAlloc)-int64(before.HeapAlloc)) / numPods
	runtime.KeepAlive(retained)
	retained = nil

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pods := build()
		_ = pods
	}
	b.ReportMetric(retainedPerPod, "retained-B/pod")
}

// BenchmarkEqual_Same measures the fast-path equality check for identical structs.
func BenchmarkEqual_Same(b *testing.B) {
	f1 := metav1.NewFieldsV1(benchmarkPayloads[1])
	f2 := metav1.NewFieldsV1(benchmarkPayloads[1])
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = f1.Equal(*f2)
	}
}

// BenchmarkEqual_Different measures the fallback-path equality check for differing structs.
func BenchmarkEqual_Different(b *testing.B) {
	f1 := metav1.NewFieldsV1(benchmarkPayloads[1])
	f2 := metav1.NewFieldsV1(benchmarkPayloads[0])
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = f1.Equal(*f2)
	}
}
