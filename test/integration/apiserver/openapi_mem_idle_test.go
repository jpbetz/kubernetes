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
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"slices"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	kubeapiservertesting "k8s.io/kubernetes/cmd/kube-apiserver/app/testing"
	"k8s.io/kubernetes/test/integration/framework"

	"k8s.io/kube-openapi/pkg/handler3"
)

// planDir is where the openapi-lazy-lowmem baseline artifacts (heap profiles) are written.
const openapiMemPlanDir = "/home/jpbetz/plans/openapi-lazy-lowmem"

const (
	openapiV2ProtoAccept = "application/com.github.proto-openapi.spec.v2@v1.0+protobuf"
)

// TestOpenAPIMemIdle boots a default kube-apiserver against empty etcd, warms every
// served resource (discovery + a dynamic List of each listable resource) and every
// OpenAPI surface (v2 protobuf, v3 discovery + every per-GV v3 doc), then dumps an
// in-use heap profile after idling so the resident OpenAPI footprint can be attributed.
func TestOpenAPIMemIdle(t *testing.T) {
	server, err := kubeapiservertesting.StartTestServer(t, kubeapiservertesting.NewDefaultTestServerOptions(), nil, framework.SharedEtcd())
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
	t.Logf("warmed: lists=%d v2protoBytes=%d v2etag=%s v3GVs=%d v3TotalBytes=%d", nLists, v2Size, v2Etag, nGV, v3Total)

	// Let transient rebuild garbage settle, then force GC and snapshot.
	time.Sleep(8 * time.Second)
	openapiMemSnapshot(t, "idle", openapiMemPlanDir+"/idle.heap")
}

// TestOpenAPIDeterministicEtag locks in the safety property A2 relies on: the /openapi/v2
// bytes (and per-GV v3 hash) are byte-deterministic for a fixed API surface, both across
// repeated fetches on one process and across two independently started apiservers.
func TestOpenAPIDeterministicEtag(t *testing.T) {
	serverA, err := kubeapiservertesting.StartTestServer(t, kubeapiservertesting.NewDefaultTestServerOptions(), nil, framework.SharedEtcd())
	if err != nil {
		t.Fatal(err)
	}
	defer serverA.TearDownFn()
	configA := serverA.ClientConfig

	// Same-process determinism: two fetches must yield the identical ETag.
	etagA1, _ := openapiMemFetchV2Proto(t, configA)
	etagA2, _ := openapiMemFetchV2Proto(t, configA)
	if etagA1 == "" {
		t.Fatalf("server A returned empty v2 ETag")
	}
	if etagA1 != etagA2 {
		t.Fatalf("same-process v2 ETag not stable: %q vs %q", etagA1, etagA2)
	}

	clientA, err := clientset.NewForConfig(configA)
	if err != nil {
		t.Fatal(err)
	}
	hashA := openapiMemV3GVHash(t, configA, clientA, "apis/apps/v1")

	// Cross-process determinism: a second, independent apiserver must produce the
	// same v2 ETag and the same apps/v1 v3 hash for the identical built-in surface.
	serverB, err := kubeapiservertesting.StartTestServer(t, kubeapiservertesting.NewDefaultTestServerOptions(), nil, framework.SharedEtcd())
	if err != nil {
		t.Fatal(err)
	}
	defer serverB.TearDownFn()
	configB := serverB.ClientConfig

	etagB, _ := openapiMemFetchV2Proto(t, configB)
	if etagA1 != etagB {
		t.Fatalf("cross-process v2 ETag mismatch: A=%q B=%q", etagA1, etagB)
	}
	clientB, err := clientset.NewForConfig(configB)
	if err != nil {
		t.Fatal(err)
	}
	hashB := openapiMemV3GVHash(t, configB, clientB, "apis/apps/v1")
	if hashA == "" {
		t.Fatalf("server A returned empty apps/v1 v3 hash")
	}
	if hashA != hashB {
		t.Fatalf("cross-process v3 apps/v1 hash mismatch: A=%q B=%q", hashA, hashB)
	}
	t.Logf("determinism OK: v2etag=%s v3apps/v1hash=%s", etagA1, hashA)
}

