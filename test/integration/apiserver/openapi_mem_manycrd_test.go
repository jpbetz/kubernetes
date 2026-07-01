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
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	kubeapiservertesting "k8s.io/kubernetes/cmd/kube-apiserver/app/testing"
	"k8s.io/kubernetes/test/integration/framework"
)

// TestOpenAPIMemManyCRD installs N CRDs (distinct group each, non-trivial structural v3
// schema, served version v1), warms every OpenAPI surface (v3 discovery + each CRD's
// per-GV v3 doc + the merged v2 protobuf), then dumps an in-use heap profile so the
// resident OpenAPI footprint as a function of N can be attributed. N is set via
// OPENAPI_BENCH_CRDS (default 150).
func TestOpenAPIMemManyCRD(t *testing.T) {
	n := 150
	if v := os.Getenv("OPENAPI_BENCH_CRDS"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("bad OPENAPI_BENCH_CRDS=%q: %v", v, err)
		}
		n = parsed
	}

	var flags []string
	heapTag := "off"
	if os.Getenv("OPENAPI_LAZY") == "1" {
		flags = []string{"--feature-gates=OpenAPILazyGraph=true"}
		heapTag = "on"
	}
	server, err := kubeapiservertesting.StartTestServer(t, kubeapiservertesting.NewDefaultTestServerOptions(), flags, framework.SharedEtcd())
	if err != nil {
		t.Fatal(err)
	}
	defer server.TearDownFn()
	config := server.ClientConfig

	apiextClient, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	client, err := clientset.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	groups := make([]string, 0, n)
	created := 0
	for i := 0; i < n; i++ {
		crd := openapiMemBenchCRD(i)
		if _, err := apiextClient.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{}); err != nil {
			t.Logf("create CRD %d (%s) failed (continuing): %v", i, crd.Name, err)
			continue
		}
		groups = append(groups, crd.Spec.Group)
		created++
	}
	t.Logf("created %d/%d CRDs", created, n)

	// Wait for each created CRD to reach Established.
	establishedCRDs := make([]string, 0, created)
	establishedGroups := make([]string, 0, created)
	for i, g := range groups {
		name := openapiMemBenchCRDName(i)
		if err := openapiMemWaitEstablished(ctx, apiextClient, name); err != nil {
			t.Logf("CRD %s not Established (continuing): %v", name, err)
			continue
		}
		establishedCRDs = append(establishedCRDs, name)
		establishedGroups = append(establishedGroups, g)
	}
	t.Logf("established %d/%d CRDs", len(establishedCRDs), created)

	// OpenAPI publishing to the aggregator lags CRD establishment; wait until the CRD
	// group-versions show up in the v3 discovery doc (best-effort).
	nSeen := openapiMemWaitV3Published(t, client, establishedGroups, 60*time.Second)
	t.Logf("v3 discovery lists %d/%d CRD group-versions", nSeen, len(establishedGroups))

	// Warm: every per-GV v3 doc (built-ins + CRDs) and the merged v2 protobuf.
	nGV, v3Total := openapiMemWarmV3(t, config, client)
	v2Etag, v2Size := openapiMemFetchV2Proto(t, config)
	t.Logf("warmed: v3GVs=%d v3TotalBytes=%d v2protoBytes=%d v2etag=%s", nGV, v3Total, v2Size, v2Etag)

	time.Sleep(10 * time.Second)
	openapiMemSnapshot(t, fmt.Sprintf("manycrd-N=%d-%s", created, heapTag), openapiMemPlanDir+"/manycrd-"+heapTag+".heap")
}

func openapiMemBenchCRDName(i int) string {
	return fmt.Sprintf("widgets.bench%d.example.com", i)
}

// openapiMemBenchCRD returns a cluster-scoped CRD in a distinct group with a non-trivial
// structural OpenAPI v3 schema (~20 properties incl. nested objects and arrays), served
// version v1 only.
func openapiMemBenchCRD(i int) *apiextensionsv1.CustomResourceDefinition {
	group := fmt.Sprintf("bench%d.example.com", i)
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: openapiMemBenchCRDName(i)},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: group,
			Scope: apiextensionsv1.ClusterScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "widgets",
				Singular: "widget",
				Kind:     "Widget",
				ListKind: "WidgetList",
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Subresources: &apiextensionsv1.CustomResourceSubresources{
						Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
					},
					Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: openapiMemBenchSchema()},
				},
			},
		},
	}
}

