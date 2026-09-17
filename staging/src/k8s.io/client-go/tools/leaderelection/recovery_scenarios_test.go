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

// Scenario tests for recovery mode. Each test runs the real election loop
// inside a testing/synctest bubble against the fake lease server, so every
// timestamp below is exact virtual time and the tests take milliseconds.
//
// Timeline arithmetic with the standard timers (LeaseDuration 15s,
// RenewDeadline 10s, RetryPeriod 2s, client timeout RenewDeadline/2 = 5s):
//   - acquired at 0; renewals at 0, 2, 4, ...
//   - an outage starting at 5s makes the attempt at 6s the first to fail; the
//     renew poll started at 6s gives up at 6s+RenewDeadline = 16s, so the
//     lease is lost at exactly 16s = lastSuccess(4s) + RetryPeriod + RenewDeadline.
//   - while inactive (and while acquiring), attempts run every RetryPeriod
//     plus a jitter of up to JitterFactor*RetryPeriod, so the gap between two
//     attempts is in [2s, 4.4s).

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	clientfeatures "k8s.io/client-go/features"
	clientfeaturestesting "k8s.io/client-go/features/testing"
)

const (
	sec       = time.Second
	msec      = time.Millisecond
	outageAt  = 5 * sec
	lossAt    = 16 * sec // for an outage starting at outageAt with standard timers
	tolerance = 20 * sec // HealthzAdaptor timeout used by kube-controller-manager
	// maxGap is the longest possible gap between two jittered attempts.
	maxGap     = time.Duration(float64(2*sec) * (1 + JitterFactor))
	jitterSlop = 100 * msec
)

func enableRecovery(t *testing.T) {
	t.Helper()
	clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.LeaderElectionRecovery, true)
}

func recoveryCfg(deadline time.Duration) harnessConfig {
	c := standardTimers()
	c.Recovery = &RecoveryConfig{RecoveryDeadline: deadline}
	return c
}

// scenario is one bubble: a fake server, a context, and the electors started.
type scenario struct {
	t        *testing.T
	ctx      context.Context
	cancel   context.CancelFunc
	t0       time.Time
	server   *simLeaseServer
	electors []*electorHarness

	violationsMu sync.Mutex
	violations   []time.Duration
}

func runScenario(t *testing.T, fn func(s *scenario)) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := newTestContext(t)
		s := &scenario{t: t, ctx: ctx, cancel: cancel, t0: time.Now(), server: newSimLeaseServer()}
		defer s.shutdown()
		fn(s)
	})
}

// shutdown cancels the context and makes sure every elector returns, so the
// bubble can exit (synctest fails the test on leaked goroutines).
func (s *scenario) shutdown() {
	s.t.Helper()
	s.cancel()
	for _, h := range s.electors {
		h.lock.unstick()
	}
	advance(2 * time.Minute)
	for _, h := range s.electors {
		if !h.returned() {
			s.t.Errorf("%s: Run did not return after cancel; ops=%v", h.id, h.lock.opLog())
		}
	}
	s.violationsMu.Lock()
	defer s.violationsMu.Unlock()
	if len(s.violations) > 0 {
		s.t.Errorf("two write gates were open at the same time at %v", s.violations)
	}
}

func (s *scenario) now() time.Duration { return time.Since(s.t0) }

// t0Ctx is a context that is never cancelled, for probes that must outlive s.ctx.
func (s *scenario) t0Ctx() context.Context { return context.WithoutCancel(s.ctx) }

// at advances virtual time to the given offset from the start and settles.
func (s *scenario) at(d time.Duration) {
	s.t.Helper()
	if d < s.now() {
		s.t.Fatalf("cannot go back in time: now=%v requested=%v", s.now(), d)
	}
	advance(d - s.now())
}

// newElector builds an elector but does not start it.
func (s *scenario) newElector(id string, cfg harnessConfig) *electorHarness {
	s.t.Helper()
	h := newElectorHarness(s.t, s.server, id, cfg)
	s.electors = append(s.electors, h)
	return h
}

// start runs an elector and lets it take its first steps.
func (s *scenario) start(h *electorHarness) {
	h.run(s.ctx)
	settle()
}

// elector builds and starts an elector.
func (s *scenario) elector(id string, cfg harnessConfig) *electorHarness {
	s.t.Helper()
	h := s.newElector(id, cfg)
	s.start(h)
	return h
}

// monitorExclusion samples all gates every 10ms of virtual time and records
// any instant at which more than one is open.
func (s *scenario) monitorExclusion(hs ...*electorHarness) {
	go func() {
		ticker := time.NewTicker(10 * msec)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				open := 0
				for _, h := range hs {
					if h.le.gate != nil && h.le.gate.isOpen() {
						open++
					}
				}
				if open > 1 {
					s.violationsMu.Lock()
					s.violations = append(s.violations, s.now())
					s.violationsMu.Unlock()
				}
			}
		}
	}()
}

func (s *scenario) expectTime(got time.Time, want time.Duration, msg string) {
	s.t.Helper()
	if got.Sub(s.t0) != want {
		s.t.Errorf("%s: at %v, want %v", msg, got.Sub(s.t0), want)
	}
}

func (s *scenario) expectTimeWithin(got time.Time, lo, hi time.Duration, msg string) {
	s.t.Helper()
	d := got.Sub(s.t0)
	if d < lo || d > hi {
		s.t.Errorf("%s: at %v, want within [%v, %v]", msg, d, lo, hi)
	}
}

func (s *scenario) expectReturnedAt(h *electorHarness, want time.Duration, msg string) {
	s.t.Helper()
	h.expectReturned(s.t0, msg)
	at, _ := h.returnedAt()
	s.expectTime(at, want, msg+": Run returned")
}

func (s *scenario) expectLeadingCtxLive(h *electorHarness, msg string) {
	s.t.Helper()
	if err := h.rec.leadingCtx(s.t).Err(); err != nil {
		s.t.Errorf("t=%v %s: %s: OnStartedLeading context cancelled: %v", s.now(), h.id, msg, err)
	}
}

func (s *scenario) expectLeadingCtxCancelled(h *electorHarness, msg string) {
	s.t.Helper()
	if err := h.rec.leadingCtx(s.t).Err(); err == nil {
		s.t.Errorf("t=%v %s: %s: OnStartedLeading context still live", s.now(), h.id, msg)
	}
}

func opNames(ops []lockOp) []string {
	out := make([]string, 0, len(ops))
	for _, op := range ops {
		name := op.op
		if op.err != nil {
			name += "!"
		}
		out = append(out, name)
	}
	return out
}

func lastEvent(h *electorHarness) string {
	ev := h.lock.eventLog()
	if len(ev) == 0 {
		return ""
	}
	return ev[len(ev)-1]
}

