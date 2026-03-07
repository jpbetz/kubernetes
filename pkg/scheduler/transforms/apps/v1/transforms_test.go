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
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// makeRealisticReplicaSet creates a ReplicaSet that approximates what the
// scheduler sees in a real cluster, including ManagedFields, a full
// PodTemplateSpec, and Status.
func makeRealisticReplicaSet(i int) *appsv1.ReplicaSet {
	replicas := int32(3)
	t := metav1.Now()
	isController := true
	return &appsv1.ReplicaSet{
		TypeMeta: metav1.TypeMeta{Kind: "ReplicaSet", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("nginx-%d-abcdef", i),
			Namespace:       "default",
			UID:             "uid-1234",
			ResourceVersion: "12345",
			Generation:      2,
			Labels: map[string]string{
				"app":                "nginx",
				"pod-template-hash":  "abcdef",
				"tier":               "frontend",
				"environment":        "production",
			},
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision":               "3",
				"deployment.kubernetes.io/desired-replicas":        "3",
				"deployment.kubernetes.io/max-replicas":            "4",
				"kubectl.kubernetes.io/last-applied-configuration": strings.Repeat("x", 2048),
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       fmt.Sprintf("nginx-%d", i),
					UID:        "owner-uid",
					Controller: &isController,
				},
			},
			ManagedFields: makeManagedFields(),
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":               "nginx",
					"pod-template-hash": "abcdef",
				},
			},
			Template: makePodTemplateSpec(),
		},
		Status: appsv1.ReplicaSetStatus{
			Replicas:             3,
			FullyLabeledReplicas: 3,
			ReadyReplicas:        3,
			AvailableReplicas:    3,
			ObservedGeneration:   2,
			Conditions: []appsv1.ReplicaSetCondition{
				{
					Type:               appsv1.ReplicaSetReplicaFailure,
					Status:             corev1.ConditionFalse,
					LastTransitionTime: t,
					Reason:             "MinimumReplicasAvailable",
					Message:            "ReplicaSet has minimum availability",
				},
			},
		},
	}
}

// makeRealisticStatefulSet creates a StatefulSet that approximates what the
// scheduler sees in a real cluster.
func makeRealisticStatefulSet(i int) *appsv1.StatefulSet {
	replicas := int32(3)
	revHistoryLimit := int32(10)
	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{Kind: "StatefulSet", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("mysql-%d", i),
			Namespace:       "default",
			UID:             "uid-5678",
			ResourceVersion: "67890",
			Generation:      3,
			Labels: map[string]string{
				"app":         "mysql",
				"tier":        "database",
				"environment": "production",
			},
			Annotations: map[string]string{
				"kubectl.kubernetes.io/last-applied-configuration": strings.Repeat("y", 3072),
			},
			ManagedFields: makeManagedFields(),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "mysql"},
			},
			Template:            makePodTemplateSpec(),
			ServiceName:         "mysql",
			PodManagementPolicy: appsv1.OrderedReadyPodManagement,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
			},
			RevisionHistoryLimit: &revHistoryLimit,
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:        3,
			ReadyReplicas:   3,
			CurrentReplicas: 3,
			UpdatedReplicas: 3,
			CurrentRevision: "mysql-abc",
			UpdateRevision:  "mysql-abc",
		},
	}
}

func makePodTemplateSpec() corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app":               "nginx",
				"pod-template-hash": "abcdef",
				"tier":              "frontend",
			},
			Annotations: map[string]string{
				"prometheus.io/scrape": "true",
				"prometheus.io/port":   "9090",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "nginx",
					Image: "nginx:1.25.3",
					Ports: []corev1.ContainerPort{
						{Name: "http", ContainerPort: 80, Protocol: corev1.ProtocolTCP},
						{Name: "metrics", ContainerPort: 9090, Protocol: corev1.ProtocolTCP},
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
					Env: []corev1.EnvVar{
						{Name: "ENV", Value: "production"},
						{Name: "LOG_LEVEL", Value: "info"},
						{Name: "CONFIG_PATH", Value: "/etc/config"},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "config", MountPath: "/etc/config", ReadOnly: true},
						{Name: "secrets", MountPath: "/etc/secrets", ReadOnly: true},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path:   "/healthz",
								Port:   intstr.FromInt32(80),
								Scheme: corev1.URISchemeHTTP,
							},
						},
						InitialDelaySeconds: 10,
						PeriodSeconds:       30,
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path:   "/ready",
								Port:   intstr.FromInt32(80),
								Scheme: corev1.URISchemeHTTP,
							},
						},
						PeriodSeconds: 10,
					},
				},
				{
					Name:  "sidecar",
					Image: "envoyproxy/envoy:v1.28",
					Ports: []corev1.ContainerPort{
						{Name: "proxy", ContainerPort: 8080, Protocol: corev1.ProtocolTCP},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
					},
				},
			},
			InitContainers: []corev1.Container{
				{
					Name:    "init-config",
					Image:   "busybox:1.36",
					Command: []string{"sh", "-c", "cp /config/* /shared/"},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"},
						},
					},
				},
				{
					Name: "secrets",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{SecretName: "app-secrets"},
					},
				},
			},
			ServiceAccountName:           "default",
			TerminationGracePeriodSeconds: int64Ptr(30),
			DNSPolicy:                    corev1.DNSClusterFirst,
			RestartPolicy:                corev1.RestartPolicyAlways,
		},
	}
}

