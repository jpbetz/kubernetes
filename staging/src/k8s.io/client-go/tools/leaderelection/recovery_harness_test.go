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

// Test harness for recovery mode. Everything here is meant to run inside a
// testing/synctest bubble: time is virtual, so scenarios assert exact
// timestamps and the whole suite runs in milliseconds of wall time.
//
// The pieces:
//   - simLeaseServer: the apiserver's view of one Lease, with resourceVersion
//     preconditions, shared by all electors in a test.
//   - simLock: one elector's resourcelock.Interface against the server, with a
//     per-client connectivity mode (ok, error, blackhole, latency, uncertain).
//   - gateProbe: sends reads and writes through le.WriteGate() so tests observe
//     the gate exactly the way a controller's client would.
//   - callbackRecorder: timestamps every callback invocation.
//   - electorHarness: bundles the above for one elector.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	rl "k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2/ktesting"
)

var leasesGR = schema.GroupResource{Group: "coordination.k8s.io", Resource: "leases"}

// commit is one successful write observed by the fake server.
type commit struct {
	by  string
	op  string
	at  time.Time
	rv  int64
	rec rl.LeaderElectionRecord
}

// simLeaseServer models a single Lease object as the apiserver stores it.
type simLeaseServer struct {
	mu      sync.Mutex
	exists  bool
	rv      int64
	record  rl.LeaderElectionRecord
	commits []commit
}

func newSimLeaseServer() *simLeaseServer { return &simLeaseServer{} }

func (s *simLeaseServer) get() (rl.LeaderElectionRecord, []byte, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.exists {
		return rl.LeaderElectionRecord{}, nil, 0, apierrors.NewNotFound(leasesGR, "lock")
	}
	raw, err := json.Marshal(s.record)
	if err != nil {
		panic(err)
	}
	return s.record, raw, s.rv, nil
}

func (s *simLeaseServer) create(by string, rec rl.LeaderElectionRecord) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exists {
		return 0, apierrors.NewAlreadyExists(leasesGR, "lock")
	}
	s.exists = true
	s.rv++
	s.record = rec
	s.commits = append(s.commits, commit{by: by, op: "create", at: time.Now(), rv: s.rv, rec: rec})
	return s.rv, nil
}

// update applies a conditional update, like the real apiserver does for the
// resourceVersion carried by the Lease object the LeaseLock last observed.
func (s *simLeaseServer) update(by string, seenRV int64, rec rl.LeaderElectionRecord) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.exists {
		return 0, apierrors.NewNotFound(leasesGR, "lock")
	}
	if seenRV != s.rv {
		return 0, apierrors.NewConflict(leasesGR, "lock", fmt.Errorf("resourceVersion %d != %d", seenRV, s.rv))
	}
	s.rv++
	s.record = rec
	s.commits = append(s.commits, commit{by: by, op: "update", at: time.Now(), rv: s.rv, rec: rec})
	return s.rv, nil
}

// forceHolder simulates an out-of-band takeover (or admin edit) by writing a
// new holder directly into storage.
func (s *simLeaseServer) forceHolder(holder string, leaseDuration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.exists = true
	s.rv++
	s.record = rl.LeaderElectionRecord{
		HolderIdentity:       holder,
		LeaseDurationSeconds: int(leaseDuration / time.Second),
		AcquireTime:          metaTime(now),
		RenewTime:            metaTime(now),
		LeaderTransitions:    s.record.LeaderTransitions + 1,
	}
	s.commits = append(s.commits, commit{by: holder, op: "force", at: now, rv: s.rv, rec: s.record})
}

// delete removes the Lease, as an administrator might.
func (s *simLeaseServer) delete() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exists = false
	s.record = rl.LeaderElectionRecord{}
}

func (s *simLeaseServer) holder() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.exists {
		return ""
	}
	return s.record.HolderIdentity
}

// commitLog returns every successful write the server saw.
func (s *simLeaseServer) commitLog() []commit {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]commit(nil), s.commits...)
}

// releaseCommitsBy counts successful writes by id that cleared the holder.
func (s *simLeaseServer) releaseCommitsBy(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.commits {
		if c.by == id && c.rec.HolderIdentity == "" {
			n++
		}
	}
	return n
}

