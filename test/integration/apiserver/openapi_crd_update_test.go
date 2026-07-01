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

package apiserver

import (
	"context"
	"strings"
	"testing"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
)

func crdWithProperty(group, prop string) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "widgets." + group},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: group,
			Scope: apiextensionsv1.ClusterScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: "widgets", Singular: "widget", Kind: "Widget", ListKind: "WidgetList",
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: "v1", Served: true, Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type: "object",
						Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"spec": {Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
								prop: {Type: "string"},
							}},
						},
					},
				},
			}},
		},
	}
}

func waitV2Contains(t *testing.T, config *restclient.Config, needle string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(string(fetchV2JSON(t, config)), needle) {
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

// TestOpenAPICRDv2ContentUpdateNotStale guards against the weak-path staleness bug:
// a content-only update of an existing CRD's schema must be reflected in the
// aggregated /openapi/v2 (gate on), not served stale until the weak bytes are
// GC-reclaimed. On the buggy code the update-branch never bumps the weak
// generation, so the new field never appears.
func TestOpenAPICRDv2ContentUpdateNotStale(t *testing.T) {
	server := startServer(t, true) // OpenAPILazyGraph=true
	defer server.TearDownFn()
	config := server.ClientConfig
	apiext := apiextensionsclient.NewForConfigOrDie(config)
	ctx := context.Background()
	const group = "updatetest.example.com"

	crd := crdWithProperty(group, "fieldalpha")
	if _, err := apiext.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := openapiMemWaitEstablished(ctx, apiext, crd.Name); err != nil {
		t.Fatal(err)
	}
	if !waitV2Contains(t, config, "fieldalpha", 60*time.Second) {
		t.Fatalf("initial CRD schema (fieldalpha) never appeared in /openapi/v2")
	}

	// Content-only update: replace the schema's property name.
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest, err := apiext.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, crd.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		latest.Spec.Versions[0].Schema = crdWithProperty(group, "fieldbravo").Spec.Versions[0].Schema
		_, err = apiext.ApiextensionsV1().CustomResourceDefinitions().Update(ctx, latest, metav1.UpdateOptions{})
		return err
	}); err != nil {
		t.Fatalf("update CRD: %v", err)
	}

	if !waitV2Contains(t, config, "fieldbravo", 60*time.Second) {
		t.Fatalf("gate on: /openapi/v2 never reflected the CRD schema UPDATE (stale weak cache)")
	}
}
