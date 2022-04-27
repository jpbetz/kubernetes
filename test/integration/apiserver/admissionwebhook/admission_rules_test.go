/*
Copyright 2022 The Kubernetes Authors.

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

package admissionwebhook

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	v1 "k8s.io/api/admissionregistration/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apiextensions-apiserver/test/integration/fixtures"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	apiservertesting "k8s.io/kubernetes/cmd/kube-apiserver/app/testing"

	"k8s.io/kubernetes/test/integration/framework"
)

func TestCRDRule(t *testing.T) {
	server, err := apiservertesting.StartTestServer(t, apiservertesting.NewDefaultTestServerOptions(), nil, framework.SharedEtcd())
	if err != nil {
		t.Fatal(err)
	}
	defer server.TearDownFn()

	client, err := kubernetes.NewForConfig(server.ClientConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	apiExtensionClient, err := clientset.NewForConfig(server.ClientConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dynamicClient, err := dynamic.NewForConfig(server.ClientConfig)
	if err != nil {
		t.Fatal(err)
	}

	structuralWithValidators := crdWithSchema(t, "Structural", structuralSchema)
	crd, err := fixtures.CreateNewV1CustomResourceDefinition(structuralWithValidators, apiExtensionClient, dynamicClient)
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

	// register a validation rule
	_, err = client.AdmissionregistrationV1().ValidatingRuleConfigurations().Create(context.TODO(),
		&v1.ValidatingRuleConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "rule1",
			},
			ValidatingRules: []v1.ValidatingRule{
				{
					Name:        "rule1.1",
					Validations: []v1.Validation{{Rule: "self.spec.x == 'abc'"}},
					MatchRules: []v1.RuleWithOperations{{
						Operations: []v1.OperationType{v1.Create, v1.Update},
						Rule:       v1.Rule{APIGroups: []string{gvr.Group}, APIVersions: []string{gvr.Version}, Resources: []string{gvr.Resource}},
					}},
				},
			},
		},
		metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Second) // TODO: properly wait for rule to register

	_, err = crClient.Create(context.TODO(), &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": gvr.Group + "/" + gvr.Version,
		"kind":       crd.Spec.Names.Kind,
		"metadata": map[string]interface{}{
			"name": name1,
		},
		"spec": map[string]interface{}{
			"x": "abc",
		},
	}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	name2 := names.SimpleNameGenerator.GenerateName("cr-1")

	_, err = crClient.Create(context.TODO(), &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": gvr.Group + "/" + gvr.Version,
		"kind":       crd.Spec.Names.Kind,
		"metadata": map[string]interface{}{
			"name": name2,
		},
		"spec": map[string]interface{}{
			"x": "nope",
		},
	}}, metav1.CreateOptions{})
	if err == nil {
		t.Fatal("expected create to fail due to admission rule")
	}
}

func crdWithSchema(t *testing.T, kind string, schemaJson []byte) *apiextensionsv1.CustomResourceDefinition {
	plural := strings.ToLower(kind) + "s"
	var c apiextensionsv1.CustomResourceValidation
	err := json.Unmarshal(schemaJson, &c)
	if err != nil {
		t.Fatal(err)
	}

	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s.mygroup.example.com", plural)},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "mygroup.example.com",
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1beta1",
				Served:  true,
				Storage: true,
				Schema:  &c,
				Subresources: &apiextensionsv1.CustomResourceSubresources{
					Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
				},
			}},
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: plural,
				Kind:   kind,
			},
			Scope: apiextensionsv1.ClusterScoped,
		},
	}
}

var structuralSchema = []byte(`
{
  "openAPIV3Schema": {
    "description": "CRD with CEL validators",
    "type": "object",
    "properties": {
	  "metadata": {
        "type": "object",
        "properties": {
		  "name": { "type": "string" }
	    }
      },
      "spec": {
        "type": "object",
        "properties": {
          "x": { "type": "string" }
        }
      },
      "status": {
        "type": "object",
        "properties": {}
	  }
    }
  }
}`)