// release simulates another candidate releasing the lease the way
// ReleaseOnCancel does: holder cleared, lease duration 1s.
func (s *simLeaseServer) release(by string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.rv++
	s.record = rl.LeaderElectionRecord{
		LeaseDurationSeconds: 1,
		AcquireTime:          metaTime(now),
		RenewTime:            metaTime(now),
		LeaderTransitions:    s.record.LeaderTransitions,
	}
	s.commits = append(s.commits, commit{by: by, op: "release", at: now, rv: s.rv, rec: s.record})
}

// lastCommitBy returns the time of the most recent successful write by id at or
// before t, and whether there was one.
func (s *simLeaseServer) lastCommitBy(id string, t time.Time) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var last time.Time
	found := false
	for _, c := range s.commits {
		if c.by == id && !c.at.After(t) {
			last = c.at
			found = true
		}
	}
	return last, found
}

// lockMode is one client's connectivity to the fake server.
type lockMode int

const (
	// modeOK: requests succeed, after the configured latency. Writes are
	// applied halfway through the latency, so a write whose context ends in
	// the second half is an uncertain commit: applied, response lost.
	modeOK lockMode = iota
	// modeError: requests fail immediately with a server error.
	modeError
	// modeBlackhole: requests hang until the request context is done (client
	// timeout or caller cancellation) and then fail with the context error.
	modeBlackhole
	// modeUncertain: writes are applied by the server immediately but the
	// response is lost: the client hangs until its context is done. Reads
	// behave like blackhole.
	modeUncertain
)

func (m lockMode) String() string {
	switch m {
	case modeOK:
		return "ok"
	case modeError:
		return "error"
	case modeBlackhole:
		return "blackhole"
	case modeUncertain:
		return "uncertain"
	}
	return fmt.Sprintf("mode(%d)", int(m))
}

// lockOp is one call the elector made on its lock.
type lockOp struct {
	op   string
	at   time.Time // when the call started
	done time.Time // when it returned
	mode lockMode
	err  error
}

func (o lockOp) String() string {
	return fmt.Sprintf("%s@%v->%v(%s,%v)", o.op, o.at.Sub(bubbleEpoch), o.done.Sub(bubbleEpoch), o.mode, o.err)
}

// bubbleEpoch is synctest's fixed start time, used to print offsets.
var bubbleEpoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// simLock is one elector's view of the shared fake server.
type simLock struct {
	id     string
	server *simLeaseServer

	mu         sync.Mutex
	mode       lockMode
	minLatency time.Duration
	maxLatency time.Duration
	// timeout bounds every request, like the rest.Config.Timeout that
	// resourcelock.NewFromKubeconfig sets to RenewDeadline/2.
	timeout time.Duration
	// ignoreCtx makes requests ignore context cancellation and deadlines,
	// modelling a client whose transport does not honour them. Blackholed
	// requests then block on stuck instead.
	ignoreCtx bool
	stuck     chan struct{}
	rng       *rand.Rand
	seenRV    int64 // resourceVersion of the Lease object last observed, like LeaseLock.lease
	ops       []lockOp
	events    []string
}

var _ rl.Interface = &simLock{}

func newSimLock(id string, server *simLeaseServer, timeout time.Duration) *simLock {
	return &simLock{id: id, server: server, timeout: timeout, rng: rand.New(rand.NewSource(int64(len(id)) + 1)), stuck: make(chan struct{})}
}

func (l *simLock) setMode(m lockMode) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.mode = m
}

// setLatency sets the response latency range for modeOK requests.
func (l *simLock) setLatency(min, max time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.minLatency, l.maxLatency = min, max
}

func (l *simLock) setIgnoreCtx(v bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ignoreCtx = v
}

// unstick releases requests that are blocked because ignoreCtx is set while
// blackholed. Tests must call it before the bubble ends.
func (l *simLock) unstick() {
	l.mu.Lock()
	defer l.mu.Unlock()
	select {
	case <-l.stuck:
	default:
		close(l.stuck)
	}
}

func (l *simLock) opLog() []lockOp {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]lockOp(nil), l.ops...)
}

