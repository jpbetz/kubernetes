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
	"fmt"
	"io"
	"net/http"
	"sync"

	utilnet "k8s.io/apimachinery/pkg/util/net"
	clientfeatures "k8s.io/client-go/features"
	"k8s.io/client-go/transport"
)

// ErrWriteGateClosed is returned by a transport wrapped with
// LeaderElector.WriteGate for write requests made while the elector is not
// leading. A write that was in flight when the elector stopped leading is
// cancelled and also fails with an error wrapping ErrWriteGateClosed. As with
// any cancelled request, the server may already have applied such a write;
// the error does not distinguish that case from a rejected write.
var ErrWriteGateClosed = errors.New("leader election write gate is closed: not leading")

// WriteGate is a transport wrapper that rejects write requests (every method
// other than GET, HEAD, OPTIONS and TRACE) while the elector it belongs to is
// not leading, and cancels writes that are in flight when the elector stops
// leading. Rejected and cancelled writes fail with an error wrapping
// ErrWriteGateClosed. Reads always pass.
//
// Create it with NewWriteGate before building any client, apply Wrapper to the
// rest.Config those clients are built from (for example with rest.Config.Wrap),
// and hand it to NewLeaderElector through LeaderElectionConfig.WriteGate. A
// gate is bound to the first LeaderElector constructed with it and cannot be
// handed to another. The clients used by the elector's lock and by its event
// recorder must be built from an ungated config, or the elector could neither
// renew nor report while the gate is closed.
//
// Upgraded connections (SPDY and WebSocket streams such as exec, attach and
// port-forward) are checked only at the handshake, which is a write; once
// established they are not tracked, so closing the gate neither cancels nor
// waits for them.
//
// The LeaderElectionRecovery feature gate is read once, in NewWriteGate. When
// it is disabled, Wrapper passes every request through unchanged.
// NewLeaderElector rejects a gate that was created while the feature gate was
// disabled if the feature gate is enabled by the time the elector is created.
type WriteGate struct {
	// gate is nil when the feature gate is disabled.
	gate *writeGate

	mu    sync.Mutex
	owned bool
}

// NewWriteGate returns a new, closed write gate.
func NewWriteGate() *WriteGate {
	if !clientfeatures.FeatureGates().Enabled(clientfeatures.LeaderElectionRecovery) {
		return &WriteGate{}
	}
	return &WriteGate{gate: newWriteGate()}
}

// Wrapper returns the transport wrapper that applies this gate.
func (g *WriteGate) Wrapper() transport.WrapperFunc {
	if g == nil || g.gate == nil {
		return passthroughWriteGate()
	}
	return g.gate.wrapper()
}

// claim binds the gate to an elector. It fails if another elector already
// claimed it: two electors sharing a gate would defeat its purpose.
func (g *WriteGate) claim() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.owned {
		return errors.New("WriteGate is already in use by another LeaderElector")
	}
	g.owned = true
	return nil
}

func (g *WriteGate) open() {
	if g.gate != nil {
		g.gate.open()
	}
}

// close returns once every in-flight gated write has been cancelled.
func (g *WriteGate) close() {
	if g.gate != nil {
		g.gate.close()
	}
}

func (g *WriteGate) isOpen() bool {
	return g.gate != nil && g.gate.isOpen()
}

// writeGate tracks whether the elector is leading and gates write requests.
//
// Only the election loop opens and closes it. close returns only after every
// in-flight write's context has been cancelled, so a caller that observes
// OnStoppedLeading (which runs after close) knows no gated write can still be
// making progress on the client side.
type writeGate struct {
	mu       sync.Mutex
	isOpen_  bool
	inflight map[*inflightWrite]struct{}
}

// inflightWrite is one write request that passed the gate and has not
// finished (response body closed or request failed).
type inflightWrite struct {
	cancel context.CancelCauseFunc
}

func newWriteGate() *writeGate {
	return &writeGate{inflight: map[*inflightWrite]struct{}{}}
}

// open lets writes pass. Idempotent.
func (g *writeGate) open() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.isOpen_ = true
}

