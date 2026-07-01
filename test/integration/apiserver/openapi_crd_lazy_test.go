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
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
)

// TestOpenAPICRDLazyCorrectness verifies that with OpenAPILazyGraph on, CRD schemas
// are still published to BOTH /openapi/v3 and /openapi/v2 (i.e. the weak paths do
// not drop CRD content). This guards against the weak handler ignoring dynamic
// (CRD-driven) spec updates.
func TestOpenAPICRDLazyCorrectness(t *testing.T) {
	lazy := os.Getenv("OPENAPI_LAZY") == "1"
	t.Logf("gate OpenAPILazyGraph=%v", lazy)
	server := startServer(t, lazy)
	defer server.TearDownFn()
	config := server.ClientConfig

	apiext, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	client, err := clientset.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	const n = 3
	groups := make([]string, 0, n)
	for i := 0; i < n; i++ {
		crd := openapiMemBenchCRD(i)
		if _, err := apiext.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create CRD %d: %v", i, err)
		}
		groups = append(groups, crd.Spec.Group)
		if err := openapiMemWaitEstablished(ctx, apiext, openapiMemBenchCRDName(i)); err != nil {
			t.Fatalf("CRD %d not established: %v", i, err)
		}
	}
	// Wait for the CRD group-versions to appear in v3 discovery.
	seen := openapiMemWaitV3Published(t, client, groups, 60*time.Second)
	if seen != len(groups) {
		t.Fatalf("gate on: only %d/%d CRD group-versions published to v3 discovery", seen, len(groups))
	}

	// v3: each CRD's per-GV doc must contain the Widget kind.
	disco := openapiMemV3Discovery(t, client)
	rt, err := restclient.TransportFor(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		key := "apis/" + g + "/v1"
		gv, ok := disco.Paths[key]
		if !ok {
			t.Errorf("v3 discovery missing %s", key)
			continue
		}
		req, _ := http.NewRequest("GET", config.Host+gv.ServerRelativeURL, nil)
		resp, err := rt.RoundTrip(req)
		if err != nil {
			t.Errorf("GET %s: %v", gv.ServerRelativeURL, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if !bytes.Contains(body, []byte("Widget")) {
			t.Errorf("gate on: v3 doc for %s does not contain the CRD kind (len=%d)", key, len(body))
		}
	}

	// v2: the merged spec must eventually contain each CRD group (the aggregated
	// /openapi/v2 re-downloads delegates on a resync, so this lags establishment).
	var v2 []byte
	deadline := time.Now().Add(90 * time.Second)
	for {
		v2 = fetchV2JSON(t, config)
		missing := false
		for _, g := range groups {
			if !strings.Contains(string(v2), g) {
				missing = true
				break
			}
		}
		if !missing {
			break
		}
		if time.Now().After(deadline) {
			for _, g := range groups {
				if !strings.Contains(string(v2), g) {
					t.Errorf("gate on: /openapi/v2 still missing CRD group %s after 90s (len=%d)", g, len(v2))
				}
			}
			break
		}
		time.Sleep(2 * time.Second)
	}
	t.Logf("gate on: %d CRDs published to v3 and v2 (v2 len=%d)", len(groups), len(v2))
}
