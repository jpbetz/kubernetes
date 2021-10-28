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

package apiserver

import (
	"context"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apiextensions-apiserver/test/integration/fixtures"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
	genericfeatures "k8s.io/apiserver/pkg/features"
	"k8s.io/apiserver/pkg/storage/names"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/dynamic"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	apiservertesting "k8s.io/kubernetes/cmd/kube-apiserver/app/testing"
	"k8s.io/kubernetes/test/integration/framework"
	"testing"
)

func TestCustomResourceExpressionCompilation(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, genericfeatures.CustomResourceValidationExpressions, true)()

	server, err := apiservertesting.StartTestServer(t, apiservertesting.NewDefaultTestServerOptions(), nil, framework.SharedEtcd())
	if err != nil {
		t.Fatal(err)
	}
	defer server.TearDownFn()
	config := server.ClientConfig

	apiExtensionClient, err := clientset.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}

	noxuDefinition := fixtures.NewNoxuV1CustomResourceDefinition(apiextensionsv1.ClusterScoped)
	var c apiextensionsv1.CustomResourceValidation
	err = json.Unmarshal(schemaWithValidationExpression, &c)
	if err != nil {
		t.Fatal(err)
	}
	for i := range noxuDefinition.Spec.Versions {
		noxuDefinition.Spec.Versions[i].Schema = &c
	}

	crd, err := fixtures.CreateNewV1CustomResourceDefinition(noxuDefinition, apiExtensionClient, dynamicClient)
	if err != nil {
		t.Fatal(err)
	}
	gvr := schema.GroupVersionResource{
		Group:    crd.Spec.Group,
		Version:  crd.Spec.Versions[0].Name,
		Resource: crd.Spec.Names.Plural,
	}
	crClient := dynamicClient.Resource(gvr)

	name1 := names.SimpleNameGenerator.GenerateName("cr-1")
	_, err = crClient.Create(context.TODO(), &unstructured.Unstructured{Object: map[string]interface{}{
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
	if err != nil {
		t.Fatalf("Failed to create custom resource: %v", err)
	}
}

func TestCustomResourceExpressionValidation(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, genericfeatures.CustomResourceValidationExpressions, true)()

	server, err := apiservertesting.StartTestServer(t, apiservertesting.NewDefaultTestServerOptions(), nil, framework.SharedEtcd())
	if err != nil {
		t.Fatal(err)
	}
	defer server.TearDownFn()
	config := server.ClientConfig

	apiExtensionClient, err := clientset.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}

	noxuDefinition := fixtures.NewNoxuV1CustomResourceDefinition(apiextensionsv1.ClusterScoped)
	var c apiextensionsv1.CustomResourceValidation
	err = json.Unmarshal(schemaWithValidationExpression, &c)
	if err != nil {
		t.Fatal(err)
	}
	for i := range noxuDefinition.Spec.Versions {
		noxuDefinition.Spec.Versions[i].Schema = &c
	}

	crd, err := fixtures.CreateNewV1CustomResourceDefinition(noxuDefinition, apiExtensionClient, dynamicClient)
	if err != nil {
		t.Fatal(err)
	}
	gvr := schema.GroupVersionResource{
		Group:    crd.Spec.Group,
		Version:  crd.Spec.Versions[0].Name,
		Resource: crd.Spec.Names.Plural,
	}
	crClient := dynamicClient.Resource(gvr)

	t.Run("create a valid custom resource", func(t *testing.T) {
		name1 := names.SimpleNameGenerator.GenerateName("cr-1")
		_, err = crClient.Create(context.TODO(), &unstructured.Unstructured{Object: map[string]interface{}{
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
		if err != nil {
			t.Fatalf("Failed to create custom resource: %v", err)
		}
	})
	t.Run("create an invalid custom resource", func(t *testing.T) {
		name1 := names.SimpleNameGenerator.GenerateName("cr-1")
		_, err = crClient.Create(context.TODO(), &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": gvr.Group + "/" + gvr.Version,
			"kind":       crd.Spec.Names.Kind,
			"metadata": map[string]interface{}{
				"name": name1,
			},
			"spec": map[string]interface{}{
				"x": int64(-1),
				"y": int64(0),
			},
		}}, metav1.CreateOptions{})
		if err == nil {
			t.Fatalf("Expected create of invalid custom resource to fail: %v", err)
		}
	})
}

var schemaWithValidationExpression = []byte(`
{
  "openAPIV3Schema": {
    "description": "CRD with CEL validation expressions",
    "type": "object",
    "properties": {
      "spec": {
        "type": "object",
        "x-kubernetes-validator": [
          {
            "rule": "x + y > 0"
          }
        ],
        "properties": {
          "x": {
            "type": "integer"
          },
          "y": {
            "type": "integer"
          }
        }
      },
      "status": {
        "type": "object"
      }
    }
  },
  "x-kubernetes-validator": [
    {
      "rule": "health == 'ok' || health == 'unhealthy'",
      "properties": {
        "health": {
          "type": "string"
        }
      }
    }
  ]
}`)

var schemaWithIllegalValidationExpression = []byte(`{
  "openAPIV3Schema": {
    "description": "CRD with CEL validation expressions",
    "type": "object",
    "properties": {
      "spec": {
        "type": "object",
        "x-kubernetes-validator": [
          {
            "rule": "x.z"
          }
        ],
        "properties": {
          "x": {
            "type": "integer"
          },
          "y": {
            "type": "integer"
          }
        }
      },
      "status": {
        "type": "object"
      }
    }
  },
  "x-kubernetes-validator": [
    {
      "rule": "health > 1",
      "properties": {
        "health": {
          "type": "string"
        }
      }
    }
  ]
}`)