// close rejects new writes and cancels every write currently in flight.
// Idempotent. When close returns, all in-flight writes have been cancelled.
func (g *writeGate) close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.isOpen_ = false
	for w := range g.inflight {
		w.cancel(ErrWriteGateClosed)
	}
	clear(g.inflight)
}

// isOpen reports whether writes currently pass.
func (g *writeGate) isOpen() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.isOpen_
}

// inflightCount reports how many writes are tracked as in flight (for tests).
func (g *writeGate) inflightCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.inflight)
}

// admit registers a new write if the gate is open. It returns the context the
// request must use and a release function to call when the request finishes.
func (g *writeGate) admit(parent context.Context) (context.Context, func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.isOpen_ {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancelCause(parent)
	w := &inflightWrite{cancel: cancel}
	g.inflight[w] = struct{}{}
	var once sync.Once
	release := func() {
		once.Do(func() {
			g.mu.Lock()
			delete(g.inflight, w)
			g.mu.Unlock()
			cancel(nil)
		})
	}
	return ctx, release, true
}

// wrapper returns the transport wrapper that applies this gate.
func (g *writeGate) wrapper() transport.WrapperFunc {
	return func(rt http.RoundTripper) http.RoundTripper {
		return &gatedRoundTripper{gate: g, rt: rt}
	}
}

// passthroughWriteGate is the wrapper handed out when the LeaderElectionRecovery
// feature gate is disabled.
func passthroughWriteGate() transport.WrapperFunc {
	return func(rt http.RoundTripper) http.RoundTripper { return rt }
}

// isWriteMethod classifies requests. Only the safe methods of RFC 9110 pass a
// closed gate; anything else, including unknown methods, is treated as a write.
func isWriteMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	}
	return true
}

type gatedRoundTripper struct {
	gate *writeGate
	rt   http.RoundTripper
}

var _ utilnet.RoundTripperWrapper = &gatedRoundTripper{}

func (t *gatedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if !isWriteMethod(req.Method) {
		return t.rt.RoundTrip(req)
	}
	ctx, release, ok := t.gate.admit(req.Context())
	if !ok {
		return nil, ErrWriteGateClosed
	}
	resp, err := t.rt.RoundTrip(req.WithContext(ctx))
	if err != nil {
		release()
		if errors.Is(err, ErrWriteGateClosed) {
			return nil, err
		}
		if cause := context.Cause(ctx); errors.Is(cause, ErrWriteGateClosed) {
			// The gate closed while the request was in flight. Report that,
			// keeping the transport's error for context.
			return nil, fmt.Errorf("%w: %v", ErrWriteGateClosed, err)
		}
		return nil, err
	}
	if resp.Body == nil {
		// Nothing left in flight.
		release()
		return resp, nil
	}
	// The write stays in flight until the caller is done with the body.
	resp.Body = &releasingBody{ReadCloser: resp.Body, ctx: ctx, release: release}
	return resp, nil
}

func (t *gatedRoundTripper) CancelRequest(req *http.Request) {
	type canceler interface{ CancelRequest(*http.Request) }
	if c, ok := t.rt.(canceler); ok {
		c.CancelRequest(req)
	}
}

func (t *gatedRoundTripper) WrappedRoundTripper() http.RoundTripper { return t.rt }

// releasingBody keeps a write registered as in flight until its body is
// closed, and reports body reads that failed because the gate closed as gate
// errors. (HTTP/2 transports return a bare context error for reads on a
// cancelled request, where HTTP/1 returns the cancellation cause.)
type releasingBody struct {
	io.ReadCloser
	ctx     context.Context
	release func()
}

func (b *releasingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && err != io.EOF && !errors.Is(err, ErrWriteGateClosed) {
		if cause := context.Cause(b.ctx); errors.Is(cause, ErrWriteGateClosed) {
			return n, fmt.Errorf("%w: %v", ErrWriteGateClosed, err)
		}
	}
	return n, err
}

func (b *releasingBody) Close() error {
	defer b.release()
	return b.ReadCloser.Close()
}
