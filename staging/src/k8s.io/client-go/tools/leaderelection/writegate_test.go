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

package leaderelection

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/wait"
	clientfeatures "k8s.io/client-go/features"
	clientfeaturestesting "k8s.io/client-go/features/testing"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// These tests exercise the write gate on its own, without an election loop.

// http.NewRequest turns "" into GET, so the empty method is not listed.
var writeMethods = []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodConnect, "PROPFIND", "get"}
var readMethods = []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace}

func newTestGate() (*writeGate, *probeTransport, http.RoundTripper) {
	g := newWriteGate()
	inner := &probeTransport{}
	return g, inner, g.wrapper()(inner)
}

func doRequest(t *testing.T, rt http.RoundTripper, ctx context.Context, method string) error {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, "https://example.invalid/apis/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}

func TestWriteGateMethodClassification(t *testing.T) {
	for _, state := range []string{"closed", "open", "reclosed"} {
		for _, method := range append(append([]string{}, readMethods...), writeMethods...) {
			t.Run(state+"/"+methodName(method), func(t *testing.T) {
				g, inner, rt := newTestGate()
				switch state {
				case "open":
					g.open()
				case "reclosed":
					g.open()
					g.close()
				}
				done := make(chan error, 1)
				go func() { done <- doRequest(t, rt, context.Background(), method) }()
				// Writes block in the inner transport until released.
				time.Sleep(10 * time.Millisecond)
				inner.releaseAll()
				err := <-done

				isRead := isReadMethod(method)
				wantPass := isRead || state == "open"
				if wantPass {
					if err != nil {
						t.Fatalf("expected %s to pass with gate %s, got %v", methodName(method), state, err)
					}
					if inner.reached(method) != 1 {
						t.Fatalf("expected %s to reach the transport", methodName(method))
					}
					return
				}
				if !errors.Is(err, ErrWriteGateClosed) {
					t.Fatalf("expected ErrWriteGateClosed for %s with gate %s, got %v", methodName(method), state, err)
				}
				if inner.reached(method) != 0 {
					t.Fatalf("rejected %s must not reach the transport", methodName(method))
				}
			})
		}
	}
}

func methodName(m string) string {
	if m == "" {
		return "empty"
	}
	return m
}

func TestWriteGateStartsClosedAndReopens(t *testing.T) {
	g, _, rt := newTestGate()
	ctx := context.Background()
	if err := doRequest(t, rt, ctx, http.MethodDelete); !errors.Is(err, ErrWriteGateClosed) {
		t.Fatalf("new gate must be closed, got %v", err)
	}
	for i := 0; i < 3; i++ {
		g.open()
		g.open() // idempotent
		if !g.isOpen() {
			t.Fatal("isOpen after open = false")
		}
		if err := quickWrite(t, rt, ctx); err != nil {
			t.Fatalf("cycle %d: expected write to pass after open, got %v", i, err)
		}
		g.close()
		g.close() // idempotent
		if g.isOpen() {
			t.Fatal("isOpen after close = true")
		}
		if err := doRequest(t, rt, ctx, http.MethodPut); !errors.Is(err, ErrWriteGateClosed) {
			t.Fatalf("cycle %d: expected ErrWriteGateClosed after close, got %v", i, err)
		}
	}
}

// quickWrite sends a PUT and releases it as soon as it reaches the transport.
func quickWrite(t *testing.T, rt http.RoundTripper, ctx context.Context) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- doRequest(t, rt, ctx, http.MethodPut) }()
	// Release whatever reaches the inner transport.
	inner := rt.(utilnet.RoundTripperWrapper).WrappedRoundTripper().(*probeTransport)
	deadline := time.After(wait.ForeverTestTimeout)
	for {
		select {
		case err := <-done:
			return err
		case <-deadline:
			t.Fatal("write never completed")
		case <-time.After(time.Millisecond):
			inner.releaseAll()
		}
	}
}

