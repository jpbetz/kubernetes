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

package v1

import (
	"fmt"
	"runtime"
	"testing"
	"unsafe"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/cache"
)

// makeObjectMeta creates a realistic ObjectMeta with labels, annotations,
// ownerReferences, and managedFields.
func makeObjectMeta(i int) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:            fmt.Sprintf("replicaset-%d", i),
		Namespace:       "default",
		UID:             types.UID(fmt.Sprintf("uid-%d", i)),
		ResourceVersion: fmt.Sprintf("%d", 1000+i),
		Generation:      int64(i),
		Labels: map[string]string{
			"app":        fmt.Sprintf("myapp-%d", i%100),
			"version":    "v1",
			"team":       "platform",
			"managed-by": "deployment-controller",
		},
		Annotations: map[string]string{
			"deployment.kubernetes.io/revision":              fmt.Sprintf("%d", i%10),
			"deployment.kubernetes.io/desired-replicas":      "3",
			"deployment.kubernetes.io/max-replicas":          "4",
			"kubectl.kubernetes.io/last-applied-configuration": fmt.Sprintf(`{"apiVersion":"apps/v1","kind":"ReplicaSet","metadata":{"name":"replicaset-%d","namespace":"default"}}`, i),
		},
		OwnerReferences: []metav1.OwnerReference{
			{
				APIVersion:         "apps/v1",
				Kind:               "Deployment",
				Name:               fmt.Sprintf("deployment-%d", i%100),
				UID:                types.UID(fmt.Sprintf("deploy-uid-%d", i%100)),
				Controller:         boolPtr(true),
				BlockOwnerDeletion: boolPtr(true),
			},
		},
		ManagedFields: []metav1.ManagedFieldsEntry{
			{
				Manager:   "kube-controller-manager",
				Operation: metav1.ManagedFieldsOperationUpdate,
				FieldsV1:  &metav1.FieldsV1{Raw: []byte(`{"f:metadata":{"f:ownerReferences":{}},"f:spec":{"f:replicas":{},"f:selector":{},"f:template":{"f:metadata":{"f:labels":{}},"f:spec":{"f:containers":{}}}}}`)},
			},
			{
				Manager:   "kube-controller-manager",
				Operation: metav1.ManagedFieldsOperationUpdate,
				FieldsV1:  &metav1.FieldsV1{Raw: []byte(`{"f:status":{"f:availableReplicas":{},"f:fullyLabeledReplicas":{},"f:observedGeneration":{},"f:readyReplicas":{},"f:replicas":{}}}`)},
			},
		},
	}
}

// makeSelector creates a realistic label selector.
func makeSelector(i int) *metav1.LabelSelector {
	return &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app":     fmt.Sprintf("myapp-%d", i%100),
			"version": "v1",
			"pod-template-hash": fmt.Sprintf("hash-%d", i),
		},
	}
}

// makeFullReplicaSet creates a realistic full ReplicaSet with PodTemplateSpec and Status.
func makeFullReplicaSet(i int) *appsv1.ReplicaSet {
	replicas := int32(3)
	return &appsv1.ReplicaSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "ReplicaSet",
		},
		ObjectMeta: makeObjectMeta(i),
		Spec: appsv1.ReplicaSetSpec{
			Replicas: &replicas,
			Selector: makeSelector(i),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":               fmt.Sprintf("myapp-%d", i%100),
						"version":           "v1",
						"pod-template-hash": fmt.Sprintf("hash-%d", i),
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "default",
					RestartPolicy:      corev1.RestartPolicyAlways,
					Containers: []corev1.Container{
						{
							Name:    "main",
							Image:   fmt.Sprintf("registry.example.com/myapp-%d:v1.2.3", i%100),
							Command: []string{"/bin/main", "--config=/etc/config/app.yaml"},
							Env: []corev1.EnvVar{
								{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
								{Name: "POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}}},
								{Name: "DB_HOST", Value: "postgres.default.svc.cluster.local"},
								{Name: "DB_PORT", Value: "5432"},
								{Name: "LOG_LEVEL", Value: "info"},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "config", MountPath: "/etc/config", ReadOnly: true},
								{Name: "data", MountPath: "/var/data"},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intOrStringFromInt(8080)},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       10,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intOrStringFromInt(8080)},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
							},
						},
						{
							Name:  "sidecar",
							Image: "registry.example.com/sidecar:v0.5.0",
							Env: []corev1.EnvVar{
								{Name: "PROXY_PORT", Value: "9090"},
								{Name: "METRICS_ENABLED", Value: "true"},
								{Name: "LOG_FORMAT", Value: "json"},
								{Name: "UPSTREAM_HOST", Value: "localhost"},
								{Name: "UPSTREAM_PORT", Value: "8080"},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "config", MountPath: "/etc/sidecar", ReadOnly: true},
								{Name: "tmp", MountPath: "/tmp"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("config-%d", i%100)},
								},
							},
						},
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "tmp",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
		Status: appsv1.ReplicaSetStatus{
			Replicas:             3,
			FullyLabeledReplicas: 3,
			ReadyReplicas:        3,
			AvailableReplicas:    3,
			ObservedGeneration:   int64(i),
			Conditions: []appsv1.ReplicaSetCondition{
				{
					Type:    appsv1.ReplicaSetReplicaFailure,
					Status:  corev1.ConditionFalse,
					Reason:  "MinimumReplicasAvailable",
					Message: "ReplicaSet has minimum availability",
				},
			},
		},
	}
}