// opsBetween returns ops that started in [from, to].
func (l *simLock) opsBetween(from, to time.Time) []lockOp {
	var out []lockOp
	for _, op := range l.opLog() {
		if !op.at.Before(from) && !op.at.After(to) {
			out = append(out, op)
		}
	}
	return out
}

func (l *simLock) eventLog() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

func (l *simLock) record(op string, started time.Time, mode lockMode, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ops = append(l.ops, lockOp{op: op, at: started, done: time.Now(), mode: mode, err: err})
}

func (l *simLock) snapshot() (mode lockMode, latency, timeout time.Duration, ignore bool, stuck chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	latency = l.minLatency
	if l.maxLatency > l.minLatency {
		latency += time.Duration(l.rng.Int63n(int64(l.maxLatency - l.minLatency)))
	}
	return l.mode, latency, l.timeout, l.ignoreCtx, l.stuck
}

// sleepCtx waits d, or until ctx is done unless ignore is set.
func sleepCtx(ctx context.Context, d time.Duration, ignore bool) error {
	if d <= 0 {
		if !ignore {
			return ctx.Err()
		}
		return nil
	}
	if ignore {
		time.Sleep(d)
		return nil
	}
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// request runs one call against the server according to the lock's mode.
// apply performs the server-side operation and returns the new (or current)
// resourceVersion. For reads apply has no side effects.
func (l *simLock) request(ctx context.Context, op string, isWrite bool, apply func() (int64, error)) (int64, error) {
	started := time.Now()
	mode, latency, timeout, ignore, stuck := l.snapshot()
	if timeout > 0 && !ignore {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	fail := func(err error) (int64, error) {
		l.record(op, started, mode, err)
		return 0, err
	}
	hang := func() error {
		if ignore {
			<-stuck
			return errors.New("stuck request released by test")
		}
		<-ctx.Done()
		return ctx.Err()
	}
	switch mode {
	case modeError:
		return fail(apierrors.NewInternalError(errors.New("fake server error")))
	case modeBlackhole:
		return fail(hang())
	case modeUncertain:
		if isWrite {
			_, _ = apply()
		}
		return fail(hang())
	}
	// modeOK: request in transit.
	if err := sleepCtx(ctx, latency/2, ignore); err != nil {
		return fail(err)
	}
	rv, applyErr := apply()
	// Response in transit; the write (if any) is already applied.
	if err := sleepCtx(ctx, latency-latency/2, ignore); err != nil {
		return fail(err)
	}
	l.record(op, started, mode, applyErr)
	return rv, applyErr
}

func (l *simLock) Get(ctx context.Context) (*rl.LeaderElectionRecord, []byte, error) {
	var rec rl.LeaderElectionRecord
	var raw []byte
	rv, err := l.request(ctx, "get", false, func() (int64, error) {
		var rv int64
		var err error
		rec, raw, rv, err = l.server.get()
		return rv, err
	})
	if err != nil {
		return nil, nil, err
	}
	l.mu.Lock()
	l.seenRV = rv
	l.mu.Unlock()
	return &rec, raw, nil
}

func (l *simLock) Create(ctx context.Context, ler rl.LeaderElectionRecord) error {
	rv, err := l.request(ctx, "create", true, func() (int64, error) { return l.server.create(l.id, ler) })
	if err != nil {
		return err
	}
	l.mu.Lock()
	l.seenRV = rv
	l.mu.Unlock()
	return nil
}

func (l *simLock) Update(ctx context.Context, ler rl.LeaderElectionRecord) error {
	l.mu.Lock()
	seen := l.seenRV
	l.mu.Unlock()
	if seen == 0 {
		return errors.New("lease not initialized, call get or create first")
	}
	rv, err := l.request(ctx, "update", true, func() (int64, error) { return l.server.update(l.id, seen, ler) })
	if err != nil {
		return err
	}
	l.mu.Lock()
	l.seenRV = rv
	l.mu.Unlock()
	return nil
}

func (l *simLock) RecordEvent(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, s)
}
func (l *simLock) Identity() string { return l.id }
func (l *simLock) Describe() string { return "fake/" + l.id }

// callbackRecorder timestamps callback invocations.
type callbackRecorder struct {
	mu         sync.Mutex
	started    []time.Time
	startedCtx []context.Context
	stopped    []time.Time
	newLeader  []leaderObservation
	// onStopped, if set, runs inside OnStoppedLeading (to observe ordering).
	onStopped func()
}

type leaderObservation struct {
	id string
	at time.Time
}

func (r *callbackRecorder) callbacks() LeaderCallbacks {
	return LeaderCallbacks{
		OnStartedLeading: func(ctx context.Context) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.started = append(r.started, time.Now())
			r.startedCtx = append(r.startedCtx, ctx)
		},
		OnStoppedLeading: func() {
			r.mu.Lock()
			hook := r.onStopped
			r.stopped = append(r.stopped, time.Now())
			r.mu.Unlock()
			if hook != nil {
				hook()
			}
		},
		OnNewLeader: func(id string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.newLeader = append(r.newLeader, leaderObservation{id: id, at: time.Now()})
		},
	}
}

