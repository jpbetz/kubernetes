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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	clientfeatures "k8s.io/client-go/features"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/component-base/featuregate"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	kubeapiservertesting "k8s.io/kubernetes/cmd/kube-apiserver/app/testing"
	"k8s.io/kubernetes/test/integration/framework"
)

// These tests exercise leader election recovery mode (KEP-6361) against a
// real kube-apiserver. The election client of the candidate under test goes
// through a reverse proxy that can be taken down, which makes the apiserver
// unreachable for that client only; gated controller clients and standby
// candidates talk to the apiserver directly.

const (
	leaseDuration = 6 * time.Second
	renewDeadline = 4 * time.Second
	retryPeriod   = 1 * time.Second

	pollInterval = 100 * time.Millisecond
)

// outageProxy is a reverse proxy in front of the apiserver that can be taken
// down. While down it behaves like an unreachable apiserver: requests hang
// until the client gives up on them.
type outageProxy struct {
	server   *httptest.Server
	upstream *httputil.ReverseProxy
	down     atomic.Bool
	stopCh   chan struct{}
	stopOnce sync.Once
}

func newOutageProxy(t *testing.T, backend *rest.Config) *outageProxy {
	t.Helper()
	target, err := url.Parse(backend.Host)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httputil.NewSingleHostReverseProxy(target)
	upstream.FlushInterval = -1
	// The upstream transport carries the credentials, so clients of the
	// proxy need none of their own.
	transport, err := rest.TransportFor(backend)
	if err != nil {
		t.Fatal(err)
	}
	upstream.Transport = transport
	p := &outageProxy{upstream: upstream, stopCh: make(chan struct{})}
	p.server = httptest.NewServer(http.HandlerFunc(p.serveHTTP))
	t.Cleanup(p.Close)
	return p
}

func (p *outageProxy) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if !p.down.Load() {
		p.upstream.ServeHTTP(w, r)
		return
	}
	// net/http only notices the client hanging up once the request body has
	// been consumed, so drain it before waiting for the client to give up.
	_, _ = io.Copy(io.Discard, r.Body)
	select {
	case <-r.Context().Done():
	case <-p.stopCh:
	}
}

// SetDown starts or ends an outage. Idle upstream connections are dropped at
// the start of an outage so nothing is left half-open across it.
func (p *outageProxy) SetDown(down bool) {
	p.down.Store(down)
	if down {
		utilnet.CloseIdleConnectionsFor(p.upstream.Transport)
	}
}

func (p *outageProxy) Close() {
	p.stopOnce.Do(func() { close(p.stopCh) })
	p.server.Close()
}

// ClientConfig returns a config for clients that reach the apiserver through
// the proxy.
func (p *outageProxy) ClientConfig() *rest.Config {
	return &rest.Config{Host: p.server.URL}
}

// candidate is one LeaderElector together with the observations the tests
// assert on: callback counts, the OnStartedLeading context, observed leaders,
// and whether Run has returned.
type candidate struct {
	t        *testing.T
	identity string
	elector  *leaderelection.LeaderElector
	gated    kubernetes.Interface

	mu         sync.Mutex
	started    int
	stopped    int
	leaders    []string
	startedCtx context.Context

	startedCh chan struct{}
	runDone   chan struct{}
}

// newCandidate builds an elector whose lock client goes through electionCfg
// and whose gated controller client goes through gatedCfg.
func newCandidate(t *testing.T, identity, leaseName string, electionCfg, gatedCfg *rest.Config, recovery *leaderelection.RecoveryConfig) *candidate {
	t.Helper()
	c := &candidate{
		t:         t,
		identity:  identity,
		startedCh: make(chan struct{}),
		runDone:   make(chan struct{}),
	}

	gate := leaderelection.NewWriteGate()
	cfg := rest.CopyConfig(gatedCfg)
	cfg.Wrap(gate.Wrapper())
	c.gated = kubernetes.NewForConfigOrDie(cfg)

	lock, err := resourcelock.NewFromKubeconfig(resourcelock.LeasesResourceLock, metav1.NamespaceSystem, leaseName,
		resourcelock.ResourceLockConfig{Identity: identity}, electionCfg, renewDeadline)
	if err != nil {
		t.Fatal(err)
	}

	c.elector, err = leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:          lock,
		LeaseDuration: leaseDuration,
		RenewDeadline: renewDeadline,
		RetryPeriod:   retryPeriod,
		Name:          leaseName,
		Recovery:      recovery,
		WriteGate:     gate,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				c.mu.Lock()
				defer c.mu.Unlock()
				c.started++
				t.Logf("%s: OnStartedLeading (#%d)", identity, c.started)
				if c.started == 1 {
					c.startedCtx = ctx
					close(c.startedCh)
				}
			},
			OnStoppedLeading: func() {
				c.mu.Lock()
				defer c.mu.Unlock()
				c.stopped++
				t.Logf("%s: OnStoppedLeading (#%d)", identity, c.stopped)
			},
			OnNewLeader: func(leader string) {
				c.mu.Lock()
				defer c.mu.Unlock()
				c.leaders = append(c.leaders, leader)
				t.Logf("%s: OnNewLeader(%q)", identity, leader)
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (c *candidate) run(ctx context.Context) {
	go func() {
		defer close(c.runDone)
		c.elector.Run(ctx)
		c.t.Logf("%s: Run returned", c.identity)
	}()
}

func (c *candidate) counts() (started, stopped int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.started, c.stopped
}

func (c *candidate) observedLeaders() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.leaders...)
}