func TestWriteGateCloseCancelsInFlightWritesSynchronously(t *testing.T) {
	g, inner, rt := newTestGate()
	g.open()

	const n = 5
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() { results <- doRequest(t, rt, context.Background(), http.MethodPost) }()
	}
	if err := waitFor(func() bool { return inner.reached(http.MethodPost) == n }); err != nil {
		t.Fatal("writes did not reach the transport")
	}

	g.close()

	// Synchronous: by the time close returns, every in-flight write's context is done
	// with our cause, without any scheduling of the request goroutines.
	inner.mu.Lock()
	reqs := append([]probeRequest(nil), inner.requests...)
	inner.mu.Unlock()
	for _, r := range reqs {
		select {
		case <-r.ctx.Done():
		default:
			t.Fatalf("in-flight write context not cancelled when close() returned")
		}
		if cause := context.Cause(r.ctx); !errors.Is(cause, ErrWriteGateClosed) {
			t.Fatalf("cancellation cause = %v, want ErrWriteGateClosed", cause)
		}
	}
	for i := 0; i < n; i++ {
		select {
		case err := <-results:
			if !errors.Is(err, ErrWriteGateClosed) {
				t.Fatalf("cancelled write error = %v, want to wrap ErrWriteGateClosed", err)
			}
		case <-time.After(wait.ForeverTestTimeout):
			t.Fatal("cancelled write did not return")
		}
	}
	if got := g.inflightCount(); got != 0 {
		t.Fatalf("inflight registry has %d entries after cancellation, want 0", got)
	}
}