// S1: acquiring opens the gate; renewals continue every RetryPeriod.
func TestRecoveryScenario_AcquireOpensGate(t *testing.T) {
	enableRecovery(t)
	runScenario(t, func(s *scenario) {
		a := s.newElector("a", recoveryCfg(0))
		a.expectGateClosed(s.ctx, s.t0, "before Run")
		s.start(a)

		a.expectCounts(s.t0, 1, 0, "after acquire")
		a.expectGateOpen(s.ctx, s.t0, "after acquire")
		s.expectLeadingCtxLive(a, "after acquire")
		if got := s.server.holder(); got != "a" {
			t.Errorf("holder = %q, want a", got)
		}
		if !a.le.IsLeader() || a.le.GetLeader() != "a" {
			t.Errorf("IsLeader=%v GetLeader=%q", a.le.IsLeader(), a.le.GetLeader())
		}
		if got := lastEvent(a); got != "became leader" {
			t.Errorf("last event = %q, want became leader", got)
		}

		s.at(6 * sec)
		ops := a.lock.opLog()
		want := []string{"get!", "create", "update", "update", "update", "update"}
		if fmt.Sprint(opNames(ops)) != fmt.Sprint(want) {
			t.Errorf("ops = %v, want %v", ops, want)
		}
		for i, wantAt := range []time.Duration{0, 0, 0, 2 * sec, 4 * sec, 6 * sec} {
			s.expectTime(ops[i].at, wantAt, fmt.Sprintf("op %d", i))
		}
		a.expectGateOpen(s.ctx, s.t0, "t=6s")
		a.expectRunning(s.t0, "t=6s")
		a.expectCounts(s.t0, 1, 0, "t=6s")
	})
}

// S2+S3: losing the lease closes the gate at exactly RenewDeadline after the
// first failed attempt, cancels in-flight writes, fires OnStoppedLeading once,
// keeps Run and the OnStartedLeading context alive; a later successful renewal
// reopens the gate without a second OnStartedLeading.
func TestRecoveryScenario_LossClosesGateAndRenewalReopensIt(t *testing.T) {
	enableRecovery(t)
	runScenario(t, func(s *scenario) {
		a := s.newElector("a", recoveryCfg(0))
		// Observe the gate from inside OnStoppedLeading: it must already be closed.
		var hookMu sync.Mutex
		var hookErr error
		var hookGateOpen bool
		var hookLeadingCtxErr []error
		a.rec.onStopped = func() {
			ctx, cancel := context.WithTimeout(s.ctx, msec)
			defer cancel()
			err := a.probe.do(ctx, http.MethodPut)
			hookMu.Lock()
			defer hookMu.Unlock()
			hookErr = err
			hookGateOpen = a.le.gate.isOpen()
			hookLeadingCtxErr = append(hookLeadingCtxErr, a.rec.leadingCtx(t).Err())
		}
		s.start(a)

		s.at(outageAt)
		a.lock.setMode(modeBlackhole)
		w := a.probe.startWrite(s.ctx)
		if rej, err := w.rejected(); rej {
			t.Fatalf("write at t=5s rejected: %v", err)
		}

		s.at(lossAt - msec)
		a.expectRunning(s.t0, "just before loss")
		a.expectCounts(s.t0, 1, 0, "just before loss")
		a.expectGateOpen(s.ctx, s.t0, "just before loss")

		s.at(lossAt)
		a.expectCounts(s.t0, 1, 1, "at loss")
		s.expectTime(a.rec.stoppedTimes()[0], lossAt, "OnStoppedLeading")
		if err := w.wait(); !errors.Is(err, ErrWriteGateClosed) {
			t.Errorf("in-flight write at loss: err = %v, want ErrWriteGateClosed", err)
		}
		a.expectGateClosed(s.ctx, s.t0, "at loss")
		s.expectLeadingCtxLive(a, "at loss")
		a.expectRunning(s.t0, "at loss")
		hookMu.Lock()
		if !errors.Is(hookErr, ErrWriteGateClosed) || hookGateOpen {
			t.Errorf("inside OnStoppedLeading: write err = %v, gate open = %v; want gate closed before the callback", hookErr, hookGateOpen)
		}
		if len(hookLeadingCtxErr) != 1 || hookLeadingCtxErr[0] != nil {
			t.Errorf("inside OnStoppedLeading at loss: OnStartedLeading context errors = %v, want one live context", hookLeadingCtxErr)
		}
		hookMu.Unlock()
		if got := lastEvent(a); got != "stopped leading" {
			t.Errorf("last event at loss = %q, want stopped leading", got)
		}
		if err := a.le.Check(tolerance); err != nil {
			t.Errorf("Check while inactive with attempts continuing: %v", err)
		}

		// Outage ends at 20s. The fast-path Update started at 16s is blackholed
		// until its 5s client timeout (21s); the slow path then succeeds at 21s.
		s.at(20 * sec)
		a.lock.setMode(modeOK)
		s.at(21*sec - msec)
		a.expectGateClosed(s.ctx, s.t0, "just before recovery")
		s.at(21 * sec)
		a.expectGateOpen(s.ctx, s.t0, "recovered")
		a.expectCounts(s.t0, 1, 1, "recovered")
		s.expectLeadingCtxLive(a, "recovered")
		a.expectRunning(s.t0, "recovered")
		if got := lastEvent(a); got != "became leader" {
			t.Errorf("last event after recovery = %q, want became leader", got)
		}
		if at, ok := s.server.lastCommitBy("a", s.t0.Add(21*sec)); !ok || at.Sub(s.t0) != 21*sec {
			t.Errorf("expected a commit by a at 21s, got %v %v", at.Sub(s.t0), ok)
		}
		// leader_election_master_status follows the gate: on at 0s, off at 16s, on at 21s.
		if got := a.metrics.sequence(); fmt.Sprint(got) != fmt.Sprint([]string{"on@0s", "off@16s", "on@21s"}) {
			t.Errorf("metrics sequence = %v", got)
		}

		// Renewals resume at RetryPeriod.
		s.at(30 * sec)
		renewals := a.lock.opsBetween(s.t0.Add(22*sec), s.t0.Add(30*sec))
		if len(renewals) < 4 {
			t.Errorf("expected renewals to resume after recovery, got %v", renewals)
		}
		for _, op := range renewals {
			if op.op != "update" || op.err != nil {
				t.Errorf("unexpected op after recovery: %v", op)
			}
		}

		// Cancelling while leading closes the gate and fires OnStoppedLeading again.
		w2 := a.probe.startWrite(s.ctx)
		s.cancel()
		settle()
		s.expectReturnedAt(a, 30*sec, "after cancel")
		a.expectCounts(s.t0, 1, 2, "after cancel")
		if err := w2.wait(); !errors.Is(err, ErrWriteGateClosed) {
			t.Errorf("in-flight write at cancel: err = %v", err)
		}
		a.expectGateClosed(s.ctx, s.t0, "after cancel")
		s.expectLeadingCtxCancelled(a, "after cancel")
		hookMu.Lock()
		// At cancellation the OnStartedLeading context is cancelled before
		// OnStoppedLeading runs, as it always has been.
		if len(hookLeadingCtxErr) != 2 || hookLeadingCtxErr[1] == nil {
			t.Errorf("inside OnStoppedLeading at cancel: OnStartedLeading context errors = %v, want the second cancelled", hookLeadingCtxErr)
		}
		hookMu.Unlock()
	})
}

// S4: several loss/recovery cycles; OnStoppedLeading fires once per loss and
// OnStartedLeading never again.
func TestRecoveryScenario_RepeatedLossAndRecovery(t *testing.T) {
	enableRecovery(t)
	for _, release := range []bool{false, true} {
		t.Run(fmt.Sprintf("releaseOnCancel=%v", release), func(t *testing.T) {
			testRepeatedLossAndRecovery(t, release)
		})
	}
}