func (c *candidate) runReturned() bool {
	select {
	case <-c.runDone:
		return true
	default:
		return false
	}
}

func (c *candidate) waitStarted(timeout time.Duration) {
	c.t.Helper()
	select {
	case <-c.startedCh:
	case <-time.After(timeout):
		c.t.Fatalf("%s: OnStartedLeading not called within %v", c.identity, timeout)
	}
}

func (c *candidate) waitRunReturned(timeout time.Duration) {
	c.t.Helper()
	select {
	case <-c.runDone:
	case <-time.After(timeout):
		c.t.Fatalf("%s: Run did not return within %v", c.identity, timeout)
	}
}

// waitStopped waits until OnStoppedLeading has been called exactly n times.
func (c *candidate) waitStopped(n int, timeout time.Duration) {
	c.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		_, stopped := c.counts()
		if stopped == n {
			return
		}
		if stopped > n || time.Now().After(deadline) {
			c.t.Fatalf("%s: OnStoppedLeading called %d times, want %d", c.identity, stopped, n)
		}
		time.Sleep(pollInterval)
	}
}

// requireCounts asserts the exact callback counts so far.
func (c *candidate) requireCounts(wantStarted, wantStopped int) {
	c.t.Helper()
	started, stopped := c.counts()
	if started != wantStarted || stopped != wantStopped {
		c.t.Fatalf("%s: OnStartedLeading=%d OnStoppedLeading=%d, want %d and %d", c.identity, started, stopped, wantStarted, wantStopped)
	}
}

func (c *candidate) requireRunning() {
	c.t.Helper()
	if c.runReturned() {
		c.t.Fatalf("%s: Run returned unexpectedly", c.identity)
	}
}

// requireStartedCtx asserts whether the OnStartedLeading context is done.
func (c *candidate) requireStartedCtx(wantDone bool) {
	c.t.Helper()
	c.mu.Lock()
	ctx := c.startedCtx
	c.mu.Unlock()
	if ctx == nil {
		c.t.Fatalf("%s: OnStartedLeading has not been called", c.identity)
	}
	select {
	case <-ctx.Done():
		if !wantDone {
			c.t.Fatalf("%s: OnStartedLeading context is cancelled: %v", c.identity, context.Cause(ctx))
		}
	default:
		if wantDone {
			c.t.Fatalf("%s: OnStartedLeading context is still live", c.identity)
		}
	}
}

// gatedWrite makes one write through the gated client.
func (c *candidate) gatedWrite(ctx context.Context) error {
	_, err := c.gated.CoreV1().ConfigMaps(metav1.NamespaceDefault).Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "gate-probe-" + c.identity + "-"},
	}, metav1.CreateOptions{})
	return err
}

func (c *candidate) requireGateClosed(ctx context.Context) {
	c.t.Helper()
	if err := c.gatedWrite(ctx); !errors.Is(err, leaderelection.ErrWriteGateClosed) {
		c.t.Fatalf("%s: gated write with gate closed: got err %v, want ErrWriteGateClosed", c.identity, err)
	}
}

func (c *candidate) requireGateOpen(ctx context.Context) {
	c.t.Helper()
	if err := c.gatedWrite(ctx); err != nil {
		c.t.Fatalf("%s: gated write with gate open: %v", c.identity, err)
	}
}