func openapiMemBenchSchema() *apiextensionsv1.JSONSchemaProps {
	str := func() apiextensionsv1.JSONSchemaProps { return apiextensionsv1.JSONSchemaProps{Type: "string"} }
	strDesc := func(d string) apiextensionsv1.JSONSchemaProps {
		return apiextensionsv1.JSONSchemaProps{Type: "string", Description: d}
	}
	integer := apiextensionsv1.JSONSchemaProps{Type: "integer", Format: "int64"}
	boolean := apiextensionsv1.JSONSchemaProps{Type: "boolean"}

	keyValueObj := apiextensionsv1.JSONSchemaProps{
		Type: "object",
		Properties: map[string]apiextensionsv1.JSONSchemaProps{
			"name":  str(),
			"value": str(),
		},
	}
	stringMap := apiextensionsv1.JSONSchemaProps{
		Type:                 "object",
		AdditionalProperties: &apiextensionsv1.JSONSchemaPropsOrBool{Allows: true, Schema: &apiextensionsv1.JSONSchemaProps{Type: "string"}},
	}

	spec := apiextensionsv1.JSONSchemaProps{
		Type:        "object",
		Description: "WidgetSpec configures a widget.",
		Properties: map[string]apiextensionsv1.JSONSchemaProps{
			"replicas":       integer,
			"image":          strDesc("container image reference"),
			"enabled":        boolean,
			"priority":       integer,
			"timeoutSeconds": integer,
			"description":    apiextensionsv1.JSONSchemaProps{Type: "string", MaxLength: ptrInt64(1024)},
			"tier":           apiextensionsv1.JSONSchemaProps{Type: "string", Enum: []apiextensionsv1.JSON{{Raw: []byte(`"gold"`)}, {Raw: []byte(`"silver"`)}, {Raw: []byte(`"bronze"`)}}},
			"labels":         stringMap,
			"annotations":    stringMap,
			"tags":           apiextensionsv1.JSONSchemaProps{Type: "array", Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &apiextensionsv1.JSONSchemaProps{Type: "string"}}},
			"env":            apiextensionsv1.JSONSchemaProps{Type: "array", Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &keyValueObj}},
			"ports": apiextensionsv1.JSONSchemaProps{
				Type: "array",
				Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"name":          str(),
						"containerPort": integer,
						"protocol":      str(),
					},
				}},
			},
			"strategy": apiextensionsv1.JSONSchemaProps{
				Type: "object",
				Properties: map[string]apiextensionsv1.JSONSchemaProps{
					"type":           str(),
					"maxSurge":       integer,
					"maxUnavailable": integer,
				},
			},
			"selector": apiextensionsv1.JSONSchemaProps{
				Type:       "object",
				Properties: map[string]apiextensionsv1.JSONSchemaProps{"matchLabels": stringMap},
			},
			"resources": apiextensionsv1.JSONSchemaProps{
				Type: "object",
				Properties: map[string]apiextensionsv1.JSONSchemaProps{
					"limits":   stringMap,
					"requests": stringMap,
				},
			},
			"volumeMounts": apiextensionsv1.JSONSchemaProps{
				Type: "array",
				Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"name":      str(),
						"mountPath": str(),
						"readOnly":  boolean,
					},
				}},
			},
			"config": apiextensionsv1.JSONSchemaProps{
				Type: "object",
				Properties: map[string]apiextensionsv1.JSONSchemaProps{
					"nested": apiextensionsv1.JSONSchemaProps{
						Type: "object",
						Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"deep":     str(),
							"deepInt":  integer,
							"deepBool": boolean,
						},
					},
				},
			},
		},
	}

	status := apiextensionsv1.JSONSchemaProps{
		Type: "object",
		Properties: map[string]apiextensionsv1.JSONSchemaProps{
			"observedGeneration": integer,
			"readyReplicas":      integer,
			"phase":              str(),
			"conditions": apiextensionsv1.JSONSchemaProps{
				Type: "array",
				Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"type":    str(),
						"status":  str(),
						"reason":  str(),
						"message": str(),
					},
				}},
			},
		},
	}

	return &apiextensionsv1.JSONSchemaProps{
		Type: "object",
		Properties: map[string]apiextensionsv1.JSONSchemaProps{
			"spec":   spec,
			"status": status,
		},
	}
}

func ptrInt64(v int64) *int64 { return &v }

func openapiMemWaitEstablished(ctx context.Context, c apiextensionsclient.Interface, name string) error {
	return wait.PollUntilContextTimeout(ctx, 200*time.Millisecond, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		crd, err := c.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
}

// openapiMemWaitV3Published polls the v3 discovery doc until all given CRD groups (at v1)
// appear, or timeout. Returns the number of CRD group-versions seen.
func openapiMemWaitV3Published(t *testing.T, client clientset.Interface, groups []string, timeout time.Duration) int {
	want := make(map[string]bool, len(groups))
	for _, g := range groups {
		want[g+"/v1"] = true
	}
	var seen int
	_ = wait.PollUntilContextTimeout(context.Background(), 500*time.Millisecond, timeout, true, func(ctx context.Context) (bool, error) {
		disco := openapiMemV3Discovery(t, client)
		seen = 0
		for _, g := range groups {
			key := "apis/" + g + "/v1"
			if _, ok := disco.Paths[key]; ok {
				seen++
			}
		}
		return seen >= len(groups), nil
	})
	return seen
}