// openapiMemWarmResources lists every listable, non-subresource resource once so the
// apiserver has fully constructed its served surface. Returns the number of lists issued.
func openapiMemWarmResources(t *testing.T, client clientset.Interface, dyn dynamic.Interface) int {
	ctx := context.Background()
	_, resources, err := client.Discovery().ServerGroupsAndResources()
	if err != nil {
		// Partial discovery errors (e.g. an aggregated APIService) are non-fatal for warming.
		t.Logf("ServerGroupsAndResources partial error (continuing): %v", err)
	}
	n := 0
	for _, rl := range resources {
		gv, perr := schema.ParseGroupVersion(rl.GroupVersion)
		if perr != nil {
			continue
		}
		for _, r := range rl.APIResources {
			if strings.Contains(r.Name, "/") { // skip subresources
				continue
			}
			if !slices.Contains(r.Verbs, "list") {
				continue
			}
			gvr := gv.WithResource(r.Name)
			if _, lerr := dyn.Resource(gvr).List(ctx, metav1.ListOptions{Limit: 1}); lerr != nil {
				continue
			}
			n++
		}
	}
	return n
}

// openapiMemFetchV2Proto GETs /openapi/v2 as protobuf and returns (etag, byteCount).
func openapiMemFetchV2Proto(t *testing.T, config *restclient.Config) (string, int) {
	rt, err := restclient.TransportFor(config)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("GET", config.Host+"/openapi/v2", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", openapiV2ProtoAccept)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /openapi/v2 proto: status %d", resp.StatusCode)
	}
	return resp.Header.Get("Etag"), len(body)
}

// openapiMemWarmV3 fetches the v3 discovery doc and every per-GV v3 document (using the
// hash-bearing serverRelativeURL) so all handler3 byte caches materialize. Returns the
// number of GVs fetched and the total bytes served.
func openapiMemWarmV3(t *testing.T, config *restclient.Config, client clientset.Interface) (int, int) {
	disco := openapiMemV3Discovery(t, client)
	rt, err := restclient.TransportFor(config)
	if err != nil {
		t.Fatal(err)
	}
	n, total := 0, 0
	for _, gv := range disco.Paths {
		req, err := http.NewRequest("GET", config.Host+gv.ServerRelativeURL, nil)
		if err != nil {
			continue
		}
		resp, err := rt.RoundTrip(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		total += len(body)
		n++
	}
	return n, total
}

func openapiMemV3Discovery(t *testing.T, client clientset.Interface) handler3.OpenAPIV3Discovery {
	raw, err := client.Discovery().RESTClient().Get().AbsPath("/openapi/v3").Do(context.Background()).Raw()
	if err != nil {
		t.Fatal(err)
	}
	disco := handler3.OpenAPIV3Discovery{}
	if err := json.Unmarshal(raw, &disco); err != nil {
		t.Fatal(err)
	}
	return disco
}

// openapiMemV3GVHash returns the hash query value from the v3 discovery serverRelativeURL
// for the given "group/version" path suffix (e.g. "apps/v1").
func openapiMemV3GVHash(t *testing.T, config *restclient.Config, client clientset.Interface, gvSuffix string) string {
	disco := openapiMemV3Discovery(t, client)
	for path, gv := range disco.Paths {
		if path != gvSuffix {
			continue
		}
		// serverRelativeURL is like /openapi/v3/apis/apps/v1?hash=<hex>
		u, err := http.NewRequest("GET", "http://x"+gv.ServerRelativeURL, nil)
		if err != nil {
			return ""
		}
		// Also warm the actual byte cache for this GV.
		if rt, terr := restclient.TransportFor(config); terr == nil {
			if req, rerr := http.NewRequest("GET", config.Host+gv.ServerRelativeURL, nil); rerr == nil {
				if resp, gerr := rt.RoundTrip(req); gerr == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
			}
		}
		return u.URL.Query().Get("hash")
	}
	return ""
}

// openapiMemSnapshot forces two GCs, writes an in-use-space heap profile to heapPath, and
// logs HeapAlloc/HeapInuse/NumGoroutine under the given label.
func openapiMemSnapshot(t *testing.T, label, heapPath string) {
	runtime.GC()
	runtime.GC()
	f, err := os.Create(heapPath)
	if err != nil {
		// Fall back to a temp dir if the developer-local plan dir is absent, so
		// this investigation harness stays portable.
		heapPath = filepath.Join(t.TempDir(), filepath.Base(heapPath))
		if f, err = os.Create(heapPath); err != nil {
			t.Fatalf("create heap profile %s: %v", heapPath, err)
		}
	}
	if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
		f.Close()
		t.Fatalf("write heap profile: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close heap profile: %v", err)
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	t.Logf("[%s] HeapAlloc=%d HeapInuse=%d HeapObjects=%d NumGoroutine=%d heap=%s",
		label, ms.HeapAlloc, ms.HeapInuse, ms.HeapObjects, runtime.NumGoroutine(), heapPath)
}