// waitGate polls gated writes until the gate is observed in the wanted state
// and returns when that happened.
func (c *candidate) waitGate(ctx context.Context, wantClosed bool, timeout time.Duration) time.Time {
	c.t.Helper()
	var observed time.Time
	err := wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		err := c.gatedWrite(ctx)
		closed := errors.Is(err, leaderelection.ErrWriteGateClosed)
		if err != nil && !closed {
			c.t.Logf("%s: gated write failed with a non-gate error: %v", c.identity, err)
			return false, nil
		}
		if closed == wantClosed {
			observed = time.Now()
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		c.t.Fatalf("%s: gate not observed closed=%v within %v: %v", c.identity, wantClosed, timeout, err)
	}
	return observed
}

func startServer(t *testing.T) *kubeapiservertesting.TestServer {
	t.Helper()
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, featuregate.Feature(clientfeatures.LeaderElectionRecovery), true)
	server := kubeapiservertesting.StartTestServerOrDie(t, kubeapiservertesting.NewDefaultTestServerOptions(), framework.DefaultTestServerFlags(), framework.SharedEtcd())
	t.Cleanup(server.TearDownFn)
	return server
}

func leaseHolder(ctx context.Context, t *testing.T, client kubernetes.Interface, name string) string {
	t.Helper()
	lease, err := client.CoordinationV1().Leases(metav1.NamespaceSystem).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lease.Spec.HolderIdentity == nil {
		return ""
	}
	return *lease.Spec.HolderIdentity
}

// TestLeaderElectionRecoveryGateFollowsLeadership checks that the write gate
// tracks leadership through an apiserver outage that is shorter than the
// recovery deadline: it closes when the lease is lost and reopens when the
// lease is renewed again, while Run keeps going.
func TestLeaderElectionRecoveryGateFollowsLeadership(t *testing.T) {
	server := startServer(t)
	proxy := newOutageProxy(t, server.ClientConfig)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	a := newCandidate(t, "a", "recovery-gate", proxy.ClientConfig(), server.ClientConfig,
		&leaderelection.RecoveryConfig{RecoveryDeadline: 60 * time.Second})

	// (a) Before Run: writes are rejected, reads pass.
	a.requireGateClosed(ctx)
	if _, err := a.gated.CoreV1().Namespaces().Get(ctx, metav1.NamespaceDefault, metav1.GetOptions{}); err != nil {
		t.Fatalf("gated read with gate closed: %v", err)
	}

	// (b) Leading: writes pass.
	a.run(ctx)
	a.waitStarted(30 * time.Second)
	a.requireGateOpen(ctx)

	// (c) Outage: the gate closes and OnStoppedLeading fires, but Run keeps
	// going and the OnStartedLeading context stays live.
	downAt := time.Now()
	proxy.SetDown(true)
	t.Logf("proxy down")
	closedAt := a.waitGate(ctx, true, 20*time.Second)
	t.Logf("gate closed %v after the outage began", closedAt.Sub(downAt).Round(time.Millisecond))
	a.waitStopped(1, 5*time.Second)
	a.requireCounts(1, 1)
	a.requireRunning()
	a.requireStartedCtx(false)

	// (d) Recovery: the gate reopens without a second OnStartedLeading.
	upAt := time.Now()
	proxy.SetDown(false)
	t.Logf("proxy up")
	openedAt := a.waitGate(ctx, false, 20*time.Second)
	t.Logf("gate reopened %v after the outage ended", openedAt.Sub(upAt).Round(time.Millisecond))
	a.requireCounts(1, 1)
	a.requireRunning()
	a.requireStartedCtx(false)

	// (e) Cancel: Run returns and OnStoppedLeading fires once more.
	cancel()
	a.waitRunReturned(30 * time.Second)
	a.requireCounts(1, 2)
	a.requireStartedCtx(true)
	if got := leaseHolder(context.Background(), t, kubernetes.NewForConfigOrDie(server.ClientConfig), "recovery-gate"); got != "a" {
		t.Errorf("lease holder = %q, want %q", got, "a")
	}
}