func TestWriteGateCloseDoesNotTouchReads(t *testing.T) {
	g, inner, rt := newTestGate()
	g.open()

	// A long-running read (think: watch) started while open.
	blockingInner := &blockingReadTransport{release: make(chan struct{})}
	rt2 := g.wrapper()(blockingInner)
	done := make(chan error, 1)
	go func() { done <- doRequest(t, rt2, context.Background(), http.MethodGet) }()
	if err := waitFor(func() bool { return blockingInner.started.Load() == 1 }); err != nil {
		t.Fatal("read did not start")
	}

	g.close()
	select {
	case err := <-done:
		t.Fatalf("read returned on close: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if ctx := blockingInner.ctx.Load(); ctx != nil && (*ctx).Err() != nil {
		t.Fatal("read context was cancelled by close")
	}
	close(blockingInner.release)
	if err := <-done; err != nil {
		t.Fatalf("read failed after close: %v", err)
	}
	// Reads issued while closed also pass, and never enter the in-flight registry.
	if err := doRequest(t, rt, context.Background(), http.MethodGet); err != nil {
		t.Fatalf("read while closed: %v", err)
	}
	if inner.reached(http.MethodGet) != 1 {
		t.Fatal("read while closed did not reach the transport")
	}
	if got := g.inflightCount(); got != 0 {
		t.Fatalf("reads must not be tracked, registry has %d", got)
	}
}

type blockingReadTransport struct {
	started atomic.Int32
	ctx     atomic.Pointer[context.Context]
	release chan struct{}
}

func (b *blockingReadTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	b.ctx.Store(&ctx)
	b.started.Add(1)
	select {
	case <-b.release:
		return okResponse(req), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestWriteGateCompletedWriteIsUnaffectedByClose(t *testing.T) {
	g, inner, rt := newTestGate()
	g.open()
	if err := quickWrite(t, rt, context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := g.inflightCount(); got != 0 {
		t.Fatalf("completed write still tracked: %d", got)
	}
	last, ok := inner.last()
	if !ok {
		t.Fatal("write did not reach transport")
	}
	g.close()
	// The derived context is released (cancelled) when the write completes; a
	// later close must not be what ended it.
	if cause := context.Cause(last.ctx); errors.Is(cause, ErrWriteGateClosed) {
		t.Fatal("completed write's context was cancelled by a later close")
	}
}

func TestWriteGateResponseBodyKeepsWriteInFlightUntilClosed(t *testing.T) {
	g, inner, rt := newTestGate()
	g.open()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid/x", nil)
	respCh := make(chan *http.Response, 1)
	go func() {
		resp, err := rt.RoundTrip(req)
		if err != nil {
			t.Error(err)
		}
		respCh <- resp
	}()
	if err := waitFor(func() bool { return inner.reached(http.MethodPost) == 1 }); err != nil {
		t.Fatal(err)
	}
	inner.releaseAll()
	resp := <-respCh
	if resp == nil {
		t.Fatal("no response")
	}
	// Headers are back but the body is still open: the write is still in flight.
	if got := g.inflightCount(); got != 1 {
		t.Fatalf("write with open body should be tracked, registry has %d", got)
	}
	last, _ := inner.last()
	g.close()
	if last.ctx.Err() == nil {
		t.Fatal("close must cancel a write whose body is still open")
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("Body.Close: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("second Body.Close: %v", err)
	}
	if got := g.inflightCount(); got != 0 {
		t.Fatalf("registry has %d entries after Body.Close, want 0", got)
	}
}

func TestWriteGateCloseOnlyAffectsCurrentEpoch(t *testing.T) {
	g, inner, rt := newTestGate()
	g.open()
	g.close()
	g.open()
	done := make(chan error, 1)
	go func() { done <- doRequest(t, rt, context.Background(), http.MethodPatch) }()
	if err := waitFor(func() bool { return inner.reached(http.MethodPatch) == 1 }); err != nil {
		t.Fatal(err)
	}
	// Still in flight: a previous close must not have affected it.
	select {
	case err := <-done:
		t.Fatalf("write from second open period completed early: %v", err)
	default:
	}
	g.close()
	if err := <-done; !errors.Is(err, ErrWriteGateClosed) {
		t.Fatalf("got %v, want ErrWriteGateClosed", err)
	}
}

func TestWriteGateWrapperPlumbing(t *testing.T) {
	g := newWriteGate()
	inner := &cancelRecordingTransport{}
	rt := g.wrapper()(inner)
	w, ok := rt.(utilnet.RoundTripperWrapper)
	if !ok {
		t.Fatal("gated round tripper must implement utilnet.RoundTripperWrapper")
	}
	if w.WrappedRoundTripper() != inner {
		t.Fatal("WrappedRoundTripper must return the inner transport")
	}
	c, ok := rt.(interface{ CancelRequest(*http.Request) })
	if !ok {
		t.Fatal("gated round tripper must implement CancelRequest")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://example.invalid/x", nil)
	c.CancelRequest(req)
	if inner.cancelled != 1 {
		t.Fatal("CancelRequest not forwarded")
	}
}

type cancelRecordingTransport struct{ cancelled int }

func (c *cancelRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return okResponse(req), nil
}
func (c *cancelRecordingTransport) CancelRequest(*http.Request) { c.cancelled++ }

func TestWriteGateDoesNotMutateCallerRequest(t *testing.T) {
	g, inner, rt := newTestGate()
	g.open()
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.invalid/x", nil)
	go inner.releaseAllEventually()
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if req.Context() != ctx {
		t.Fatal("caller's request context was replaced")
	}
}

func (p *probeTransport) releaseAllEventually() {
	for i := 0; i < 1000; i++ {
		time.Sleep(time.Millisecond)
		p.releaseAll()
	}
}

// TestWriteGateElectorFeatureGate checks WriteGate() on a LeaderElector with the
// feature gate on and off.
func TestWriteGateElectorFeatureGate(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%v", enabled), func(t *testing.T) {
			clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.LeaderElectionRecovery, enabled)
			le, err := NewLeaderElector(LeaderElectionConfig{
				Lock:          newSimLock("a", newSimLeaseServer(), 0),
				LeaseDuration: 15 * time.Second,
				RenewDeadline: 10 * time.Second,
				RetryPeriod:   2 * time.Second,
				Callbacks:     LeaderCallbacks{OnStartedLeading: func(context.Context) {}, OnStoppedLeading: func() {}},
			})
			if err != nil {
				t.Fatal(err)
			}
			inner := &probeTransport{}
			rt := le.WriteGate()(inner)
			go inner.releaseAllEventually()
			err = doRequest(t, rt, context.Background(), http.MethodPost)
			if enabled {
				// Never led: gate closed.
				if !errors.Is(err, ErrWriteGateClosed) {
					t.Fatalf("feature enabled: expected ErrWriteGateClosed before leading, got %v", err)
				}
				if inner.reached(http.MethodPost) != 0 {
					t.Fatal("rejected write reached transport")
				}
			} else {
				if err != nil {
					t.Fatalf("feature disabled: WriteGate must pass everything through, got %v", err)
				}
				if rt != http.RoundTripper(inner) {
					t.Fatal("feature disabled: WriteGate must return the inner transport unchanged")
				}
			}
		})
	}
}

// TestWriteGateThroughRESTClient sends real requests through a real
// rest.Config/http.Transport to an httptest server.
func TestWriteGateThroughRESTClient(t *testing.T) {
	var serverHits atomic.Int32
	holdWrites := make(chan struct{})
	var holdOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverHits.Add(1)
		if r.Method != http.MethodGet {
			// Hold writes until told to proceed or the client goes away.
			select {
			case <-holdWrites:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		cm := corev1.ConfigMap{TypeMeta: metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"}, ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "ns"}}
		_ = json.NewEncoder(w).Encode(cm)
	}))
	defer server.Close()

	g := newWriteGate()
	cfg := &rest.Config{Host: server.URL}
	cfg.Wrap(g.wrapper())
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "ns"}}

	// Closed: write rejected client-side, immediately, no retries, errors.Is works.
	start := time.Now()
	_, err = client.CoreV1().ConfigMaps("ns").Create(ctx, cm, metav1.CreateOptions{})
	if !errors.Is(err, ErrWriteGateClosed) {
		t.Fatalf("Create while closed: err = %v, want to wrap ErrWriteGateClosed", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("rejected write took %v; the rest client must not retry or back off", elapsed)
	}
	if serverHits.Load() != 0 {
		t.Fatalf("server saw %d requests for a rejected write", serverHits.Load())
	}
	_, err = client.CoreV1().ConfigMaps("ns").Patch(ctx, "x", "application/merge-patch+json", []byte(`{}`), metav1.PatchOptions{})
	if !errors.Is(err, ErrWriteGateClosed) {
		t.Fatalf("Patch while closed: err = %v", err)
	}
	if err := client.CoreV1().ConfigMaps("ns").Delete(ctx, "x", metav1.DeleteOptions{}); !errors.Is(err, ErrWriteGateClosed) {
		t.Fatalf("Delete while closed: err = %v", err)
	}
	// Reads pass while closed.
	if _, err := client.CoreV1().ConfigMaps("ns").Get(ctx, "x", metav1.GetOptions{}); err != nil {
		t.Fatalf("Get while closed: %v", err)
	}
	if serverHits.Load() != 1 {
		t.Fatalf("server hits = %d, want 1 (the GET)", serverHits.Load())
	}

	// Open: the write reaches the server.
	g.open()
	holdOnce.Do(func() { close(holdWrites) })
	if _, err := client.CoreV1().ConfigMaps("ns").Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Create while open: %v", err)
	}
	if serverHits.Load() != 2 {
		t.Fatalf("server hits = %d, want 2", serverHits.Load())
	}
}

