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
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"testing"

	restclient "k8s.io/client-go/rest"
	kubeapiservertesting "k8s.io/kubernetes/cmd/kube-apiserver/app/testing"
	"k8s.io/kubernetes/test/integration/framework"
)

func fetchV2JSON(t *testing.T, config *restclient.Config) []byte {
	rt, err := restclient.TransportFor(config)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", config.Host+"/openapi/v2", nil)
	req.Header.Set("Accept", "application/json")
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /openapi/v2 json: status %d", resp.StatusCode)
	}
	return b
}

func startServer(t *testing.T, lazy bool) kubeapiservertesting.TestServer {
	var flags []string
	if lazy {
		flags = []string{"--feature-gates=OpenAPILazyGraph=true"}
	}
	s, err := kubeapiservertesting.StartTestServer(t, kubeapiservertesting.NewDefaultTestServerOptions(), flags, framework.SharedEtcd())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestOpenAPILazyConsistency checks that the OpenAPILazyGraph (weak) path serves
// byte-identical /openapi/v2 to the legacy path, and that the weak path is
// deterministic across two independent apiservers (A2's safety property).
func TestOpenAPILazyConsistency(t *testing.T) {
	off := startServer(t, false)
	defer off.TearDownFn()
	on1 := startServer(t, true)
	defer on1.TearDownFn()
	on2 := startServer(t, true)
	defer on2.TearDownFn()

	bOff := fetchV2JSON(t, off.ClientConfig)
	bOn1a := fetchV2JSON(t, on1.ClientConfig)
	bOn1b := fetchV2JSON(t, on1.ClientConfig) // same process, second fetch
	bOn2 := fetchV2JSON(t, on2.ClientConfig)  // independent process

	sum := func(b []byte) string { s := sha256.Sum256(b); return fmt.Sprintf("%x", s[:8]) }
	t.Logf("v2 json bytes: off=%d(%s) on1a=%d(%s) on1b=%d(%s) on2=%d(%s)",
		len(bOff), sum(bOff), len(bOn1a), sum(bOn1a), len(bOn1b), sum(bOn1b), len(bOn2), sum(bOn2))

	// A2 safety property: the weak path must be deterministic.
	if !bytes.Equal(bOn1a, bOn1b) {
		t.Errorf("weak path NOT deterministic same-process (bytes differ between two fetches)")
	}
	if !bytes.Equal(bOn1a, bOn2) {
		t.Errorf("weak path NOT deterministic cross-process")
	}

	// Wire-compatibility: the gate should not change the served bytes.
	if !bytes.Equal(bOff, bOn1a) {
		d := firstDiff(bOff, bOn1a)
		t.Errorf("gate changes /openapi/v2 bytes: len off=%d on=%d first diff at %d\n  off: %q\n   on: %q",
			len(bOff), len(bOn1a), d, window(bOff, d), window(bOn1a, d))
	}

	// Wire-compatibility: the v2 protobuf etag must match legacy too (proto reuses
	// the JSON etag on both paths).
	etagOff, _ := openapiMemFetchV2Proto(t, off.ClientConfig)
	etagOn, _ := openapiMemFetchV2Proto(t, on1.ClientConfig)
	t.Logf("v2 proto etag: off=%s on=%s", etagOff, etagOn)
	if etagOff != etagOn {
		t.Errorf("gate changes /openapi/v2 proto etag: off=%s on=%s", etagOff, etagOn)
	}
}

func firstDiff(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func window(b []byte, off int) string {
	lo := off - 40
	if lo < 0 {
		lo = 0
	}
	hi := off + 40
	if hi > len(b) {
		hi = len(b)
	}
	return string(b[lo:hi])
}