// TestLeaderElectionRecoveryDeadline checks that Run returns once the
// recovery deadline, measured from the loss of the lease, passes during an
// outage, without calling OnStoppedLeading a second time.
func TestLeaderElectionRecoveryDeadline(t *testing.T) {
	const recoveryDeadline = 10 * time.Second

	server := startServer(t)
	proxy := newOutageProxy(t, server.ClientConfig)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	a := newCandidate(t, "a", "recovery-deadline", proxy.ClientConfig(), server.ClientConfig,
		&leaderelection.RecoveryConfig{RecoveryDeadline: recoveryDeadline})

	a.run(ctx)
	a.waitStarted(30 * time.Second)
	a.requireGateOpen(ctx)

	downAt := time.Now()
	proxy.SetDown(true)
	t.Logf("proxy down")
	lossTime := a.waitGate(ctx, true, 20*time.Second)
	t.Logf("gate closed %v after the outage began", lossTime.Sub(downAt).Round(time.Millisecond))
	a.waitStopped(1, 5*time.Second)
	a.requireRunning()

	a.waitRunReturned(recoveryDeadline + 10*time.Second)
	sinceLoss := time.Since(lossTime)
	t.Logf("Run returned %v after the gate closed", sinceLoss.Round(time.Millisecond))
	// lossTime is when the closed gate was first observed, which is slightly
	// after the loss itself, so allow Run to return a little before
	// lossTime+recoveryDeadline.
	if sinceLoss < recoveryDeadline-2*time.Second {
		t.Errorf("Run returned %v after the loss, before the %v recovery deadline", sinceLoss.Round(time.Millisecond), recoveryDeadline)
	}
	a.requireCounts(1, 1)
	a.requireStartedCtx(true)
	a.requireGateClosed(ctx)
}

// TestLeaderElectionRecoveryStandbyTakesOver checks that a recovering holder
// gives up as soon as it observes that a standby took the lease: Run returns,
// the gate stays closed, and OnNewLeader reports the new holder.
func TestLeaderElectionRecoveryStandbyTakesOver(t *testing.T) {
	const leaseName = "recovery-standby"

	server := startServer(t)
	proxy := newOutageProxy(t, server.ClientConfig)
	direct := kubernetes.NewForConfigOrDie(server.ClientConfig)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	recovery := &leaderelection.RecoveryConfig{RecoveryDeadline: 60 * time.Second}
	a := newCandidate(t, "a", leaseName, proxy.ClientConfig(), server.ClientConfig, recovery)
	b := newCandidate(t, "b", leaseName, server.ClientConfig, server.ClientConfig, recovery)

	ctxA, cancelA := context.WithCancel(ctx)
	t.Cleanup(cancelA)
	a.run(ctxA)
	a.waitStarted(30 * time.Second)
	a.requireGateOpen(ctx)

	ctxB, cancelB := context.WithCancel(ctx)
	t.Cleanup(cancelB)
	b.run(ctxB)
	// Let b observe a as the holder before the outage so that its takeover
	// is timed from a's last renewal rather than from b's start.
	if err := wait.PollUntilContextTimeout(ctx, pollInterval, 10*time.Second, true, func(context.Context) (bool, error) {
		for _, l := range b.observedLeaders() {
			if l == "a" {
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		t.Fatalf("b did not observe a as the leader: %v", err)
	}

	downAt := time.Now()
	proxy.SetDown(true)
	t.Logf("proxy down for a")
	b.waitStarted(leaseDuration + 14*time.Second)
	t.Logf("b acquired the lease %v after the outage began", time.Since(downAt).Round(time.Millisecond))
	a.waitGate(ctx, true, 10*time.Second)
	a.waitStopped(1, 5*time.Second)
	a.requireRunning()

	upAt := time.Now()
	proxy.SetDown(false)
	t.Logf("proxy up for a")
	a.waitRunReturned(15 * time.Second)
	t.Logf("a's Run returned %v after the outage ended", time.Since(upAt).Round(time.Millisecond))

	a.requireCounts(1, 1)
	a.requireStartedCtx(true)
	a.requireGateClosed(ctx)
	// OnNewLeader runs in its own goroutine, so it may trail Run's return.
	if err := wait.PollUntilContextTimeout(ctx, pollInterval, 5*time.Second, true, func(context.Context) (bool, error) {
		for _, l := range a.observedLeaders() {
			if l == "b" {
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		t.Errorf("a did not report b as the new leader; observed %v", a.observedLeaders())
	}
	if got := leaseHolder(ctx, t, direct, leaseName); got != "b" {
		t.Errorf("lease holder = %q, want %q", got, "b")
	}
	b.requireGateOpen(ctx)
	b.requireRunning()

	cancelB()
	b.waitRunReturned(30 * time.Second)
	b.requireCounts(1, 1)
}