func testRepeatedLossAndRecovery(t *testing.T, release bool) {
	runScenario(t, func(s *scenario) {
		cfg := recoveryCfg(0)
		cfg.ReleaseOnCancel = release
		a := s.elector("a", cfg)
		for cycle := 1; cycle <= 3; cycle++ {
			outage := s.now() + 5*sec
			s.at(outage)
			a.lock.setMode(modeError)
			// The last successful renewal before the outage fixes the loss time.
			var lastSuccess time.Time
			for _, op := range a.lock.opLog() {
				if op.err == nil && !op.at.After(s.t0.Add(outage)) {
					lastSuccess = op.at
				}
			}
			loss := lastSuccess.Sub(s.t0) + 2*sec + 10*sec
			s.at(loss - msec)
			a.expectCounts(s.t0, 1, cycle-1, fmt.Sprintf("cycle %d before loss", cycle))
			a.expectGateOpen(s.ctx, s.t0, fmt.Sprintf("cycle %d before loss", cycle))
			s.at(loss)
			a.expectCounts(s.t0, 1, cycle, fmt.Sprintf("cycle %d at loss", cycle))
			a.expectGateClosed(s.ctx, s.t0, fmt.Sprintf("cycle %d at loss", cycle))
			a.expectRunning(s.t0, fmt.Sprintf("cycle %d at loss", cycle))
			// A loss is not an exit: the lease is never released for it.
			if n := s.server.releaseCommitsBy("a"); n != 0 {
				t.Errorf("cycle %d: lease released %d times at a loss", cycle, n)
			}
			if got := s.server.holder(); got != "a" {
				t.Errorf("cycle %d: holder = %q at loss, want a", cycle, got)
			}

			s.at(loss + 5*sec)
			a.lock.setMode(modeOK)
			// The next attempt starts within maxGap of the restore.
			s.at(loss + 5*sec + maxGap + jitterSlop)
			a.expectGateOpen(s.ctx, s.t0, fmt.Sprintf("cycle %d recovered", cycle))
			a.expectCounts(s.t0, 1, cycle, fmt.Sprintf("cycle %d recovered", cycle))
			s.expectLeadingCtxLive(a, fmt.Sprintf("cycle %d recovered", cycle))
		}
		// The gauge alternates with the gate: on, then (off, on) per cycle.
		seq := a.metrics.sequence()
		if len(seq) != 7 {
			t.Fatalf("metrics sequence = %v, want 7 transitions", seq)
		}
		for i, ev := range seq {
			want := "on"
			if i%2 == 1 {
				want = "off"
			}
			if ev[:len(want)] != want {
				t.Errorf("metrics sequence[%d] = %s, want %s", i, ev, want)
			}
		}
	})
}

// S5: the recovery deadline, measured from the loss, ends Run.
func TestRecoveryScenario_RecoveryDeadlineEndsRun(t *testing.T) {
	enableRecovery(t)
	for _, release := range []bool{false, true} {
		t.Run(fmt.Sprintf("releaseOnCancel=%v", release), func(t *testing.T) {
			runScenario(t, func(s *scenario) {
				cfg := recoveryCfg(30 * sec)
				cfg.ReleaseOnCancel = release
				a := s.elector("a", cfg)
				s.at(outageAt)
				a.lock.setMode(modeBlackhole)
				s.at(lossAt)
				a.expectCounts(s.t0, 1, 1, "at loss")

				deadline := lossAt + 30*sec
				s.at(deadline - msec)
				a.expectRunning(s.t0, "just before deadline")
				s.expectLeadingCtxLive(a, "just before deadline")
				s.at(deadline)
				if release {
					// The release attempt runs against the blackholed server and
					// gives up after the client timeout.
					a.expectRunning(s.t0, "release in progress")
					s.at(deadline + 5*sec)
					s.expectReturnedAt(a, deadline+5*sec, "after release attempt")
					// The release Get starts at the deadline and hangs for the
					// client timeout. (A recovery attempt whose timer fires at
					// the same instant fails immediately on the expired context
					// and may also appear.)
					var releaseGets int
					for _, op := range a.lock.opsBetween(s.t0.Add(deadline), s.t0.Add(deadline)) {
						if op.op == "get" && op.done.Sub(s.t0) == deadline+5*sec {
							releaseGets++
						}
					}
					if releaseGets != 1 {
						t.Errorf("expected one release Get spanning the client timeout at the deadline, got %v", a.lock.opsBetween(s.t0.Add(deadline), s.t0.Add(deadline)))
					}
				} else {
					s.expectReturnedAt(a, deadline, "at deadline")
				}
				a.expectCounts(s.t0, 1, 1, "after return")
				a.expectGateClosed(s.ctx, s.t0, "after return")
				s.expectLeadingCtxCancelled(a, "after return")
				if got := a.metrics.sequence(); fmt.Sprint(got) != fmt.Sprint([]string{"on@0s", "off@16s"}) {
					t.Errorf("metrics sequence = %v", got)
				}
			})
		})
	}
}

// S6: observing another holder ends Run.
func TestRecoveryScenario_AnotherHolderEndsRun(t *testing.T) {
	enableRecovery(t)
	runScenario(t, func(s *scenario) {
		a := s.elector("a", recoveryCfg(0))
		s.at(outageAt)
		a.lock.setMode(modeBlackhole)
		s.at(lossAt)
		a.expectCounts(s.t0, 1, 1, "at loss")

		s.at(25 * sec)
		s.server.forceHolder("b", 15*sec)
		s.at(30*sec - msec)
		a.expectRunning(s.t0, "cannot observe b while partitioned")
		a.lock.setMode(modeOK)
		// The first attempt after the partition heals observes b: either the
		// one started in [28s, 30.4s) (blackholed until its 5s client timeout
		// if it started before 30s) or the next one, within maxGap.
		s.at(30*sec + 5*sec + maxGap + jitterSlop)
		a.expectReturned(s.t0, "after observing b")
		at, _ := a.returnedAt()
		s.expectTimeWithin(at, 30*sec, 30*sec+5*sec+maxGap+jitterSlop, "Run returned")
		leaders := a.rec.leaders()
		if len(leaders) == 0 || leaders[len(leaders)-1].id != "b" {
			t.Errorf("OnNewLeader observations = %v, want last b", leaders)
		}
		a.expectCounts(s.t0, 1, 1, "after return")
		a.expectGateClosed(s.ctx, s.t0, "after return")
		s.expectLeadingCtxCancelled(a, "after return")
		if got := s.server.holder(); got != "b" {
			t.Errorf("holder = %q, want b (a must not overwrite)", got)
		}
		if at, ok := s.server.lastCommitBy("a", s.t0.Add(60*sec)); ok && at.After(s.t0.Add(25*sec)) {
			t.Errorf("a committed at %v after b took over", at.Sub(s.t0))
		}
	})
}

// S7: cancelling while inactive returns promptly and does not fire
// OnStoppedLeading a second time.
func TestRecoveryScenario_CancelWhileInactive(t *testing.T) {
	enableRecovery(t)
	for _, release := range []bool{false, true} {
		t.Run(fmt.Sprintf("releaseOnCancel=%v", release), func(t *testing.T) {
			runScenario(t, func(s *scenario) {
				cfg := recoveryCfg(0)
				cfg.ReleaseOnCancel = release
				a := s.elector("a", cfg)
				s.at(outageAt)
				a.lock.setMode(modeBlackhole)
				s.at(lossAt)
				a.expectCounts(s.t0, 1, 1, "at loss")
				s.at(20 * sec)
				s.cancel()
				settle()
				if release {
					a.expectRunning(s.t0, "release in progress")
					s.at(25 * sec)
					s.expectReturnedAt(a, 25*sec, "after release attempt")
				} else {
					s.expectReturnedAt(a, 20*sec, "after cancel")
				}
				a.expectCounts(s.t0, 1, 1, "after cancel")
				a.expectGateClosed(s.ctx, s.t0, "after cancel")
				s.expectLeadingCtxCancelled(a, "after cancel")
			})
		})
	}
}