// TestWriteGateThroughRESTClientInFlightCancel holds a real write on the server
// and closes the gate under it.
func TestWriteGateThroughRESTClientInFlightCancel(t *testing.T) {
	arrived := make(chan struct{}, 1)
	serverSawCancel := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The server only notices a client disconnect once the request body has
		// been consumed.
		_, _ = io.Copy(io.Discard, r.Body)
		arrived <- struct{}{}
		<-r.Context().Done()
		serverSawCancel <- struct{}{}
	}))
	defer server.Close()

	g := newWriteGate()
	g.open()
	cfg := &rest.Config{Host: server.URL}
	cfg.Wrap(g.wrapper())
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := client.CoreV1().ConfigMaps("ns").Create(context.Background(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "x"}}, metav1.CreateOptions{})
		result <- err
	}()
	select {
	case <-arrived:
	case <-time.After(wait.ForeverTestTimeout):
		t.Fatal("write never arrived at server")
	}
	g.close()
	select {
	case err := <-result:
		if !errors.Is(err, ErrWriteGateClosed) {
			t.Fatalf("in-flight write error = %v, want to wrap ErrWriteGateClosed", err)
		}
	case <-time.After(wait.ForeverTestTimeout):
		t.Fatal("in-flight write did not return after close")
	}
	select {
	case <-serverSawCancel:
	case <-time.After(wait.ForeverTestTimeout):
		t.Fatal("server did not observe the cancelled request")
	}
}

// TestWriteGateConcurrentToggle hammers the gate from many goroutines while it
// flips. Run with -race. Every write outcome must be success or a gate error.
func TestWriteGateConcurrentToggle(t *testing.T) {
	g := newWriteGate()
	inner := &instantTransport{}
	rt := g.wrapper()(inner)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	var passed, rejected atomic.Int64
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				err := doRequest(t, rt, ctx, http.MethodPost)
				switch {
				case err == nil:
					passed.Add(1)
				case errors.Is(err, ErrWriteGateClosed):
					rejected.Add(1)
				case errors.Is(err, context.Canceled):
				default:
					t.Errorf("unexpected write error: %v", err)
					return
				}
				if err := doRequest(t, rt, ctx, http.MethodGet); err != nil && !errors.Is(err, context.Canceled) {
					t.Errorf("read failed: %v", err)
					return
				}
			}
		}()
	}
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		g.open()
		g.close()
	}
	cancel()
	wg.Wait()
	if passed.Load() == 0 || rejected.Load() == 0 {
		t.Fatalf("toggle test did not exercise both outcomes: passed=%d rejected=%d", passed.Load(), rejected.Load())
	}
	if got := g.inflightCount(); got != 0 {
		t.Fatalf("registry leaked %d entries", got)
	}
}

