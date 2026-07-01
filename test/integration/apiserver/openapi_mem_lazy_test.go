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
	"os"
	"testing"
	"time"

	"k8s.io/client-go/dynamic"
	clientset "k8s.io/client-go/kubernetes"
	kubeapiservertesting "k8s.io/kubernetes/cmd/kube-apiserver/app/testing"
	"k8s.io/kubernetes/test/integration/framework"
)

// TestOpenAPIMemLazy measures resident OpenAPI with the OpenAPILazyGraph gate
// off vs on, using the same warming as TestOpenAPIMemIdle. Toggle with the
// OPENAPI_LAZY env var (1 = gate on). Dumps lazy-off.heap / lazy-on.heap for
// pprof comparison. Run twice:
//
//	OPENAPI_LAZY=0 go test ... -run ^TestOpenAPIMemLazy$
//	OPENAPI_LAZY=1 go test ... -run ^TestOpenAPIMemLazy$
func TestOpenAPIMemLazy(t *testing.T) {
	var flags []string
	tag := "off"
	heap := openapiMemPlanDir + "/lazy-off.heap"
	if os.Getenv("OPENAPI_LAZY") == "1" {
		flags = []string{"--feature-gates=OpenAPILazyGraph=true"}
		tag = "on"
		heap = openapiMemPlanDir + "/lazy-on.heap"
	}

	server, err := kubeapiservertesting.StartTestServer(t, kubeapiservertesting.NewDefaultTestServerOptions(), flags, framework.SharedEtcd())
	if err != nil {
		t.Fatal(err)
	}
	defer server.TearDownFn()
	config := server.ClientConfig

	client, err := clientset.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}

	nLists := openapiMemWarmResources(t, client, dyn)
	v2Etag, v2Size := openapiMemFetchV2Proto(t, config)
	nGV, v3Total := openapiMemWarmV3(t, config, client)
	// Second round exercises the 304 / resident-etag path (gate on) and, after
	// GC, the weak bytes materialized above should be reclaimed.
	openapiMemFetchV2Proto(t, config)
	openapiMemWarmV3(t, config, client)
	t.Logf("[gate=%s] warmed lists=%d v2Bytes=%d v2etag=%s v3GVs=%d v3Bytes=%d", tag, nLists, v2Size, v2Etag, nGV, v3Total)

	time.Sleep(8 * time.Second)
	openapiMemSnapshot(t, "lazy-"+tag, heap)
}