func (r *callbackRecorder) startedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.started)
}

func (r *callbackRecorder) stoppedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.stopped)
}

func (r *callbackRecorder) stoppedTimes() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Time(nil), r.stopped...)
}

func (r *callbackRecorder) leaders() []leaderObservation {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]leaderObservation(nil), r.newLeader...)
}

// leadingCtx returns the context passed to the (single) OnStartedLeading call.
func (r *callbackRecorder) leadingCtx(t *testing.T) context.Context {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.startedCtx) != 1 {
		t.Fatalf("expected exactly one OnStartedLeading call, got %d", len(r.startedCtx))
	}
	return r.startedCtx[0]
}

// probeTransport is the http.RoundTripper under the write gate. Reads return
// 200 immediately. Writes block until released (or their context is done), so
// tests can hold a write in flight across a gate transition.
type probeTransport struct {
	mu       sync.Mutex
	requests []probeRequest
}

type probeRequest struct {
	method string
	at     time.Time
	// ctx is the context the request carried when it reached the transport.
	ctx context.Context
	// release unblocks an in-flight write.
	release chan struct{}
}

func (p *probeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := probeRequest{method: req.Method, at: time.Now(), ctx: req.Context(), release: make(chan struct{})}
	p.mu.Lock()
	p.requests = append(p.requests, rec)
	p.mu.Unlock()
	if isReadMethod(req.Method) {
		return okResponse(req), nil
	}
	select {
	case <-rec.release:
		return okResponse(req), nil
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
}

func isReadMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}

func okResponse(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader("ok")),
		Header:     http.Header{},
		Request:    req,
	}
}

// reached reports how many requests of the given method reached the underlying
// transport, i.e. passed the gate.
func (p *probeTransport) reached(method string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, r := range p.requests {
		if r.method == method {
			n++
		}
	}
	return n
}

// last returns the most recent request that reached the transport.
func (p *probeTransport) last() (probeRequest, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) == 0 {
		return probeRequest{}, false
	}
	return p.requests[len(p.requests)-1], true
}

// releaseAll unblocks every in-flight write.
func (p *probeTransport) releaseAll() { p.releaseFrom(0) }

// releaseFrom unblocks in-flight writes registered at index from onwards.
func (p *probeTransport) releaseFrom(from int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := from; i < len(p.requests); i++ {
		r := p.requests[i]
		select {
		case <-r.release:
		default:
			close(r.release)
		}
	}
}

func (p *probeTransport) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

// gateProbe sends requests through the gated transport.
type gateProbe struct {
	inner *probeTransport
	rt    http.RoundTripper
}

func newGateProbe(le *LeaderElector) *gateProbe {
	inner := &probeTransport{}
	return &gateProbe{inner: inner, rt: le.WriteGate()(inner)}
}