type instantTransport struct{}

func (instantTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: req}, nil
}

func waitFor(cond func() bool) error {
	return wait.PollUntilContextTimeout(context.Background(), time.Millisecond, wait.ForeverTestTimeout, true, func(context.Context) (bool, error) { return cond(), nil })
}

// newTLSTestServer starts an httptest server, HTTP/2 when http2 is set, and
// returns a rest.Config that trusts it.
func newTLSTestServer(t *testing.T, http2 bool, handler http.Handler) (*httptest.Server, *rest.Config) {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	server.EnableHTTP2 = http2
	server.StartTLS()
	t.Cleanup(server.Close)
	cfg := &rest.Config{Host: server.URL}
	cfg.TLSClientConfig.CAData = pemCert(t, server.Certificate().Raw)
	if !http2 {
		// Force HTTP/1.1: client-go enables HTTP/2 for TLS transports by default.
		cfg.NextProtos = []string{"http/1.1"}
	}
	return server, cfg
}

func pemCert(t *testing.T, der []byte) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// TestWriteGateCloseDuringBodyRead closes the gate after the response headers
// arrived but before the body is complete, over HTTP/1.1 and HTTP/2. HTTP/2
// transports return a bare context error for such reads; the gate must still
// surface ErrWriteGateClosed.
func TestWriteGateCloseDuringBodyRead(t *testing.T) {
	for _, http2 := range []bool{false, true} {
		t.Run(fmt.Sprintf("http2=%v", http2), func(t *testing.T) {
			headersSent := make(chan struct{}, 1)
			// The handler holds the stream open until the test releases it, so
			// the only way the client's body read can end is the local
			// cancellation by the gate (a server-side end of stream would race
			// it and produce a clean EOF instead).
			releaseHandler := make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(releaseHandler) }) }
			var sawProto atomic.Value
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sawProto.Store(r.Proto)
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"kind":"ConfigMap","apiVersion":"v1",`))
				w.(http.Flusher).Flush()
				headersSent <- struct{}{}
				<-releaseHandler
			})
			_, cfg := newTLSTestServer(t, http2, handler)
			t.Cleanup(release)
			g := newWriteGate()
			g.open()
			cfg.Wrap(g.wrapper())
			client, err := kubernetes.NewForConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				_, err := client.CoreV1().ConfigMaps("ns").Create(context.Background(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "x"}}, metav1.CreateOptions{})
				result <- err
			}()
			select {
			case <-headersSent:
			case <-time.After(wait.ForeverTestTimeout):
				t.Fatal("server never sent headers")
			}
			g.close()
			select {
			case err := <-result:
				if !errors.Is(err, ErrWriteGateClosed) {
					t.Fatalf("Create interrupted during body read: err = %v (%T), want to wrap ErrWriteGateClosed", err, err)
				}
			case <-time.After(wait.ForeverTestTimeout):
				t.Fatal("Create did not return after close")
			}
			release()
			wantProto := "HTTP/1.1"
			if http2 {
				wantProto = "HTTP/2.0"
			}
			if got := sawProto.Load(); got != wantProto {
				t.Fatalf("server saw %v, want %s: the test did not exercise the intended protocol", got, wantProto)
			}
			if got := g.inflightCount(); got != 0 {
				t.Fatalf("registry leaked %d entries", got)
			}
		})
	}
}

// TestWriteGateInFlightCancelHTTP2 is TestWriteGateThroughRESTClientInFlightCancel
// over HTTP/2, which is what apiserver clients negotiate by default.
func TestWriteGateInFlightCancelHTTP2(t *testing.T) {
	arrived := make(chan struct{}, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		arrived <- struct{}{}
		<-r.Context().Done()
	})
	_, cfg := newTLSTestServer(t, true, handler)
	g := newWriteGate()
	g.open()
	cfg.Wrap(g.wrapper())
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := client.CoreV1().ConfigMaps("ns").Create(context.Background(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "x"}}, metav1.CreateOptions{})
		result <- err
	}()
	select {
	case <-arrived:
	case <-time.After(wait.ForeverTestTimeout):
		t.Fatal("write never arrived")
	}
	g.close()
	select {
	case err := <-result:
		if !errors.Is(err, ErrWriteGateClosed) {
			t.Fatalf("err = %v, want to wrap ErrWriteGateClosed", err)
		}
	case <-time.After(wait.ForeverTestTimeout):
		t.Fatal("write did not return after close")
	}
}

// TestWriteGateRejectionIsNotRetried checks, for every write verb the typed
// client uses, that a rejected write makes exactly one attempt, returns at
// once, and that the surfaced error matches none of the rest client's
// retryable-error predicates (so a future broadening of the retry policy to
// idempotent verbs would be caught here).
func TestWriteGateRejectionIsNotRetried(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(corev1.ConfigMap{TypeMeta: metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"}})
	}))
	defer server.Close()
	g := newWriteGate()
	attempts := &countingTransport{}
	cfg := &rest.Config{Host: server.URL}
	// Wrappers apply in order, so the counter ends up outermost and sees
	// every attempt the rest client makes, including rejected ones.
	cfg.Wrap(g.wrapper())
	cfg.Wrap(func(rt http.RoundTripper) http.RoundTripper { attempts.rt = rt; return attempts })
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "ns"}}
	calls := map[string]func() error{
		"create": func() error {
			_, err := client.CoreV1().ConfigMaps("ns").Create(ctx, cm, metav1.CreateOptions{})
			return err
		},
		"update": func() error {
			_, err := client.CoreV1().ConfigMaps("ns").Update(ctx, cm, metav1.UpdateOptions{})
			return err
		},
		"patch": func() error {
			_, err := client.CoreV1().ConfigMaps("ns").Patch(ctx, "x", "application/merge-patch+json", []byte(`{}`), metav1.PatchOptions{})
			return err
		},
		"delete": func() error { return client.CoreV1().ConfigMaps("ns").Delete(ctx, "x", metav1.DeleteOptions{}) },
		"deletecollection": func() error {
			return client.CoreV1().ConfigMaps("ns").DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
		},
		"raw delete without body": func() error {
			return client.CoreV1().RESTClient().Delete().Namespace("ns").Resource("configmaps").Name("x").Do(ctx).Error()
		},
		"raw post without body": func() error {
			return client.CoreV1().RESTClient().Post().Namespace("ns").Resource("configmaps").Do(ctx).Error()
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			attempts.n.Store(0)
			before := hits.Load()
			start := time.Now()
			err := call()
			if !errors.Is(err, ErrWriteGateClosed) {
				t.Fatalf("err = %v, want to wrap ErrWriteGateClosed", err)
			}
			if d := time.Since(start); d > time.Second {
				t.Fatalf("took %v: the client must not sleep or back off", d)
			}
			if attempts.n.Load() != 1 {
				t.Fatalf("transport attempts = %d, want 1", attempts.n.Load())
			}
			if hits.Load() != before {
				t.Fatal("rejected write reached the server")
			}
			if utilnet.IsProbableEOF(err) || utilnet.IsConnectionReset(err) || utilnet.IsHTTP2ConnectionLost(err) || utilnet.IsTimeout(err) {
				t.Fatalf("gate error must not look retryable: %v", err)
			}
		})
	}
}

// countingTransport sits under the gate and counts attempts that pass it.
type countingTransport struct {
	rt http.RoundTripper
	n  atomic.Int32
}

func (c *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.n.Add(1)
	return c.rt.RoundTrip(req)
}

// bareCancelTransport returns a body whose Read blocks until the request
// context is done and then fails with the bare context error, as HTTP/2
// transports do (they do not return context.Cause).
type bareCancelTransport struct{}

func (bareCancelTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Request:    req,
		Body:       &blockingBody{ctx: req.Context()},
	}, nil
}

type blockingBody struct{ ctx context.Context }

func (b *blockingBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (b *blockingBody) Close() error { return nil }

// TestWriteGateBodyReadErrorTranslated pins the translation of body-read
// failures caused by the gate into ErrWriteGateClosed, independently of what
// the real HTTP transports return.
func TestWriteGateBodyReadErrorTranslated(t *testing.T) {
	g := newWriteGate()
	g.open()
	rt := g.wrapper()(bareCancelTransport{})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid/x", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	readErr := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(resp.Body)
		readErr <- err
	}()
	time.Sleep(10 * time.Millisecond)
	g.close()
	select {
	case err := <-readErr:
		// The gate error is the distinct signal; the transport's error is kept
		// in the message but deliberately not wrapped, so generic
		// errors.Is(err, context.Canceled) shutdown checks do not swallow it.
		if !errors.Is(err, ErrWriteGateClosed) || errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), context.Canceled.Error()) {
			t.Fatalf("body read error = %v; want ErrWriteGateClosed mentioning the transport error", err)
		}
	case <-time.After(wait.ForeverTestTimeout):
		t.Fatal("body read did not fail after close")
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	// A read that fails for another reason while the gate is open is passed through.
	g.open()
	ctx, cancel := context.WithCancel(context.Background())
	req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.invalid/x", nil)
	resp2, err := rt.RoundTrip(req2)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	_, err = io.ReadAll(resp2.Body)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrWriteGateClosed) {
		t.Fatalf("caller cancellation must not be reported as a gate error, got %v", err)
	}
	resp2.Body.Close()
	if got := g.inflightCount(); got != 0 {
		t.Fatalf("registry has %d entries, want 0", got)
	}
}

// errorTransport fails every request.
type errorTransport struct{ err error }

func (e errorTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, e.err }

// TestWriteGateFailedWriteIsReleased checks that a write which fails in the
// transport for an unrelated reason is dropped from the in-flight registry
// and its derived context released, while the gate stays open.
func TestWriteGateFailedWriteIsReleased(t *testing.T) {
	g := newWriteGate()
	g.open()
	boom := errors.New("connection refused")
	rt := g.wrapper()(errorTransport{err: boom})
	for i := 0; i < 3; i++ {
		err := doRequest(t, rt, context.Background(), http.MethodPost)
		if !errors.Is(err, boom) || errors.Is(err, ErrWriteGateClosed) {
			t.Fatalf("err = %v, want the transport error unchanged", err)
		}
	}
	if got := g.inflightCount(); got != 0 {
		t.Fatalf("registry has %d entries after failed writes, want 0", got)
	}
	if !g.isOpen() {
		t.Fatal("gate must stay open")
	}
}

// nilBodyTransport returns responses without a body, as some custom
// RoundTrippers do.
type nilBodyTransport struct{}

func (nilBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Request: req}, nil
}

func TestWriteGateNilResponseBody(t *testing.T) {
	g := newWriteGate()
	g.open()
	rt := g.wrapper()(nilBodyTransport{})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid/x", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Body != nil {
		t.Fatal("body must stay nil")
	}
	if got := g.inflightCount(); got != 0 {
		t.Fatalf("a bodiless response leaves nothing in flight, registry has %d", got)
	}
}

// TestWriteGateForeignErrorsPassThrough: failures that are not the gate's
// doing are returned unchanged while the gate is open, and release the
// registry entry.
func TestWriteGateForeignErrorsPassThrough(t *testing.T) {
	t.Run("caller cancels an in-flight write", func(t *testing.T) {
		g, inner, rt := newTestGate()
		g.open()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- doRequest(t, rt, ctx, http.MethodPost) }()
		if err := waitFor(func() bool { return inner.reached(http.MethodPost) == 1 }); err != nil {
			t.Fatal(err)
		}
		cancel()
		err := <-done
		if !errors.Is(err, context.Canceled) || errors.Is(err, ErrWriteGateClosed) {
			t.Fatalf("err = %v, want the caller's cancellation unchanged", err)
		}
		if got := g.inflightCount(); got != 0 {
			t.Fatalf("registry has %d entries, want 0", got)
		}
		if !g.isOpen() {
			t.Fatal("gate must stay open")
		}
	})
	t.Run("caller deadline", func(t *testing.T) {
		g, _, rt := newTestGate()
		g.open()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		err := doRequest(t, rt, ctx, http.MethodPut)
		if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrWriteGateClosed) {
			t.Fatalf("err = %v, want the caller's deadline unchanged", err)
		}
		if got := g.inflightCount(); got != 0 {
			t.Fatalf("registry has %d entries, want 0", got)
		}
	})
}