// S8: cancelling while leading closes the gate (cancelling in-flight writes)
// and fires OnStoppedLeading once.
func TestRecoveryScenario_CancelWhileLeading(t *testing.T) {
	enableRecovery(t)
	for _, release := range []bool{false, true} {
		t.Run(fmt.Sprintf("releaseOnCancel=%v", release), func(t *testing.T) {
			testCancelWhileLeading(t, release)
		})
	}
}

func testCancelWhileLeading(t *testing.T, release bool) {
	runScenario(t, func(s *scenario) {
		cfg := recoveryCfg(0)
		cfg.ReleaseOnCancel = release
		a := s.newElector("a", cfg)
		var ctxErrInHook error
		var hookCalled bool
		var holderInHook string
		a.rec.onStopped = func() {
			hookCalled = true
			ctxErrInHook = a.rec.leadingCtx(t).Err()
			holderInHook = s.server.holder()
		}
		s.start(a)
		s.at(outageAt)
		w := a.probe.startWrite(s.ctx)
		s.cancel()
		settle()
		s.expectReturnedAt(a, outageAt, "after cancel")
		if !hookCalled || ctxErrInHook == nil {
			t.Errorf("OnStoppedLeading at cancel: called=%v, OnStartedLeading ctx err=%v; want the context cancelled first", hookCalled, ctxErrInHook)
		}
		// At shutdown the lease is released before OnStoppedLeading runs, as it
		// always has been, so a callback that exits the process cannot
		// pre-empt the release.
		if release && holderInHook != "" {
			t.Errorf("OnStoppedLeading ran before the lease was released (holder %q)", holderInHook)
		}
		if !release && holderInHook != "a" {
			t.Errorf("lease released without ReleaseOnCancel (holder %q)", holderInHook)
		}
		if err := w.wait(); !errors.Is(err, ErrWriteGateClosed) {
			t.Errorf("in-flight write: err = %v, want ErrWriteGateClosed", err)
		}
		a.expectCounts(s.t0, 1, 1, "after cancel")
		a.expectGateClosed(s.ctx, s.t0, "after cancel")
		s.expectLeadingCtxCancelled(a, "after cancel")
	})
}

// S9: with the feature gate disabled, Recovery is ignored and WriteGate is a
// passthrough: behaviour is exactly today's.
func TestRecoveryScenario_FeatureDisabled(t *testing.T) {
	clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.LeaderElectionRecovery, false)
	runScenario(t, func(s *scenario) {
		a := s.newElector("a", recoveryCfg(30*sec))
		if err := a.probe.write(s.ctx); err != nil {
			t.Errorf("feature disabled: write before leading must pass through, got %v", err)
		}
		s.start(a)
		s.at(outageAt)
		a.lock.setMode(modeBlackhole)
		s.at(lossAt - msec)
		a.expectRunning(s.t0, "before loss")
		s.at(lossAt)
		s.expectReturnedAt(a, lossAt, "at loss")
		a.expectCounts(s.t0, 1, 1, "at loss")
		s.expectLeadingCtxCancelled(a, "at loss")
		if err := a.probe.write(s.ctx); err != nil {
			t.Errorf("feature disabled: write after Run returned must pass through, got %v", err)
		}
	})
}

// S10: with the feature gate enabled but no Recovery, the loop behaves as
// today while the write gate tracks leadership.
func TestRecoveryScenario_GateWithoutRecovery(t *testing.T) {
	enableRecovery(t)
	for _, release := range []bool{false, true} {
		t.Run(fmt.Sprintf("releaseOnCancel=%v", release), func(t *testing.T) {
			runScenario(t, func(s *scenario) {
				cfg := standardTimers()
				cfg.ReleaseOnCancel = release
				a := s.newElector("a", cfg)
				a.expectGateClosed(s.ctx, s.t0, "before Run")
				var hookGateOpen bool
				var hookCtxErr error
				var hookLastOp lockOp
				a.rec.onStopped = func() {
					hookGateOpen = a.le.gate.isOpen()
					hookCtxErr = a.rec.leadingCtx(t).Err()
					ops := a.lock.opLog()
					hookLastOp = ops[len(ops)-1]
				}
				s.start(a)
				a.expectGateOpen(s.ctx, s.t0, "leading")
				s.at(outageAt)
				a.lock.setMode(modeBlackhole)
				w := a.probe.startWrite(s.ctx)
				s.at(lossAt)
				if release {
					// Today: release is attempted after a failed renewal too.
					a.expectRunning(s.t0, "release in progress")
					s.at(lossAt + 5*sec)
					s.expectReturnedAt(a, lossAt+5*sec, "after release attempt")
				} else {
					s.expectReturnedAt(a, lossAt, "at loss")
				}
				a.expectCounts(s.t0, 1, 1, "after return")
				if err := w.wait(); !errors.Is(err, ErrWriteGateClosed) {
					t.Errorf("in-flight write at loss: err = %v", err)
				}
				a.expectGateClosed(s.ctx, s.t0, "after return")
				if hookGateOpen {
					t.Error("gate was open inside OnStoppedLeading")
				}
				if hookCtxErr == nil {
					t.Error("legacy mode: OnStartedLeading context must be cancelled before OnStoppedLeading runs")
				}
				// Today the release attempt (a Get that times out against the
				// blackholed server) completes before OnStoppedLeading runs.
				if release && (hookLastOp.op != "get" || hookLastOp.at.Sub(s.t0) != lossAt || hookLastOp.done.Sub(s.t0) != lossAt+5*sec) {
					t.Errorf("legacy mode: OnStoppedLeading ran before the release attempt finished; last op at callback time: %v", hookLastOp)
				}
				s.expectLeadingCtxCancelled(a, "after return")
			})
		})
	}
}

// S11: a renewal that commits but whose response is lost still leads to loss
// at RenewDeadline, and recovery proceeds through the slow path without a
// spurious leader change.
func TestRecoveryScenario_UncertainCommit(t *testing.T) {
	enableRecovery(t)
	runScenario(t, func(s *scenario) {
		a := s.elector("a", recoveryCfg(0))
		s.at(outageAt)
		a.lock.setMode(modeUncertain)
		s.at(lossAt)
		a.expectCounts(s.t0, 1, 1, "at loss")
		if at, ok := s.server.lastCommitBy("a", s.t0.Add(lossAt)); !ok || at.Sub(s.t0) != 6*sec {
			t.Errorf("expected the uncertain renewal to have committed at 6s, got %v %v", at.Sub(s.t0), ok)
		}
		s.at(20 * sec)
		a.lock.setMode(modeOK)
		s.at(21*sec - msec)
		a.expectGateClosed(s.ctx, s.t0, "before recovery")
		s.at(21 * sec)
		a.expectGateOpen(s.ctx, s.t0, "recovered via slow path")
		a.expectCounts(s.t0, 1, 1, "recovered")
		for _, l := range a.rec.leaders() {
			if l.id != "a" {
				t.Errorf("unexpected OnNewLeader(%q) at %v", l.id, l.at.Sub(s.t0))
			}
		}
	})
}

