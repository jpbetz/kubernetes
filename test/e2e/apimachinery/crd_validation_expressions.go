/*
Copyright 2021 The Kubernetes Authors.

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

package apimachinery

import (
	"context"
	"github.com/onsi/ginkgo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/features"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/client-go/dynamic"
	"k8s.io/kubernetes/test/e2e/framework"
	e2eskipper "k8s.io/kubernetes/test/e2e/framework/skipper"
)

var _ = SIGDescribe("CustomResourceValidationExpressions [Privileged:ClusterAdmin]", func() {
	f := framework.NewDefaultFramework("crd-validation-expressions")

	testCrd, err := setupCRD(f, schemaWithValidationExpression, "common-group", "v1")
	if err != nil {
		framework.Failf("%v", err)
	}
	defer func() {
		if err := cleanupCRD(f, testCrd); err != nil {
			framework.Failf("%v", err)
		}
	}()
	crd := testCrd.Crd

	config, err := framework.LoadConfig()
	framework.ExpectNoError(err, "loading config")

	dynamicClient, err := dynamic.NewForConfig(config)
	framework.ExpectNoError(err, "initializing dynamic client")
	gvr := schema.GroupVersionResource{
		Group:    crd.Spec.Group,
		Version:  crd.Spec.Versions[0].Name,
		Resource: crd.Spec.Names.Plural,
	}

	ginkgo.It("MUST NOT fail validation for create of a custom resource that satisfies the x-kubernetes-validator rules", func() {
		// features.CustomResourceValidationExpressions can be enabled or disabled for this test
		crClient := dynamicClient.Resource(gvr)

		name1 := names.SimpleNameGenerator.GenerateName("cr-1")
		cr, err := crClient.Create(context.TODO(), &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": gvr.Group + "/" + gvr.Version,
			"kind":       crd.Spec.Names.Kind,
			"metadata": map[string]interface{}{
				"name": name1,
			},
			"spec": map[string]interface{}{
				"x": int64(1),
				"y": int64(0),
			},
		}}, metav1.CreateOptions{})
		framework.ExpectNoError(err, "validation rules satisfied")

		cr.Object["status"] = map[string]interface{}{
			"health": "ok",
		}
		_, err = crClient.UpdateStatus(context.TODO(), cr, metav1.UpdateOptions{})
		framework.ExpectNoError(err, "status validation rules satisfied")
	})
	ginkgo.It("MUST fail validation for create of a custom resource that does not satisfy the x-kubernetes-validator rules, when the CustomResourceValidationExpressions feature is enabled", func() {
		e2eskipper.SkipUnlessFeatureGateEnabled(features.CustomResourceValidationExpressions)
		crClient := dynamicClient.Resource(gvr)

		name1 := names.SimpleNameGenerator.GenerateName("cr-1")
		_, err = crClient.Create(context.TODO(), &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": gvr.Group + "/" + gvr.Version,
			"kind":       crd.Spec.Names.Kind,
			"metadata": map[string]interface{}{
				"name": name1,
			},
			"spec": map[string]interface{}{
				"x": int64(0),
				"y": int64(0),
			},
		}}, metav1.CreateOptions{})
		framework.ExpectError(err, "validation rules not satisfied")
		// TODO: check the error contents
	})
	ginkgo.It("MUST fail validation for update of a custom resource status that does not satisfy to the x-kubernetes-validator rules, when the CustomResourceValidationExpressions feature is enabled", func() {
		e2eskipper.SkipUnlessFeatureGateEnabled(features.CustomResourceValidationExpressions)
		crClient := dynamicClient.Resource(gvr)

		name1 := names.SimpleNameGenerator.GenerateName("cr-1")
		cr, err := crClient.Create(context.TODO(), &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": gvr.Group + "/" + gvr.Version,
			"kind":       crd.Spec.Names.Kind,
			"metadata": map[string]interface{}{
				"name": name1,
			},
			"spec": map[string]interface{}{
				"x": int64(1),
				"y": int64(0),
			},
		}}, metav1.CreateOptions{})
		framework.ExpectNoError(err, "validation rules satisfied")

		cr.Object["status"] = map[string]interface{}{
			"health": "confused",
		}
		_, err = crClient.UpdateStatus(context.TODO(), cr, metav1.UpdateOptions{})
		framework.ExpectError(err, "status validation rules not satisfied")
	})
	ginkgo.It("MUST NOT fail validation for create of a custom resource that does not satisfy to the x-kubernetes-validator rules, when the CustomResourceValidationExpressions feature is disabled", func() {
		e2eskipper.SkipIfFeatureGateEnabled(features.CustomResourceValidationExpressions)
		crClient := dynamicClient.Resource(gvr)

		name1 := names.SimpleNameGenerator.GenerateName("cr-1")
		cr, err := crClient.Create(context.TODO(), &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": gvr.Group + "/" + gvr.Version,
			"kind":       crd.Spec.Names.Kind,
			"metadata": map[string]interface{}{
				"name": name1,
			},
			"spec": map[string]interface{}{
				"x": int64(0),
				"y": int64(0),
			},
		}}, metav1.CreateOptions{})
		framework.ExpectNoError(err, "validation expressions should be ignored because CustomResourceValidationExpressions feature is disabled")

		cr.Object["status"] = map[string]interface{}{
			"health": "confused",
		}
		_, err = crClient.UpdateStatus(context.TODO(), cr, metav1.UpdateOptions{})
		framework.ExpectNoError(err, "validation expressions should be ignored because CustomResourceValidationExpressions feature is disabled")
	})
	ginkgo.It("MUST fail validation for create of a custom resource definition with x-kubernetes-validator rules containing invalid syntax", func() {
		// features.CustomResourceValidationExpressions can be enabled or disabled for this test
		_, err = setupCRD(f, schemaWithIllegalValidationExpression, "common-group", "v1")
		framework.ExpectError(err, "x-kubernetes-validator rules containing invalid syntax")
	})
})

var schemaWithValidationExpression = []byte(`description: CRD with CEL validation expressions
type: object
properties:
  spec:
    type: object
	x-kubernetes-validator:
	- rule: "x + y > 0"
    properties:
      x:
        type: integer
      y:
        type: integer
        
  status:
    type: object
	x-kubernetes-validator:
	- rule: "health == 'ok' || health == 'unhealthy'"
    properties:
      health:
        type: string`)

var schemaWithIllegalValidationExpression = []byte(`description: CRD with illegal CEL validation expressions
type: object
properties:
  spec:
    type: object
	x-kubernetes-validator:
	- rule: "x.z"
    properties:
      x:
        type: integer
      y:
        type: integer
        
  status:
    type: object
	x-kubernetes-validator:
	- rule: "health > 1"
    properties:
      health:
        type: string`)