func (g *gateProbe) do(ctx context.Context, method string) error {
	req, err := http.NewRequestWithContext(ctx, method, "https://example.invalid/api/v1/namespaces/default/configmaps/x", nil)
	if err != nil {
		return err
	}
	resp, err := g.rt.RoundTrip(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}

// read sends a GET, which must pass in every gate state.
func (g *gateProbe) read(ctx context.Context) error { return g.do(ctx, http.MethodGet) }

// write sends a PUT that completes immediately (released before return).
// Returns nil if the gate let it through. Writes held open with startWrite
// are not disturbed.
func (g *gateProbe) write(ctx context.Context) error {
	before := g.inner.count()
	done := make(chan error, 1)
	go func() { done <- g.do(ctx, http.MethodPut) }()
	// Let the request reach the inner transport (or be rejected), then release it.
	synctest.Wait()
	g.inner.releaseFrom(before)
	return <-done
}

// probeWrite is a write held open inside the transport.
type probeWrite struct {
	result chan error
	rec    *probeRequest
}

// startWrite issues a write and returns once it is blocked inside the inner
// transport (i.e. it passed the gate) or was rejected. The caller checks
// rejected() first. The write's own context never ends, so only the gate (or
// release) can complete it.
func (g *gateProbe) startWrite(ctx context.Context) *probeWrite {
	w := &probeWrite{result: make(chan error, 1)}
	before := g.inner.count()
	go func() { w.result <- g.do(context.WithoutCancel(ctx), http.MethodPost) }()
	synctest.Wait()
	g.inner.mu.Lock()
	if len(g.inner.requests) > before {
		w.rec = &g.inner.requests[len(g.inner.requests)-1]
	}
	g.inner.mu.Unlock()
	return w
}

// rejected reports whether the write was refused by the gate without reaching
// the transport. Only meaningful immediately after startWrite.
func (w *probeWrite) rejected() (bool, error) {
	select {
	case err := <-w.result:
		w.result <- err
		return w.rec == nil, err
	default:
		return false, nil
	}
}

// wait returns the write's outcome once it completes.
func (w *probeWrite) wait() error { return <-w.result }

// release lets the in-flight write complete successfully.
func (w *probeWrite) release() {
	if w.rec != nil {
		select {
		case <-w.rec.release:
		default:
			close(w.rec.release)
		}
	}
}

// metricsSpy records leaderOn/leaderOff transitions with virtual timestamps.
type metricsSpy struct {
	mu     sync.Mutex
	events []string
	t0     time.Time
}

func (m *metricsSpy) record(kind string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, fmt.Sprintf("%s@%v", kind, time.Since(m.t0)))
}
func (m *metricsSpy) leaderOn(string)          { m.record("on") }
func (m *metricsSpy) leaderOff(string)         { m.record("off") }
func (m *metricsSpy) slowpathExercised(string) {}
func (m *metricsSpy) sequence() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.events...)
}

// electorHarness bundles one elector with its lock, probe and recorder.
type electorHarness struct {
	t       *testing.T
	id      string
	le      *LeaderElector
	lock    *simLock
	probe   *gateProbe
	rec     *callbackRecorder
	metrics *metricsSpy
	done    chan struct{}
	retMu   sync.Mutex
	retAt   time.Time
	hasRet  bool
	started atomic.Bool
}

type harnessConfig struct {
	LeaseDuration    time.Duration
	RenewDeadline    time.Duration
	RetryPeriod      time.Duration
	Recovery         *RecoveryConfig
	ReleaseOnCancel  bool
	Coordinated      bool
	WatchDog         *HealthzAdaptor
	WriteGate        *WriteGate
	clientTimeout    time.Duration // 0 => RenewDeadline/2 like NewFromKubeconfig
	disableClientTmo bool
}

// standardTimers are the values used unless a scenario says otherwise:
// LeaseDuration 15s, RenewDeadline 10s, RetryPeriod 2s (kube defaults).
func standardTimers() harnessConfig {
	return harnessConfig{LeaseDuration: 15 * time.Second, RenewDeadline: 10 * time.Second, RetryPeriod: 2 * time.Second}
}