// S12: a holder whose requests commit but always time out keeps the gate
// closed and exits at the recovery deadline.
func TestRecoveryScenario_SlowCommitsExitAtDeadline(t *testing.T) {
	enableRecovery(t)
	runScenario(t, func(s *scenario) {
		a := s.elector("a", recoveryCfg(30*sec))
		s.at(outageAt)
		a.lock.setLatency(6*sec, 6*sec) // client timeout is 5s: every response is lost
		s.at(lossAt)
		a.expectCounts(s.t0, 1, 1, "at loss")
		// The renewal issued at 6s committed at 9s (halfway through the latency).
		if at, ok := s.server.lastCommitBy("a", s.t0.Add(lossAt)); !ok || at.Sub(s.t0) != 9*sec {
			t.Errorf("expected a commit at 9s, got %v %v", at.Sub(s.t0), ok)
		}
		deadline := lossAt + 30*sec
		s.at(deadline - msec)
		a.expectRunning(s.t0, "before deadline")
		a.expectGateClosed(s.ctx, s.t0, "before deadline")
		s.at(deadline)
		s.expectReturnedAt(a, deadline, "at deadline")
		a.expectCounts(s.t0, 1, 1, "at deadline")
		s.expectLeadingCtxCancelled(a, "at deadline")
	})
}

// S13: a renewal that succeeds after the context was cancelled must not
// reopen the gate.
func TestRecoveryScenario_StaleSuccessAfterCancel(t *testing.T) {
	enableRecovery(t)
	runScenario(t, func(s *scenario) {
		a := s.elector("a", recoveryCfg(0))
		s.at(outageAt)
		a.lock.setMode(modeBlackhole)
		s.at(lossAt)
		a.expectCounts(s.t0, 1, 1, "at loss")
		// First inactive attempt: Update blackholed until 21s, Get until 26s.
		// Its retry timer has long fired, so the next attempt starts at 26s.
		// From then on the client ignores cancellation and answers after 4s:
		// Get 26s->30s, Update 30s->34s (applied at 32s).
		s.at(26*sec - msec)
		a.lock.setMode(modeOK)
		a.lock.setLatency(4*sec, 4*sec)
		a.lock.setIgnoreCtx(true)
		s.at(33 * sec)
		s.cancel()
		settle()
		a.expectRunning(s.t0, "attempt ignoring cancellation still in flight")
		s.at(34 * sec)
		s.expectReturnedAt(a, 34*sec, "after the stale success")
		if at, ok := s.server.lastCommitBy("a", s.t0.Add(34*sec)); !ok || at.Sub(s.t0) != 32*sec {
			t.Errorf("expected the stale renewal to have committed at 32s, got %v %v", at.Sub(s.t0), ok)
		}
		a.expectCounts(s.t0, 1, 1, "stale success must not count as leading again")
		a.expectGateClosed(s.ctx, s.t0, "after return")
		for _, ev := range a.lock.eventLog()[1:] {
			if ev == "became leader" {
				t.Error("recorded a 'became leader' event for a stale success")
			}
		}
	})
}

// S14: health check semantics in recovery mode.
func TestRecoveryScenario_HealthCheck(t *testing.T) {
	enableRecovery(t)
	t.Run("inactive holder is healthy while attempts continue, unhealthy when stuck", func(t *testing.T) {
		runScenario(t, func(s *scenario) {
			cfg := recoveryCfg(0)
			cfg.WatchDog = NewLeaderHealthzAdaptor(tolerance)
			a := s.elector("a", cfg)
			check := func() error { return cfg.WatchDog.Check(nil) }
			s.at(3 * sec)
			if err := check(); err != nil {
				t.Errorf("leading: %v", err)
			}
			s.at(outageAt)
			a.lock.setMode(modeBlackhole)
			s.at(lossAt)
			if err := check(); err != nil {
				t.Errorf("at loss: %v", err)
			}
			s.at(90 * sec)
			if err := check(); err != nil {
				t.Errorf("inactive at 90s with attempts continuing: %v", err)
			}
			if n := len(a.lock.opsBetween(s.t0.Add(lossAt), s.t0.Add(90*sec))); n < 5 {
				t.Errorf("expected attempts to continue while inactive, got %d ops", n)
			}
			// Stick the loop: the next request never returns.
			a.lock.setIgnoreCtx(true)
			s.at(110 * sec)
			// The stuck request is not recorded (it never returns); it started
			// within maxGap after the last recorded op ended.
			ops := a.lock.opLog()
			last := ops[len(ops)-1]
			stuckStart := last.done.Sub(s.t0)
			s.at(stuckStart + maxGap + 15*sec + tolerance + jitterSlop)
			if err := check(); err == nil {
				t.Errorf("stuck loop at %v: expected unhealthy", s.now())
			}
		})
	})
	t.Run("tight timers, zero tolerance: never unhealthy while attempting", func(t *testing.T) {
		// LeaseDuration 15s, RenewDeadline 14s, RetryPeriod 2s is valid. A
		// blackholed inactive attempt takes up to one 7s client timeout here
		// (the lease is locally expired by the loss, so there is no fast-path
		// Update), and up to RenewDeadline in general; attempts are scheduled
		// from their start, so the gap between attempt starts never exceeds
		// max(RenewDeadline, RetryPeriod*(1+JitterFactor)) < LeaseDuration.
		runScenario(t, func(s *scenario) {
			cfg := harnessConfig{LeaseDuration: 15 * sec, RenewDeadline: 14 * sec, RetryPeriod: 2 * sec, Recovery: &RecoveryConfig{}}
			cfg.WatchDog = NewLeaderHealthzAdaptor(0)
			a := s.elector("a", cfg)
			s.at(outageAt)
			a.lock.setMode(modeBlackhole)
			// Loss at 4s + 2s + 14s = 20s.
			s.at(20 * sec)
			a.expectCounts(s.t0, 1, 1, "at loss")
			for now := 20 * sec; now <= 120*sec; now += sec {
				s.at(now)
				if err := cfg.WatchDog.Check(nil); err != nil {
					t.Errorf("t=%v: healthy loop reported unhealthy: %v; ops=%v", now, err, a.lock.opLog())
					break
				}
			}
			if n := len(a.lock.opsBetween(s.t0.Add(20*sec), s.t0.Add(120*sec))); n < 6 {
				t.Errorf("expected attempts to continue, got %d ops", n)
			}
		})
	})
	t.Run("leading holder with a stuck loop is unhealthy after LeaseDuration plus tolerance", func(t *testing.T) {
		runScenario(t, func(s *scenario) {
			cfg := recoveryCfg(0)
			cfg.WatchDog = NewLeaderHealthzAdaptor(tolerance)
			a := s.elector("a", cfg)
			check := func() error { return cfg.WatchDog.Check(nil) }
			s.at(outageAt)
			a.lock.setIgnoreCtx(true)
			a.lock.setMode(modeBlackhole)
			// The renewal at 6s hangs forever; the gate stays open because the
			// loop never gets to close it. Last observation was at 4s.
			s.at(4*sec + 15*sec + tolerance - msec)
			if err := check(); err != nil {
				t.Errorf("just before threshold: %v", err)
			}
			a.expectGateOpen(s.ctx, s.t0, "stuck loop leaves gate open")
			s.at(4*sec + 15*sec + tolerance + msec)
			if err := check(); err == nil {
				t.Error("stuck leading loop: expected unhealthy")
			}
		})
	})
}