// makeManagedFields creates a realistic set of managed fields entries.
// In production clusters, ManagedFields typically has 2-5 entries and the
// FieldsV1 raw data can be several KB each.
func makeManagedFields() []metav1.ManagedFieldsEntry {
	t := metav1.Now()
	// Simulate realistic FieldsV1 JSON data sizes.
	smallFields := []byte(fmt.Sprintf(`{"f:metadata":{"f:labels":{%s}}}`, strings.Repeat(`"f:key":"val",`, 20)))
	largeFields := []byte(fmt.Sprintf(`{"f:spec":{"f:template":{"f:spec":{"f:containers":{%s}}}}}`, strings.Repeat(`"f:name":"x","f:image":"y","f:ports":{},"f:resources":{},`, 30)))

	return []metav1.ManagedFieldsEntry{
		{
			Manager:    "kubectl-client-side-apply",
			Operation:  metav1.ManagedFieldsOperationApply,
			APIVersion: "apps/v1",
			Time:       &t,
			FieldsType: "FieldsV1",
			FieldsV1:   &metav1.FieldsV1{Raw: largeFields},
		},
		{
			Manager:    "kube-controller-manager",
			Operation:  metav1.ManagedFieldsOperationUpdate,
			APIVersion: "apps/v1",
			Time:       &t,
			FieldsType: "FieldsV1",
			FieldsV1:   &metav1.FieldsV1{Raw: smallFields},
		},
		{
			Manager:    "custom-operator",
			Operation:  metav1.ManagedFieldsOperationUpdate,
			APIVersion: "apps/v1",
			Time:       &t,
			FieldsType: "FieldsV1",
			FieldsV1:   &metav1.FieldsV1{Raw: largeFields},
		},
	}
}

func int64Ptr(v int64) *int64 { return &v }

// heapAlloc returns the current HeapAlloc after forcing garbage collection.
func heapAlloc() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func BenchmarkTransformMemorySavings(b *testing.B) {
	const numObjects = 1000

	b.Run("ReplicaSet", func(b *testing.B) {
		benchmarkTransform(b, numObjects, func(i int) interface{} {
			return makeRealisticReplicaSet(i)
		}, TransformReplicaSet)
	})

	b.Run("StatefulSet", func(b *testing.B) {
		benchmarkTransform(b, numObjects, func(i int) interface{} {
			return makeRealisticStatefulSet(i)
		}, TransformStatefulSet)
	})
}

// benchmarkTransform measures memory retained by N objects before and after
// applying a transform. It allocates all objects, measures heap, applies
// the transform (which zeros fields in-place, making sub-objects eligible
// for GC), then measures heap again after GC.
func benchmarkTransform(b *testing.B, n int, makeObj func(int) interface{}, transform func(interface{}) (interface{}, error)) {
	b.Helper()

	for range b.N {
		// Allocate objects.
		objects := make([]interface{}, n)
		for i := range objects {
			objects[i] = makeObj(i)
		}

		// Measure heap with all fields populated.
		before := heapAlloc()

		// Apply transforms — zeros fields in-place, releasing references
		// to sub-objects (slices, maps, strings, nested structs).
		for i, obj := range objects {
			transformed, err := transform(obj)
			if err != nil {
				b.Fatal(err)
			}
			objects[i] = transformed
		}

		// Measure heap after GC collects the freed sub-objects.
		after := heapAlloc()

		savedBytes := int64(before) - int64(after)
		perObjectSaved := float64(savedBytes) / float64(n)
		pctSaved := float64(savedBytes) / float64(before) * 100

		b.ReportMetric(float64(before)/float64(n), "baseline-bytes/obj")
		b.ReportMetric(float64(after)/float64(n), "transformed-bytes/obj")
		b.ReportMetric(perObjectSaved, "saved-bytes/obj")
		b.ReportMetric(pctSaved, "%saved")

		runtime.KeepAlive(objects)
	}
}