// makeViewReplicaSet creates a view ReplicaSet with the same ObjectMeta as the full one.
func makeViewReplicaSet(i int) *ReplicaSet {
	return &ReplicaSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "ReplicaSet",
		},
		ObjectMeta: makeObjectMeta(i),
		Spec: ReplicaSetSpec{
			Selector: makeSelector(i),
		},
	}
}

func intOrStringFromInt(val int) intstr.IntOrString {
	return intstr.FromInt32(int32(val))
}

func boolPtr(b bool) *bool { return &b }

// BenchmarkReplicaSetCacheMemory measures heap memory for cache.Store holding
// full vs. view ReplicaSet types at various scales.
func BenchmarkReplicaSetCacheMemory(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("Full/%d", count), func(b *testing.B) {
			for range b.N {
				store := cache.NewStore(cache.MetaNamespaceKeyFunc)
				forceGC()
				var before runtime.MemStats
				runtime.ReadMemStats(&before)

				for i := range count {
					if err := store.Add(makeFullReplicaSet(i)); err != nil {
						b.Fatal(err)
					}
				}

				forceGC()
				var after runtime.MemStats
				runtime.ReadMemStats(&after)

				heapDelta := after.HeapAlloc - before.HeapAlloc
				b.ReportMetric(float64(heapDelta), "heap-bytes")
				b.ReportMetric(float64(heapDelta)/float64(count), "bytes/object")
				// Prevent store from being GC'd before measurement
				runtime.KeepAlive(store)
			}
		})

		b.Run(fmt.Sprintf("View/%d", count), func(b *testing.B) {
			for range b.N {
				store := cache.NewStore(cache.MetaNamespaceKeyFunc)
				forceGC()
				var before runtime.MemStats
				runtime.ReadMemStats(&before)

				for i := range count {
					if err := store.Add(makeViewReplicaSet(i)); err != nil {
						b.Fatal(err)
					}
				}

				forceGC()
				var after runtime.MemStats
				runtime.ReadMemStats(&after)

				heapDelta := after.HeapAlloc - before.HeapAlloc
				b.ReportMetric(float64(heapDelta), "heap-bytes")
				b.ReportMetric(float64(heapDelta)/float64(count), "bytes/object")
				runtime.KeepAlive(store)
			}
		})
	}
}

// BenchmarkReplicaSetAllocs measures per-operation allocations for inserting
// objects into a cache.Store.
func BenchmarkReplicaSetAllocs(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprintf("Full/%d", count), func(b *testing.B) {
			objects := make([]*appsv1.ReplicaSet, count)
			for i := range count {
				objects[i] = makeFullReplicaSet(i)
			}
			b.ResetTimer()
			b.ReportAllocs()
			for range b.N {
				store := cache.NewStore(cache.MetaNamespaceKeyFunc)
				for _, obj := range objects {
					if err := store.Add(obj); err != nil {
						b.Fatal(err)
					}
				}
			}
		})

		b.Run(fmt.Sprintf("View/%d", count), func(b *testing.B) {
			objects := make([]*ReplicaSet, count)
			for i := range count {
				objects[i] = makeViewReplicaSet(i)
			}
			b.ResetTimer()
			b.ReportAllocs()
			for range b.N {
				store := cache.NewStore(cache.MetaNamespaceKeyFunc)
				for _, obj := range objects {
					if err := store.Add(obj); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

// BenchmarkReplicaSetObjectSize reports the shallow struct sizes for full vs. view types.
func BenchmarkReplicaSetObjectSize(b *testing.B) {
	full := makeFullReplicaSet(0)
	view := makeViewReplicaSet(0)

	fullSize := unsafe.Sizeof(*full)
	viewSize := unsafe.Sizeof(*view)

	b.Run("Full", func(b *testing.B) {
		b.ReportMetric(float64(fullSize), "struct-bytes")
		for range b.N {
			_ = makeFullReplicaSet(0)
		}
	})

	b.Run("View", func(b *testing.B) {
		b.ReportMetric(float64(viewSize), "struct-bytes")
		for range b.N {
			_ = makeViewReplicaSet(0)
		}
	})
}

func forceGC() {
	runtime.GC()
	runtime.GC()
}
