/*
Copyright 2019 The Kubernetes Authors.

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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"k8s.io/kube-openapi/pkg/util/proto"
	prototesting "k8s.io/kube-openapi/pkg/util/proto/testing"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
	apitesting "k8s.io/kubernetes/pkg/api/testing"
	"sigs.k8s.io/structured-merge-diff/v2/fieldpath"
	"sigs.k8s.io/structured-merge-diff/v2/merge"
	"sigs.k8s.io/structured-merge-diff/v2/typed"
	"sigs.k8s.io/structured-merge-diff/v2/value"
	"sigs.k8s.io/yaml"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/endpoints/handlers/fieldmanager"
	"k8s.io/apiserver/pkg/endpoints/handlers/fieldmanager/internal"

	// Initialize install packages
	_ "k8s.io/kubernetes/pkg/apis/admission/install"
	_ "k8s.io/kubernetes/pkg/apis/admissionregistration/install"
	_ "k8s.io/kubernetes/pkg/apis/apps/install"
	_ "k8s.io/kubernetes/pkg/apis/auditregistration/install"
	_ "k8s.io/kubernetes/pkg/apis/authentication/install"
	_ "k8s.io/kubernetes/pkg/apis/authorization/install"
	_ "k8s.io/kubernetes/pkg/apis/autoscaling/install"
	_ "k8s.io/kubernetes/pkg/apis/batch/install"
	_ "k8s.io/kubernetes/pkg/apis/certificates/install"
	_ "k8s.io/kubernetes/pkg/apis/coordination/install"
	_ "k8s.io/kubernetes/pkg/apis/core/install"
	_ "k8s.io/kubernetes/pkg/apis/discovery/install"
	_ "k8s.io/kubernetes/pkg/apis/events/install"
	_ "k8s.io/kubernetes/pkg/apis/extensions/install"
	_ "k8s.io/kubernetes/pkg/apis/imagepolicy/install"
	_ "k8s.io/kubernetes/pkg/apis/networking/install"
	_ "k8s.io/kubernetes/pkg/apis/node/install"
	_ "k8s.io/kubernetes/pkg/apis/policy/install"
	_ "k8s.io/kubernetes/pkg/apis/rbac/install"
	_ "k8s.io/kubernetes/pkg/apis/scheduling/install"
	_ "k8s.io/kubernetes/pkg/apis/settings/install"
	_ "k8s.io/kubernetes/pkg/apis/storage/install"
)

var fakeSchema = prototesting.Fake{
	Path: filepath.Join(
		strings.Repeat(".."+string(filepath.Separator), 8),
		"api", "openapi-spec", "swagger.json"),
}

// TODO(jpbetz): Is there a pre-existing converter for this sort of thing?
type fakeObjectConvertor struct {
	converter  merge.Converter
	apiVersion fieldpath.APIVersion
}

func (c *fakeObjectConvertor) Convert(in, out, context interface{}) error {
	if typedValue, ok := in.(*typed.TypedValue); ok {
		fmt.Printf("typedValue found")
		var err error
		out, err = c.converter.Convert(typedValue, c.apiVersion)
		return err
	}
	fmt.Printf("typedValue not found")
	out = in
	return nil
}

func (c *fakeObjectConvertor) ConvertToVersion(in runtime.Object, _ runtime.GroupVersioner) (runtime.Object, error) {
	return in, nil
}

func (c *fakeObjectConvertor) ConvertFieldLabel(_ schema.GroupVersionKind, _, _ string) (string, string, error) {
	return "", "", errors.New("not implemented")
}

type fakeObjectDefaulter struct{}

func (d *fakeObjectDefaulter) Default(in runtime.Object) {}

type TestFieldManager struct {
	fieldManager fieldmanager.Manager
	emptyObj     runtime.Object
	liveObj      runtime.Object
}

func NewTestFieldManager(gvk schema.GroupVersionKind) TestFieldManager {
	m := NewFakeOpenAPIModels()
	tc := NewFakeTypeConverter(m)

	converter := internal.NewVersionConverter(tc, &fakeObjectConvertor{}, gvk.GroupVersion())
	apiVersion := fieldpath.APIVersion(gvk.GroupVersion().String())
	f, err := fieldmanager.NewStructuredMergeManager(
		m,
		&fakeObjectConvertor{converter, apiVersion},
		&fakeObjectDefaulter{},
		gvk.GroupVersion(),
		gvk.GroupVersion(),
	)
	if err != nil {
		panic(err)
	}
	live := &unstructured.Unstructured{}
	live.SetKind(gvk.Kind)
	live.SetAPIVersion(gvk.GroupVersion().String())
	f = fieldmanager.NewStripMetaManager(f)
	f = fieldmanager.NewBuildManagerInfoManager(f, gvk.GroupVersion())
	return TestFieldManager{
		fieldManager: f,
		emptyObj:     live,
		liveObj:      live.DeepCopyObject(),
	}
}

func NewFakeTypeConverter(m proto.Models) internal.TypeConverter {
	tc, err := internal.NewTypeConverter(m, false)
	if err != nil {
		panic(fmt.Sprintf("Failed to build TypeConverter: %v", err))
	}
	return tc
}

func NewFakeOpenAPIModels() proto.Models {
	d, err := fakeSchema.OpenAPISchema()
	if err != nil {
		panic(err)
	}
	m, err := proto.NewOpenAPIData(d)
	if err != nil {
		panic(err)
	}
	return m
}

func (f *TestFieldManager) Reset() {
	f.liveObj = f.emptyObj.DeepCopyObject()
}

func (f *TestFieldManager) Apply(obj []byte, manager string, force bool) error {
	out, err := fieldmanager.NewFieldManager(f.fieldManager).Apply(f.liveObj, obj, manager, force)
	if err == nil {
		f.liveObj = out
	}
	return err
}

func (f *TestFieldManager) Update(obj runtime.Object, manager string) error {
	out, err := fieldmanager.NewFieldManager(f.fieldManager).Update(f.liveObj, obj, manager)
	if err == nil {
		f.liveObj = out
	}
	return err
}

func (f *TestFieldManager) ManagedFields() []metav1.ManagedFieldsEntry {
	accessor, err := meta.Accessor(f.liveObj)
	if err != nil {
		panic(fmt.Errorf("couldn't get accessor: %v", err))
	}

	return accessor.GetManagedFields()
}

// TestUpdateApplyConflict tests that applying to an object, which
// wasn't created by apply, will give conflicts
func TestUpdateApplyConflict(t *testing.T) {
	f := NewTestFieldManager(schema.FromAPIVersionAndKind("apps/v1", "Deployment"))

	patch := []byte(`{
		"apiVersion": "apps/v1",
		"kind": "Deployment",
		"metadata": {
			"name": "deployment",
			"labels": {"app": "nginx"}
		},
		"spec": {
                        "replicas": 3,
                        "selector": {
                                "matchLabels": {
                                         "app": "nginx"
                                }
                        },
                        "template": {
                                "metadata": {
                                        "labels": {
                                                "app": "nginx"
                                        }
                                },
                                "spec": {
				        "containers": [{
					        "name":  "nginx",
					        "image": "nginx:latest"
				        }]
                                }
                        }
		}
	}`)
	newObj := &appsv1.Deployment{}
	if err := yaml.Unmarshal(patch, &newObj); err != nil {
		t.Fatalf("error decoding YAML: %v", err)
	}

	if err := f.Update(newObj, "fieldmanager_test"); err != nil {
		t.Fatalf("failed to apply object: %v", err)
	}

	err := f.Apply([]byte(`{
		"apiVersion": "apps/v1",
		"kind": "Deployment",
		"metadata": {
			"name": "deployment",
		},
		"spec": {
			"replicas": 101,
		}
	}`), "fieldmanager_conflict", false)
	if err == nil || !apierrors.IsConflict(err) {
		t.Fatalf("Expecting to get conflicts but got %v", err)
	}
}

func TestApplyStripsFields(t *testing.T) {
	f := NewTestFieldManager(schema.FromAPIVersionAndKind("apps/v1", "Deployment"))

	newObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
		},
	}

	newObj.SetName("b")
	newObj.SetNamespace("b")
	newObj.SetUID("b")
	newObj.SetClusterName("b")
	newObj.SetGeneration(0)
	newObj.SetResourceVersion("b")
	newObj.SetCreationTimestamp(metav1.NewTime(time.Now()))
	newObj.SetManagedFields([]metav1.ManagedFieldsEntry{
		{
			Manager:    "update",
			Operation:  metav1.ManagedFieldsOperationApply,
			APIVersion: "apps/v1",
		},
	})
	if err := f.Update(newObj, "fieldmanager_test"); err != nil {
		t.Fatalf("failed to apply object: %v", err)
	}

	if m := f.ManagedFields(); len(m) != 0 {
		t.Fatalf("fields did not get stripped: %v", m)
	}
}

func TestVersionCheck(t *testing.T) {
	f := NewTestFieldManager(schema.FromAPIVersionAndKind("apps/v1", "Deployment"))

	// patch has 'apiVersion: apps/v1' and live version is apps/v1 -> no errors
	err := f.Apply([]byte(`{
		"apiVersion": "apps/v1",
		"kind": "Deployment",
	}`), "fieldmanager_test", false)
	if err != nil {
		t.Fatalf("failed to apply object: %v", err)
	}

	// patch has 'apiVersion: apps/v2' but live version is apps/v1 -> error
	err = f.Apply([]byte(`{
		"apiVersion": "apps/v1beta1",
		"kind": "Deployment",
	}`), "fieldmanager_test", false)
	if err == nil {
		t.Fatalf("expected an error from mismatched patch and live versions")
	}
	switch typ := err.(type) {
	default:
		t.Fatalf("expected error to be of type %T was %T", apierrors.StatusError{}, typ)
	case apierrors.APIStatus:
		if typ.Status().Code != http.StatusBadRequest {
			t.Fatalf("expected status code to be %d but was %d",
				http.StatusBadRequest, typ.Status().Code)
		}
	}
}

func TestApplyDoesNotStripLabels(t *testing.T) {
	f := NewTestFieldManager(schema.FromAPIVersionAndKind("v1", "Pod"))

	err := f.Apply([]byte(`{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"labels": {
				"a": "b"
			},
		}
	}`), "fieldmanager_test", false)
	if err != nil {
		t.Fatalf("failed to apply object: %v", err)
	}

	if m := f.ManagedFields(); len(m) != 1 {
		t.Fatalf("labels shouldn't get stripped on apply: %v", m)
	}
}

func getObjectBytes(file string) []byte {
	s, err := ioutil.ReadFile(file)
	if err != nil {
		panic(err)
	}
	return s
}

func TestApplyNewObject(t *testing.T) {
	tests := []struct {
		gvk schema.GroupVersionKind
		obj []byte
	}{
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Pod"),
			obj: getObjectBytes("pod.yaml"),
		},
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Node"),
			obj: getObjectBytes("node.yaml"),
		},
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Endpoints"),
			obj: getObjectBytes("endpoints.yaml"),
		},
	}

	for _, test := range tests {
		t.Run(test.gvk.String(), func(t *testing.T) {
			f := NewTestFieldManager(test.gvk)

			if err := f.Apply(test.obj, "fieldmanager_test", false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func BenchmarkNewObject(b *testing.B) {
	tests := []struct {
		gvk schema.GroupVersionKind
		obj []byte
	}{
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Pod"),
			obj: getObjectBytes("pod.yaml"),
		},
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Node"),
			obj: getObjectBytes("node.yaml"),
		},
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Endpoints"),
			obj: getObjectBytes("endpoints.yaml"),
		},
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		b.Fatalf("Failed to add to scheme: %v", err)
	}

	for _, test := range tests {
		b.Run(test.gvk.Kind, func(b *testing.B) {
			f := NewTestFieldManager(test.gvk)
			decoder := serializer.NewCodecFactory(scheme).UniversalDecoder(test.gvk.GroupVersion())
			newObj, err := runtime.Decode(decoder, test.obj)
			if err != nil {
				b.Fatalf("Failed to parse yaml object: %v", err)
			}
			objMeta, err := meta.Accessor(newObj)
			if err != nil {
				b.Fatalf("Failed to get object meta: %v", err)
			}
			objMeta.SetManagedFields([]metav1.ManagedFieldsEntry{
				{
					Manager:    "default",
					Operation:  "Update",
					APIVersion: "v1",
				},
			})

			b.Run("Update", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					err := f.Update(newObj, "fieldmanager_test")
					if err != nil {
						b.Fatal(err)
					}
					f.Reset()
				}
			})
			b.Run("Apply", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					err := f.Apply(test.obj, "fieldmanager_test", false)
					if err != nil {
						b.Fatal(err)
					}
					f.Reset()
				}
			})
		})
	}
}

func BenchmarkRepeatedUpdate(b *testing.B) {
	f := NewTestFieldManager(schema.FromAPIVersionAndKind("v1", "Pod"))

	podBytes := getObjectBytes("pod.yaml")

	var obj1 *corev1.Pod
	if err := yaml.Unmarshal(podBytes, &obj1); err != nil {
		b.Fatalf("Failed to parse yaml object: %v", err)
	}
	obj1.Spec.Containers[0].Image = "nginx:latest"
	obj2 := obj1.DeepCopy()
	obj2.Spec.Containers[0].Image = "nginx:4.3"

	toUnstructured := func(o runtime.Object) *unstructured.Unstructured {
		data, err := json.Marshal(o)
		if err != nil {
			b.Fatalf("Failed to marshal to json: %v", err)
		}
		u := &unstructured.Unstructured{Object: map[string]interface{}{}}
		err = json.Unmarshal(data, u)
		if err != nil {
			b.Fatalf("Failed to unmarshal to json: %v", err)
		}
		return u
	}

	tests := []struct {
		name string
		objs []runtime.Object
	}{
		{
			name: "structured",
			objs: []runtime.Object{obj1, obj2},
		},
		{
			name: "unstructured",
			objs: []runtime.Object{toUnstructured(obj1), toUnstructured(obj2)},
		},
	}

	for _, tc := range tests {
		b.Run(tc.name, func(b *testing.B) {
			err := f.Apply(podBytes, "fieldmanager_apply", false)
			if err != nil {
				b.Fatal(err)
			}

			if err := f.Update(tc.objs[1], "fieldmanager_1"); err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				err := f.Update(tc.objs[n%len(tc.objs)], fmt.Sprintf("fieldmanager_%d", n%len(tc.objs)))
				if err != nil {
					b.Fatal(err)
				}
				f.Reset()
			}
		})
	}
}

func toUnstructured(b *testing.B, o runtime.Object) *unstructured.Unstructured {
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(o)
	if err != nil {
		b.Fatalf("Failed to unmarshal to json: %v", err)
	}
	return &unstructured.Unstructured{Object: u}
}

func BenchmarkConvertObjectToTyped(b *testing.B) {
	tests := []struct {
		gvk schema.GroupVersionKind
		obj []byte
	}{
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Pod"),
			obj: getObjectBytes("pod.yaml"),
		},
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Node"),
			obj: getObjectBytes("node.yaml"),
		},
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Endpoints"),
			obj: getObjectBytes("endpoints.yaml"),
		},
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		b.Fatalf("Failed to add to scheme: %v", err)
	}

	for _, test := range tests {
		b.Run(test.gvk.Kind, func(b *testing.B) {
			decoder := serializer.NewCodecFactory(scheme).UniversalDecoder(test.gvk.GroupVersion())
			obj, err := runtime.Decode(decoder, test.obj)
			if err != nil {
				b.Fatal(err)
			}

			m := NewFakeOpenAPIModels()
			typeConverter := NewFakeTypeConverter(m)
			//var typeConverter internal.TypeConverter = internal.DeducedTypeConverter{}

			b.Run("structured", func(b *testing.B) {
				b.ReportAllocs()
				for n := 0; n < b.N; n++ {
					_, err := typeConverter.ObjectToTyped(obj)
					if err != nil {
						b.Errorf("Error in ObjectToTyped: %v", err)
					}
				}
			})
			// b.Run("unstructured", func(b *testing.B) {
			// 	b.ReportAllocs()
			// 	for n := 0; n < b.N; n++ {
			// 		_, err := typeConverter.ObjectToTyped(toUnstructured(b, obj))
			// 		if err != nil {
			// 			b.Errorf("Error in ObjectToTyped: %v", err)
			// 		}
			// 	}
			// })
		})
	}
}

func BenchmarkToFromUnstructured(b *testing.B) {
	tests := []struct {
		gvk schema.GroupVersionKind
		obj []byte
	}{
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Pod"),
			obj: getObjectBytes("pod.yaml"),
		},
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Node"),
			obj: getObjectBytes("node.yaml"),
		},
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Endpoints"),
			obj: getObjectBytes("endpoints.yaml"),
		},
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		b.Fatalf("Failed to add to scheme: %v", err)
	}

	for _, test := range tests {
		b.Run(test.gvk.Kind, func(b *testing.B) {
			decoder := serializer.NewCodecFactory(scheme).UniversalDecoder(test.gvk.GroupVersion())
			obj, err := runtime.Decode(decoder, test.obj)
			if err != nil {
				b.Fatal(err)
			}


			b.Run("ToUnstructured", func(b *testing.B) {
				b.ReportAllocs()
				for n := 0; n < b.N; n++ {
					_, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
					if err != nil {
						b.Errorf("Error in ToUnstructured: %v", err)
					}
				}
			})
			u := toUnstructured(b, obj)
			b.Run("FromUnstructured", func(b *testing.B) {
				b.ReportAllocs()
				for n := 0; n < b.N; n++ {
					obj, err := scheme.New(test.gvk)
					if err != nil {
						b.Errorf("Error in scheme.New: %v", err)
					}
					err = runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, obj)
					if err != nil {
						b.Errorf("Error in FromUnstructured: %v", err)
					}
				}
			})
		})
	}
}

func BenchmarkCompare(b *testing.B) {
	tests := []struct {
		gvk schema.GroupVersionKind
		obj []byte
	}{
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Pod"),
			obj: getObjectBytes("pod.yaml"),
		},
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Node"),
			obj: getObjectBytes("node.yaml"),
		},
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Endpoints"),
			obj: getObjectBytes("endpoints.yaml"),
		},
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		b.Fatalf("Failed to add to scheme: %v", err)
	}

	for _, test := range tests {
		b.Run(test.gvk.Kind, func(b *testing.B) {
			decoder := serializer.NewCodecFactory(scheme).UniversalDecoder(test.gvk.GroupVersion())
			obj, err := runtime.Decode(decoder, test.obj)
			if err != nil {
				b.Fatal(err)
			}

			m := NewFakeOpenAPIModels()
			typeConverter := NewFakeTypeConverter(m)
			//var typeConverter internal.TypeConverter = internal.DeducedTypeConverter{}

			tv1, err := typeConverter.ObjectToTyped(obj)
			if err != nil {
				b.Errorf("Error in ObjectToTyped: %v", err)
			}
			tv2, err := typeConverter.ObjectToTyped(obj)
			if err != nil {
				b.Errorf("Error in ObjectToTyped: %v", err)
			}

			b.Run("structured", func(b *testing.B) {
				b.ReportAllocs()
				for n := 0; n < b.N; n++ {
					_, err = tv1.Compare(tv2)
					if err != nil {
						b.Errorf("Error in ObjectToTyped: %v", err)
					}
				}
			})
			utv1, err := typeConverter.ObjectToTyped(toUnstructured(b, obj))
			if err != nil {
				b.Errorf("Error in ObjectToTyped: %v", err)
			}
			utv2, err := typeConverter.ObjectToTyped(toUnstructured(b, obj))
			if err != nil {
				b.Errorf("Error in ObjectToTyped: %v", err)
			}
			b.Run("unstructured", func(b *testing.B) {
				b.ReportAllocs()
				for n := 0; n < b.N; n++ {
					_, err = utv1.Compare(utv2)
					if err != nil {
						b.Errorf("Error in ObjectToTyped: %v", err)
					}
				}
			})
		})
	}
}

func BenchmarkMerge(b *testing.B) {
	tests := []struct {
		gvk schema.GroupVersionKind
		obj []byte
	}{
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Pod"),
			obj: getObjectBytes("pod.yaml"),
		},
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Node"),
			obj: getObjectBytes("node.yaml"),
		},
		{
			gvk: schema.FromAPIVersionAndKind("v1", "Endpoints"),
			obj: getObjectBytes("endpoints.yaml"),
		},
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		b.Fatalf("Failed to add to scheme: %v", err)
	}

	for _, test := range tests {
		b.Run(test.gvk.Kind, func(b *testing.B) {
			decoder := serializer.NewCodecFactory(scheme).UniversalDecoder(test.gvk.GroupVersion())
			obj, err := runtime.Decode(decoder, test.obj)
			if err != nil {
				b.Fatal(err)
			}

			m := NewFakeOpenAPIModels()
			typeConverter := NewFakeTypeConverter(m)
			//var typeConverter internal.TypeConverter = internal.DeducedTypeConverter{}

			b.Run("structured", func(b *testing.B) {
				b.ReportAllocs()
				for n := 0; n < b.N; n++ {
					tv1, err := typeConverter.ObjectToTyped(obj)
					if err != nil {
						b.Errorf("Error in ObjectToTyped: %v", err)
					}
					tv2, err := typeConverter.ObjectToTyped(obj)
					if err != nil {
						b.Errorf("Error in ObjectToTyped: %v", err)
					}
					_, err = tv1.Merge(tv2)
					if err != nil {
						b.Errorf("Error in ObjectToTyped: %v", err)
					}
				}
			})
			b.Run("unstructured", func(b *testing.B) {
				b.ReportAllocs()
				for n := 0; n < b.N; n++ {
					utv1, err := typeConverter.ObjectToTyped(toUnstructured(b, obj))
					if err != nil {
						b.Errorf("Error in ObjectToTyped: %v", err)
					}
					utv2, err := typeConverter.ObjectToTyped(toUnstructured(b, obj))
					if err != nil {
						b.Errorf("Error in ObjectToTyped: %v", err)
					}
					_, err = utv1.Merge(utv2)
					if err != nil {
						b.Errorf("Error in ObjectToTyped: %v", err)
					}
				}
			})
		})
	}
}

func TestApplyFailsWithManagedFields(t *testing.T) {
	f := NewTestFieldManager(schema.FromAPIVersionAndKind("v1", "Pod"))

	err := f.Apply([]byte(`{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"managedFields": [
				{
				  "manager": "test",
				}
			]
		}
	}`), "fieldmanager_test", false)

	if err == nil {
		t.Fatalf("successfully applied with set managed fields")
	}
}

func TestApplySuccessWithNoManagedFields(t *testing.T) {
	f := NewTestFieldManager(schema.FromAPIVersionAndKind("v1", "Pod"))

	err := f.Apply([]byte(`{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"labels": {
				"a": "b"
			},
		}
	}`), "fieldmanager_test", false)

	if err != nil {
		t.Fatalf("failed to apply object: %v", err)
	}
}

func TestValueFormats(t *testing.T) {
	apifuzzer := fuzzer.FuzzerFor(apitesting.FuzzerFuncs, rand.NewSource(100), legacyscheme.Codecs)

	toRaw := func(v value.Value) map[string]interface{} {
		raw := map[string]interface{}{}
		js, err := value.ToJSON(v)
		if err != nil {
			t.Fatalf("Failed to serialize to json: %v", err)
		}
		err = json.Unmarshal(js, &raw)
		if err != nil {
			t.Fatalf("Failed to deserialize from json: %v", err)
		}
		return raw
	}

	// for _, version := range []schema.GroupVersion{{Group: "", Version: runtime.APIVersionInternal}, {Group: "", Version: "v1"}} {
	// 			f := fuzzer.FuzzerFor(FuzzerFuncs, rand.NewSource(rand.Int63()), legacyscheme.Codecs)

	t.Logf("AllKnownTypesSize: %d", len(legacyscheme.Scheme.AllKnownTypes()))
	for gvk, typ := range legacyscheme.Scheme.AllKnownTypes() {
		if gvk.Version == runtime.APIVersionInternal {
			// internal versions are not serialized to protobuf
			continue
		}

		native := reflect.New(typ).Interface()
		t.Run(gvk.String(), func(t *testing.T) {
			for i := 0; i < 10; i++ {
				t.Run(fmt.Sprintf("fuzz-%d", i), func(t *testing.T) {
					apifuzzer.Fuzz(native)

					nativeData, err := yaml.Marshal(native)
					if err != nil {
						t.Fatalf("Failed to serialize nativeData: %v", err)
					}

					var v interface{}
					if err := yaml.Unmarshal(nativeData, &v); err != nil {
						t.Fatalf("error decoding YAML: %v", err)
					}

					reflectVal, err := value.NewValueReflect(native)
					if err != nil {
						t.Errorf("Error creating reflectValue: %v", err)
					}
					raw1 := toRaw(reflectVal)
					raw2 := toRaw(value.NewValueInterface(v))

					// TODO: Use value.Equals once it has been fixed
					if !reflect.DeepEqual(raw1, raw2) {
						t.Errorf("Expected reflection and unstructured values to match, but got:\n%s\n!=\n%s\ndiff:\n%s", raw1, raw2, cmp.Diff(raw1, raw2))
					}
				})
			}
		})
	}
}