// S15: recovery through the fast path (lease still valid locally) and the
// slow path (expired) both reopen the gate.
func TestRecoveryScenario_FastAndSlowPathRecovery(t *testing.T) {
	enableRecovery(t)
	t.Run("fast path", func(t *testing.T) {
		runScenario(t, func(s *scenario) {
			// A 4s client timeout keeps the last blackholed request from ending
			// at the same instant as the renew deadline, so the loss at 16s is
			// caused by the deadline alone (see the timeline note above).
			cfg := recoveryCfg(0)
			cfg.clientTimeout = 4 * sec
			a := s.elector("a", cfg)
			s.at(outageAt)
			a.lock.setMode(modeBlackhole)
			// Connectivity returns just before the loss. The lease observed at
			// 4s is valid locally until 19s, so the immediate recovery attempt
			// at 16s takes the fast path and the gate reopens at the same
			// instant it closed.
			s.at(15 * sec)
			a.lock.setMode(modeOK)
			s.at(lossAt)
			a.expectCounts(s.t0, 1, 1, "at loss")
			a.expectGateOpen(s.t0Ctx(), s.t0, "recovered immediately")
			// At 16s the failing renew attempt ends (its slow-path Get fails on
			// the expired poll context), then the recovery attempt's fast-path
			// Update succeeds, then the renew loop's first renewal follows.
			var successes []lockOp
			for _, op := range a.lock.opsBetween(s.t0.Add(lossAt), s.t0.Add(lossAt)) {
				if op.err == nil {
					successes = append(successes, op)
				}
			}
			if len(successes) != 2 || successes[0].op != "update" || successes[1].op != "update" {
				t.Errorf("expected the recovery to be a fast-path update (no get) followed by a renewal at 16s, got %v", a.lock.opsBetween(s.t0.Add(lossAt), s.t0.Add(lossAt)))
			}
			if got := lastEvent(a); got != "became leader" {
				t.Errorf("last event = %q", got)
			}
		})
	})
	t.Run("slow path", func(t *testing.T) {
		runScenario(t, func(s *scenario) {
			a := s.elector("a", recoveryCfg(0))
			s.at(outageAt)
			a.lock.setMode(modeError)
			s.at(lossAt)
			s.at(25 * sec)
			a.lock.setMode(modeOK)
			s.at(25*sec + maxGap + jitterSlop)
			a.expectGateOpen(s.ctx, s.t0, "recovered")
			ops := a.lock.opsBetween(s.t0.Add(25*sec), s.t0.Add(25*sec+maxGap+jitterSlop))
			if len(ops) < 2 || ops[0].op != "get" || ops[0].err != nil || ops[1].op != "update" || ops[1].err != nil {
				t.Errorf("expected get then update, got %v", ops)
			}
		})
	})
}

// S16: two electors sharing one lease.
func TestRecoveryScenario_TwoElectors(t *testing.T) {
	enableRecovery(t)
	t.Run("partition heals before takeover: holder recovers, standby never leads", func(t *testing.T) {
		runScenario(t, func(s *scenario) {
			cfg := recoveryCfg(0)
			cfg.clientTimeout = 4 * sec // see the fast path scenario
			a := s.elector("a", cfg)
			s.at(sec)
			b := s.elector("b", recoveryCfg(0))
			s.monitorExclusion(a, b)
			s.at(outageAt)
			a.lock.setMode(modeBlackhole)
			s.at(15 * sec)
			a.lock.setMode(modeOK)
			s.at(lossAt)
			// Loss and immediate fast-path recovery happen at the same instant.
			a.expectCounts(s.t0, 1, 1, "at loss")
			a.expectGateOpen(s.ctx, s.t0, "recovered immediately")
			s.at(60 * sec)
			a.expectGateOpen(s.ctx, s.t0, "still leading")
			a.expectRunning(s.t0, "still leading")
			b.expectCounts(s.t0, 0, 0, "standby never led")
			b.expectGateClosed(s.ctx, s.t0, "standby")
			b.expectRunning(s.t0, "standby")
		})
	})
	t.Run("partition outlasts takeover: standby leads, holder exits on observing it", func(t *testing.T) {
		runScenario(t, func(s *scenario) {
			a := s.elector("a", recoveryCfg(0))
			s.at(sec)
			b := s.elector("b", recoveryCfg(0))
			s.monitorExclusion(a, b)
			s.at(outageAt)
			a.lock.setMode(modeBlackhole)
			s.at(lossAt)
			a.expectCounts(s.t0, 1, 1, "at loss")
			// b first observed the 4s record at its first attempt after 4s,
			// i.e. in [4s, 9.4s), and may take over LeaseDuration later, at its
			// next attempt: in [19s, 28.8s).
			s.at(19*sec - msec)
			b.expectCounts(s.t0, 0, 0, "standby waiting")
			s.at(4*sec + maxGap + 15*sec + maxGap + jitterSlop)
			b.expectCounts(s.t0, 1, 0, "standby took over")
			b.expectGateOpen(s.ctx, s.t0, "standby leading")
			a.expectGateClosed(s.ctx, s.t0, "former holder")
			s.at(30 * sec)
			a.lock.setMode(modeOK)
			s.at(30*sec + 5*sec + maxGap + jitterSlop)
			a.expectReturned(s.t0, "former holder observed b")
			leaders := a.rec.leaders()
			if len(leaders) == 0 || leaders[len(leaders)-1].id != "b" {
				t.Errorf("a's OnNewLeader observations = %v", leaders)
			}
			a.expectCounts(s.t0, 1, 1, "former holder")
			if got := s.server.holder(); got != "b" {
				t.Errorf("holder = %q", got)
			}
		})
	})
}

// S17: a zero recovery deadline means no limit.
func TestRecoveryScenario_UnlimitedDeadline(t *testing.T) {
	enableRecovery(t)
	runScenario(t, func(s *scenario) {
		a := s.elector("a", recoveryCfg(0))
		s.at(outageAt)
		a.lock.setMode(modeError)
		s.at(lossAt)
		a.expectCounts(s.t0, 1, 1, "at loss")
		s.at(10 * time.Minute)
		a.expectRunning(s.t0, "long outage")
		a.expectGateClosed(s.ctx, s.t0, "long outage")
		a.lock.setMode(modeOK)
		s.at(10*time.Minute + maxGap + jitterSlop)
		a.expectGateOpen(s.ctx, s.t0, "recovered after a long outage")
		a.expectCounts(s.t0, 1, 1, "recovered")
	})
}

// S22: a lock client without its own timeout. The renew loop bounds its
// attempts with the poll deadline; the inactive loop must bound each attempt
// too, or a blackholed request would hang forever and recovery would never
// happen once connectivity returns.
func TestRecoveryScenario_RecoveryWithoutClientTimeout(t *testing.T) {
	enableRecovery(t)
	runScenario(t, func(s *scenario) {
		cfg := recoveryCfg(0)
		cfg.disableClientTmo = true
		a := s.elector("a", cfg)
		s.at(outageAt)
		a.lock.setMode(modeBlackhole)
		s.at(lossAt)
		a.expectCounts(s.t0, 1, 1, "at loss")
		// Inactive attempts: [16s, 26s) hangs on the fast-path Update and is
		// cut off by the attempt bound; the next starts at 26s and hangs on
		// the Get until 36s; and so on.
		s.at(30 * sec)
		a.lock.setMode(modeOK)
		a.expectGateClosed(s.ctx, s.t0, "request from before the restore still hanging")
		s.at(36*sec + maxGap + jitterSlop)
		a.expectGateOpen(s.ctx, s.t0, "recovered once the hung attempt was cut off")
		a.expectCounts(s.t0, 1, 1, "recovered")
		if err := a.le.Check(tolerance); err != nil {
			t.Errorf("Check after recovery: %v", err)
		}
	})
}