func newElectorHarness(t *testing.T, server *simLeaseServer, id string, cfg harnessConfig) *electorHarness {
	t.Helper()
	timeout := cfg.clientTimeout
	if timeout == 0 && !cfg.disableClientTmo {
		timeout = cfg.RenewDeadline / 2
		if timeout < time.Second {
			timeout = time.Second
		}
	}
	lock := newSimLock(id, server, timeout)
	rec := &callbackRecorder{}
	le, err := NewLeaderElector(LeaderElectionConfig{
		Lock:            lock,
		LeaseDuration:   cfg.LeaseDuration,
		RenewDeadline:   cfg.RenewDeadline,
		RetryPeriod:     cfg.RetryPeriod,
		Callbacks:       rec.callbacks(),
		Recovery:        cfg.Recovery,
		ReleaseOnCancel: cfg.ReleaseOnCancel,
		Coordinated:     cfg.Coordinated,
		WatchDog:        cfg.WatchDog,
		WriteGate:       cfg.WriteGate,
		Name:            "harness-" + id,
	})
	if err != nil {
		t.Fatalf("NewLeaderElector(%s): %v", id, err)
	}
	if cfg.WatchDog != nil {
		cfg.WatchDog.SetLeaderElection(le)
	}
	spy := &metricsSpy{t0: time.Now()}
	le.metrics = spy
	return &electorHarness{t: t, id: id, le: le, lock: lock, probe: newGateProbe(le), rec: rec, metrics: spy, done: make(chan struct{})}
}

// run starts le.Run in the bubble and records when it returns.
func (h *electorHarness) run(ctx context.Context) {
	if h.started.Swap(true) {
		h.t.Fatalf("%s: run called twice", h.id)
	}
	go func() {
		defer close(h.done)
		h.le.Run(ctx)
		h.retMu.Lock()
		h.retAt = time.Now()
		h.hasRet = true
		h.retMu.Unlock()
	}()
}

func (h *electorHarness) returned() bool {
	select {
	case <-h.done:
		return true
	default:
		return false
	}
}

func (h *electorHarness) returnedAt() (time.Time, bool) {
	h.retMu.Lock()
	defer h.retMu.Unlock()
	return h.retAt, h.hasRet
}

// newTestContext returns a logger-carrying context for use inside a bubble.
func newTestContext(t *testing.T) (context.Context, context.CancelFunc) {
	_, ctx := ktesting.NewTestContext(t)
	return context.WithCancel(ctx)
}

// advance moves virtual time forward and lets everything that becomes runnable settle.
func advance(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

// settle lets all runnable goroutines in the bubble finish without advancing time.
func settle() { synctest.Wait() }

// since is a readable virtual-time offset for messages.
func since(start time.Time) time.Duration { return time.Since(start) }

// expectGateOpen / expectGateClosed assert the probe's view of the gate.
func (h *electorHarness) expectGateOpen(ctx context.Context, start time.Time, msg string) {
	h.t.Helper()
	if err := h.probe.write(ctx); err != nil {
		h.t.Errorf("t=%v %s: %s: expected write to pass the gate, got %v", since(start), h.id, msg, err)
	}
}

func (h *electorHarness) expectGateClosed(ctx context.Context, start time.Time, msg string) {
	h.t.Helper()
	err := h.probe.write(ctx)
	if !errors.Is(err, ErrWriteGateClosed) {
		h.t.Errorf("t=%v %s: %s: expected ErrWriteGateClosed, got %v", since(start), h.id, msg, err)
	}
	if err := h.probe.read(ctx); err != nil {
		h.t.Errorf("t=%v %s: %s: expected read to pass while gate closed, got %v", since(start), h.id, msg, err)
	}
}

func (h *electorHarness) expectReturned(start time.Time, msg string) {
	h.t.Helper()
	if !h.returned() {
		h.t.Fatalf("t=%v %s: %s: expected Run to have returned; ops=%v", since(start), h.id, msg, h.lock.opLog())
	}
}

func (h *electorHarness) expectRunning(start time.Time, msg string) {
	h.t.Helper()
	if h.returned() {
		h.t.Fatalf("t=%v %s: %s: expected Run to still be running; ops=%v", since(start), h.id, msg, h.lock.opLog())
	}
}

func (h *electorHarness) expectCounts(start time.Time, started, stopped int, msg string) {
	h.t.Helper()
	if got := h.rec.startedCount(); got != started {
		h.t.Errorf("t=%v %s: %s: OnStartedLeading count = %d, want %d", since(start), h.id, msg, got, started)
	}
	if got := h.rec.stoppedCount(); got != stopped {
		h.t.Errorf("t=%v %s: %s: OnStoppedLeading count = %d, want %d (times %v)", since(start), h.id, msg, got, stopped, h.rec.stoppedTimes())
	}
}

// metaTime converts to the API time type used in records.
func metaTime(t time.Time) metav1.Time { return metav1.NewTime(t) }