// S21: the Lease is deleted while inactive. Recovery only renews; it never
// recreates or re-acquires, so Run returns and the lease stays absent.
func TestRecoveryScenario_LeaseDeletedWhileInactive(t *testing.T) {
	enableRecovery(t)
	for _, release := range []bool{false, true} {
		t.Run(fmt.Sprintf("releaseOnCancel=%v", release), func(t *testing.T) {
			runScenario(t, func(s *scenario) {
				cfg := recoveryCfg(0)
				cfg.ReleaseOnCancel = release
				a := s.elector("a", cfg)
				s.at(outageAt)
				a.lock.setMode(modeError)
				s.at(lossAt)
				a.expectCounts(s.t0, 1, 1, "at loss")
				s.at(20 * sec)
				s.server.delete()
				a.lock.setMode(modeOK)
				s.at(20*sec + maxGap + jitterSlop)
				a.expectReturned(s.t0, "gave up on a deleted lease")
				a.expectGateClosed(s.ctx, s.t0, "gave up")
				a.expectCounts(s.t0, 1, 1, "gave up")
				s.expectLeadingCtxCancelled(a, "gave up")
				if s.server.holder() != "" {
					t.Errorf("lease was recreated, holder = %q", s.server.holder())
				}
				for _, op := range a.lock.opsBetween(s.t0.Add(20*sec), s.t0.Add(60*sec)) {
					if op.op == "create" || (op.op == "update" && op.err == nil) {
						t.Errorf("unexpected write after the lease was deleted: %v", op)
					}
				}
			})
		})
	}
}

// S23: another candidate took the lease and released it again while this
// elector was inactive. The lease changed hands, so the elector must not take
// it back: Run returns once the change is observed.
func TestRecoveryScenario_LeaseChangedHandsAndReleased(t *testing.T) {
	enableRecovery(t)
	runScenario(t, func(s *scenario) {
		a := s.elector("a", recoveryCfg(0))
		s.at(outageAt)
		a.lock.setMode(modeError)
		s.at(lossAt)
		a.expectCounts(s.t0, 1, 1, "at loss")
		s.at(25 * sec)
		s.server.forceHolder("b", 15*sec)
		s.at(28 * sec)
		s.server.release("b")
		s.at(30 * sec)
		a.lock.setMode(modeOK)
		s.at(30*sec + maxGap + jitterSlop)
		a.expectReturned(s.t0, "observed that the lease changed hands")
		a.expectGateClosed(s.ctx, s.t0, "gave up")
		a.expectCounts(s.t0, 1, 1, "gave up")
		if at, ok := s.server.lastCommitBy("a", s.t0.Add(60*sec)); ok && at.After(s.t0.Add(lossAt)) {
			t.Errorf("a wrote the lease at %v after losing it", at.Sub(s.t0))
		}
		if got := s.server.holder(); got != "" {
			t.Errorf("holder = %q, want the released lease left alone", got)
		}
	})
}

// S24: another candidate took the lease and then stopped renewing it. Even
// though that lease has expired, the recovering elector does not re-acquire
// it; a fresh candidate would have to.
func TestRecoveryScenario_OtherHolderExpired(t *testing.T) {
	enableRecovery(t)
	runScenario(t, func(s *scenario) {
		a := s.elector("a", recoveryCfg(0))
		s.at(outageAt)
		a.lock.setMode(modeError)
		s.at(lossAt)
		s.at(25 * sec)
		s.server.forceHolder("b", 15*sec)
		// b never renews; a first sees b's record at ~30s and would consider
		// it expired 15s later.
		s.at(30 * sec)
		a.lock.setMode(modeOK)
		s.at(30*sec + maxGap + jitterSlop)
		a.expectReturned(s.t0, "observed b")
		leaders := a.rec.leaders()
		if len(leaders) == 0 || leaders[len(leaders)-1].id != "b" {
			t.Errorf("OnNewLeader observations = %v", leaders)
		}
		s.at(60 * sec)
		if got := s.server.holder(); got != "b" {
			t.Errorf("holder = %q, want b's expired lease left alone", got)
		}
	})
}

// S25: the release attempt when Run returns must not clear a lease that
// another candidate holds by then, even though the last observed record still
// names this elector.
func TestRecoveryScenario_ReleaseDoesNotClobberNewHolder(t *testing.T) {
	enableRecovery(t)
	runScenario(t, func(s *scenario) {
		cfg := recoveryCfg(30 * sec)
		cfg.ReleaseOnCancel = true
		a := s.elector("a", cfg)
		s.at(outageAt)
		a.lock.setMode(modeBlackhole)
		s.at(lossAt)
		a.expectCounts(s.t0, 1, 1, "at loss")
		s.at(25 * sec)
		s.server.forceHolder("b", 15*sec)
		deadline := lossAt + 30*sec
		// Connectivity returns just before the deadline. The attempt in flight
		// is still blackholed and is cut off by the deadline; the release
		// attempt that follows can reach the server and sees b.
		s.at(deadline - msec)
		a.lock.setMode(modeOK)
		if !a.le.IsLeader() {
			t.Fatal("precondition: a still believes it is the last observed holder")
		}
		s.at(deadline)
		s.expectReturnedAt(a, deadline, "at deadline")
		if got := s.server.holder(); got != "b" {
			t.Errorf("holder = %q after a's release attempt, want b", got)
		}
		if n := s.server.releaseCommitsBy("a"); n != 0 {
			t.Errorf("a cleared the lease %d times", n)
		}
		if at, ok := s.server.lastCommitBy("a", s.t0.Add(deadline)); ok && at.After(s.t0.Add(lossAt)) {
			t.Errorf("a wrote the lease at %v after losing it", at.Sub(s.t0))
		}
	})
}

// S5b: a slow OnStoppedLeading callback does not push out the recovery
// deadline, which is anchored at the loss.
func TestRecoveryScenario_DeadlineAnchoredAtLoss(t *testing.T) {
	enableRecovery(t)
	runScenario(t, func(s *scenario) {
		a := s.newElector("a", recoveryCfg(30*sec))
		a.rec.onStopped = func() { time.Sleep(10 * sec) }
		s.start(a)
		s.at(outageAt)
		a.lock.setMode(modeBlackhole)
		deadline := lossAt + 30*sec
		s.at(deadline - msec)
		a.expectRunning(s.t0, "before deadline")
		s.at(deadline)
		s.expectReturnedAt(a, deadline, "at deadline")
	})
}

// S19: the gate stays closed while another holder's lease is valid, and opens
// once this elector acquires.
func TestRecoveryScenario_GateClosedUntilAcquired(t *testing.T) {
	enableRecovery(t)
	runScenario(t, func(s *scenario) {
		s.server.forceHolder("z", 15*sec)
		a := s.elector("a", recoveryCfg(0))
		s.at(15*sec - msec)
		a.expectCounts(s.t0, 0, 0, "waiting on z")
		a.expectGateClosed(s.ctx, s.t0, "waiting on z")
		s.at(15*sec + maxGap + jitterSlop)
		a.expectCounts(s.t0, 1, 0, "acquired")
		a.expectGateOpen(s.ctx, s.t0, "acquired")
	})
}

// S18: configuration validation.
func TestRecoveryConfigValidation(t *testing.T) {
	enableRecovery(t)
	base := func() LeaderElectionConfig {
		return LeaderElectionConfig{
			Lock:          newSimLock("a", newSimLeaseServer(), 0),
			LeaseDuration: 15 * sec,
			RenewDeadline: 10 * sec,
			RetryPeriod:   2 * sec,
			Callbacks:     LeaderCallbacks{OnStartedLeading: func(context.Context) {}, OnStoppedLeading: func() {}},
		}
	}
	cases := []struct {
		name    string
		mutate  func(*LeaderElectionConfig)
		wantErr bool
	}{
		{name: "no recovery", mutate: func(*LeaderElectionConfig) {}},
		{name: "recovery unlimited", mutate: func(c *LeaderElectionConfig) { c.Recovery = &RecoveryConfig{} }},
		{name: "recovery with deadline", mutate: func(c *LeaderElectionConfig) { c.Recovery = &RecoveryConfig{RecoveryDeadline: time.Minute} }},
		{name: "negative deadline", mutate: func(c *LeaderElectionConfig) { c.Recovery = &RecoveryConfig{RecoveryDeadline: -sec} }, wantErr: true},
		{name: "coordinated with recovery", mutate: func(c *LeaderElectionConfig) { c.Recovery = &RecoveryConfig{}; c.Coordinated = true }, wantErr: true},
		{name: "coordinated without recovery", mutate: func(c *LeaderElectionConfig) { c.Coordinated = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			tc.mutate(&cfg)
			_, err := NewLeaderElector(cfg)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
	t.Run("shared write gate rejected", func(t *testing.T) {
		gate := NewWriteGate()
		cfg := base()
		cfg.WriteGate = gate
		if _, err := NewLeaderElector(cfg); err != nil {
			t.Fatalf("first elector: %v", err)
		}
		cfg2 := base()
		cfg2.WriteGate = gate
		if _, err := NewLeaderElector(cfg2); err == nil {
			t.Fatal("second elector with the same WriteGate: expected an error")
		}
	})
}

// With the feature gate disabled, Recovery is ignored entirely, including its
// validation, so a configuration that is invalid with the gate on is accepted.
func TestRecoveryConfigIgnoredWhenFeatureDisabled(t *testing.T) {
	clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.LeaderElectionRecovery, false)
	cfg := LeaderElectionConfig{
		Lock:          newSimLock("a", newSimLeaseServer(), 0),
		LeaseDuration: 15 * sec,
		RenewDeadline: 10 * sec,
		RetryPeriod:   2 * sec,
		Callbacks:     LeaderCallbacks{OnStartedLeading: func(context.Context) {}, OnStoppedLeading: func() {}},
		Recovery:      &RecoveryConfig{RecoveryDeadline: -sec},
		Coordinated:   true,
	}
	if _, err := NewLeaderElector(cfg); err != nil {
		t.Fatalf("feature disabled: Recovery must be ignored, got %v", err)
	}
}

// The feature gate is read when the WriteGate and the elector are
// constructed; flipping it afterwards changes nothing.
func TestRecoveryFeatureGateReadAtConstruction(t *testing.T) {
	for _, enabledAtConstruction := range []bool{true, false} {
		t.Run(fmt.Sprintf("enabledAtConstruction=%v", enabledAtConstruction), func(t *testing.T) {
			clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.LeaderElectionRecovery, enabledAtConstruction)
			gate := NewWriteGate()
			le, err := NewLeaderElector(LeaderElectionConfig{
				Lock:          newSimLock("a", newSimLeaseServer(), 0),
				LeaseDuration: 15 * sec,
				RenewDeadline: 10 * sec,
				RetryPeriod:   2 * sec,
				Callbacks:     LeaderCallbacks{OnStartedLeading: func(context.Context) {}, OnStoppedLeading: func() {}},
				Recovery:      &RecoveryConfig{},
				WriteGate:     gate,
			})
			if err != nil {
				t.Fatal(err)
			}
			clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.LeaderElectionRecovery, !enabledAtConstruction)
			inner := &probeTransport{}
			rt := le.WriteGate()(inner)
			go inner.releaseAllEventually()
			err = doRequest(t, rt, context.Background(), http.MethodPost)
			if enabledAtConstruction {
				if !errors.Is(err, ErrWriteGateClosed) {
					t.Errorf("gate constructed with the feature on must keep gating, got %v", err)
				}
				if le.recovery == nil {
					t.Error("recovery config dropped after the feature was flipped off")
				}
			} else {
				if err != nil {
					t.Errorf("gate constructed with the feature off must keep passing through, got %v", err)
				}
				if le.recovery != nil {
					t.Error("recovery config picked up after the feature was flipped on")
				}
			}
		})
	}
}

// A WriteGate created while the feature gate was disabled is a passthrough;
// an elector created after the feature gate was enabled must refuse it,
// otherwise recovery mode would run with ungated clients. The reverse
// mismatch (gate on, elector off) is harmless: legacy loop, functional gate.
func TestRecoveryWriteGateFeatureMismatch(t *testing.T) {
	newConfig := func(gate *WriteGate) LeaderElectionConfig {
		return LeaderElectionConfig{
			Lock:          newSimLock("a", newSimLeaseServer(), 0),
			LeaseDuration: 15 * sec,
			RenewDeadline: 10 * sec,
			RetryPeriod:   2 * sec,
			Callbacks:     LeaderCallbacks{OnStartedLeading: func(context.Context) {}, OnStoppedLeading: func() {}},
			Recovery:      &RecoveryConfig{},
			WriteGate:     gate,
		}
	}
	t.Run("gate off, elector on", func(t *testing.T) {
		clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.LeaderElectionRecovery, false)
		gate := NewWriteGate()
		clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.LeaderElectionRecovery, true)
		if _, err := NewLeaderElector(newConfig(gate)); err == nil {
			t.Fatal("expected an error for a passthrough gate with recovery enabled")
		}
	})
	t.Run("gate on, elector off", func(t *testing.T) {
		clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.LeaderElectionRecovery, true)
		gate := NewWriteGate()
		clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.LeaderElectionRecovery, false)
		le, err := NewLeaderElector(newConfig(gate))
		if err != nil {
			t.Fatal(err)
		}
		if le.recovery != nil {
			t.Error("recovery must be ignored when the elector is created with the feature disabled")
		}
		inner := &probeTransport{}
		rt := le.WriteGate()(inner)
		if err := doRequest(t, rt, context.Background(), http.MethodPost); !errors.Is(err, ErrWriteGateClosed) {
			t.Errorf("a gate created with the feature on keeps gating, got %v", err)
		}
	})
}

// S20: a WriteGate created and applied to a client before the elector exists
// follows the elector's state.
func TestRecoveryScenario_ExternalWriteGate(t *testing.T) {
	enableRecovery(t)
	runScenario(t, func(s *scenario) {
		gate := NewWriteGate()
		inner := &probeTransport{}
		probe := &gateProbe{inner: inner, rt: gate.Wrapper()(inner)}
		if err := probe.write(s.ctx); !errors.Is(err, ErrWriteGateClosed) {
			t.Fatalf("new gate must be closed, got %v", err)
		}
		cfg := recoveryCfg(0)
		cfg.WriteGate = gate
		a := s.elector("a", cfg)
		if err := probe.write(s.ctx); err != nil {
			t.Errorf("gate must be open once leading, got %v", err)
		}
		// le.WriteGate() hands out the same gate.
		a.expectGateOpen(s.ctx, s.t0, "elector wrapper")
		s.at(outageAt)
		a.lock.setMode(modeBlackhole)
		s.at(lossAt)
		if err := probe.write(s.ctx); !errors.Is(err, ErrWriteGateClosed) {
			t.Errorf("gate must be closed after loss, got %v", err)
		}
		s.at(20 * sec)
		a.lock.setMode(modeOK)
		s.at(21 * sec)
		if err := probe.write(s.ctx); err != nil {
			t.Errorf("gate must reopen after recovery, got %v", err)
		}
	})
}
