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

// Randomized model-based simulation of recovery mode.
//
// Each seed builds a random plan (one or two electors, random connectivity
// timeline per elector with error/blackhole/uncertain/latency outages, random
// recovery deadline, optional cancellation), runs it in a synctest bubble
// while probing the write gates, and checks the invariants below. Every seed
// is a subtest, so a failure reproduces with -run 'TestRecoveryProperties/seed=N'.
// The plan is seed-stable; attempt timing is not, because election jitter
// comes from wait.Jitter's global math/rand source, so a failure also logs the
// lock operations and lease commits. Set LE_RECOVERY_SEEDS to run more seeds.
//
// The election timers are the kube defaults (15s/10s/2s) and are not varied:
// the protocol itself only guarantees non-overlapping leadership when
// LeaseDuration exceeds RenewDeadline+RetryPeriod, which NewLeaderElector
// does not enforce, so other timers would report that pre-existing property
// rather than a recovery-mode bug.
//
// Invariants:
//   I1 never two gates open at the same instant.
//   I2 a gate is open at t only if its elector received a successful acquire
//      or renew response in [t-(RetryPeriod+RenewDeadline), t].
//   I3 while closed, writes fail with ErrWriteGateClosed; reads always pass;
//      writes never fail with anything else.
//   I4 a write in flight when the gate closes fails with ErrWriteGateClosed.
//   I5 OnStartedLeading is called at most once; OnStoppedLeading once per gate
//      close (plus once if never led); "stopped leading" events and leaderOff
//      metrics agree; the OnStartedLeading context is cancelled iff Run returned.
//   I6 liveness: a lone elector whose connectivity is good for long enough,
//      and which has not given up, is leading again within
//      RenewDeadline + RetryPeriod*(1+JitterFactor) of connectivity returning.
//   I7 an elector whose connectivity stays bad for the whole recovery deadline
//      after a loss returns from Run by loss + deadline (+ RenewDeadline for a
//      release attempt).
//   I8 after Run returns the gate stays closed.
//   I9 a lone elector never gives up early: Run does not return before the
//      context is cancelled, or before loss + RecoveryDeadline.

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	propDuration   = 300 * sec
	propProbeEvery = 500 * msec
	propClientTmo  = 5 * sec // RenewDeadline / 2
)

type propSegment struct {
	from, to time.Duration
	mode     lockMode
	minLat   time.Duration
	maxLat   time.Duration
}

func (seg propSegment) String() string {
	if seg.mode == modeOK && seg.maxLat > 0 {
		return fmt.Sprintf("[%v,%v) ok latency %v-%v", seg.from, seg.to, seg.minLat, seg.maxLat)
	}
	return fmt.Sprintf("[%v,%v) %v", seg.from, seg.to, seg.mode)
}

// good reports whether the segment lets requests succeed within the client
// timeout.
func (seg propSegment) good() bool { return seg.mode == modeOK && seg.maxLat < propClientTmo }

// bad reports whether every request in the segment fails from the client's
// point of view (writes may still commit).
func (seg propSegment) bad() bool { return seg.mode != modeOK || seg.minLat > propClientTmo }

type propPlan struct {
	seed     int64
	deadline time.Duration
	release  bool
	bStart   time.Duration // 0 = no second elector
	cancelAt time.Duration // 0 = never
	timeline map[string][]propSegment
}

func (p propPlan) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "seed=%d deadline=%v release=%v bStart=%v cancelAt=%v\n", p.seed, p.deadline, p.release, p.bStart, p.cancelAt)
	for _, id := range []string{"a", "b"} {
		if segs, ok := p.timeline[id]; ok {
			fmt.Fprintf(&b, "  %s: %v\n", id, segs)
		}
	}
	return b.String()
}

func newPropPlan(seed int64) propPlan {
	rng := rand.New(rand.NewSource(seed))
	p := propPlan{seed: seed, timeline: map[string][]propSegment{}}
	p.deadline = []time.Duration{0, 0, 20 * sec, 40 * sec, 90 * sec}[rng.Intn(5)]
	p.release = rng.Intn(3) == 0
	if rng.Intn(2) == 0 {
		p.bStart = time.Duration(1+rng.Intn(10)) * sec
	}
	if rng.Intn(3) == 0 {
		p.cancelAt = time.Duration(30+rng.Intn(240)) * sec
	}
	ids := []string{"a"}
	if p.bStart > 0 {
		ids = append(ids, "b")
	}
	for _, id := range ids {
		var segs []propSegment
		at := time.Duration(0)
		good := true
		for at < propDuration {
			seg := propSegment{from: at}
			if good {
				seg.to = at + time.Duration(5+rng.Intn(56))*sec
				seg.mode = modeOK
			} else {
				seg.to = at + time.Duration(1+rng.Intn(45))*sec
				switch rng.Intn(4) {
				case 0:
					seg.mode = modeError
				case 1:
					seg.mode = modeBlackhole
				case 2:
					seg.mode = modeUncertain
				default:
					seg.mode = modeOK
					seg.minLat = time.Duration(1+rng.Intn(4)) * sec
					seg.maxLat = seg.minLat + time.Duration(rng.Intn(5))*sec
				}
			}
			if seg.to > propDuration {
				seg.to = propDuration
			}
			segs = append(segs, seg)
			at = seg.to
			good = !good
		}
		p.timeline[id] = segs
	}
	return p
}

func (p propPlan) segmentAt(id string, t time.Duration) propSegment {
	for _, seg := range p.timeline[id] {
		if t >= seg.from && t < seg.to {
			return seg
		}
	}
	return propSegment{from: propDuration, to: propDuration, mode: modeOK}
}

func (p propPlan) badThroughout(id string, from, to time.Duration) bool {
	for t := from; t <= to; t += propProbeEvery {
		if !p.segmentAt(id, t).bad() {
			return false
		}
	}
	return true
}

func TestRecoveryProperties(t *testing.T) {
	enableRecovery(t)
	seeds := 150
	if v := os.Getenv("LE_RECOVERY_SEEDS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("LE_RECOVERY_SEEDS: %v", err)
		}
		seeds = n
	}
	for seed := 0; seed < seeds; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			runRecoveryProperty(t, int64(seed))
		})
	}
}

// gateSample is one monitor observation of one elector's gate.
type gateTransition struct {
	at   time.Duration
	open bool
}

type propElector struct {
	h           *electorHarness
	transitions []gateTransition
	held        *probeWrite
	heldSince   time.Duration
	lastOpen    bool
	openProbes  []time.Duration // probe times at which a write passed
}

func runRecoveryProperty(t *testing.T, seed int64) {
	plan := newPropPlan(seed)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("plan:\n%s", plan)
		}
	})
	runScenario(t, func(s *scenario) {
		ctx := s.t0Ctx()
		var electorsMu sync.Mutex // the sampler goroutine reads the map while b may be added
		electors := map[string]*propElector{}
		newElector := func(id string) {
			cfg := recoveryCfg(plan.deadline)
			cfg.ReleaseOnCancel = plan.release
			h := s.newElector(id, cfg)
			electorsMu.Lock()
			electors[id] = &propElector{h: h}
			electorsMu.Unlock()
			s.start(h)
		}
		newElector("a")
		t.Cleanup(func() {
			if !t.Failed() {
				return
			}
			electorsMu.Lock()
			defer electorsMu.Unlock()
			for id, e := range electors {
				t.Logf("%s ops: %v", id, e.h.lock.opLog())
				t.Logf("%s transitions: %v", id, e.transitions)
			}
			for _, c := range s.server.commitLog() {
				t.Logf("commit by %s %s at %v holder=%q rv=%d", c.by, c.op, c.at.Sub(s.t0), c.rec.HolderIdentity, c.rv)
			}
		})

		// Gate sampler: 10ms resolution, records transitions per elector.
		sampleStop := make(chan struct{})
		sampleDone := make(chan struct{})
		go func() {
			defer close(sampleDone)
			ticker := time.NewTicker(10 * msec)
			defer ticker.Stop()
			for {
				select {
				case <-sampleStop:
					return
				case <-ticker.C:
					open := 0
					electorsMu.Lock()
					for _, e := range electors {
						isOpen := e.h.le.gate.isOpen()
						if isOpen {
							open++
						}
						if isOpen != e.lastOpen {
							e.transitions = append(e.transitions, gateTransition{at: s.now(), open: isOpen})
							e.lastOpen = isOpen
						}
					}
					electorsMu.Unlock()
					if open > 1 {
						s.violationsMu.Lock()
						s.violations = append(s.violations, s.now())
						s.violationsMu.Unlock()
					}
				}
			}
		}()

		applied := map[string]int{}
		cancelled := false
		for now := time.Duration(0); now <= propDuration; now += propProbeEvery {
			s.at(now)
			if plan.bStart > 0 && now == plan.bStart {
				newElector("b")
			}
			if plan.cancelAt > 0 && now == plan.cancelAt && !cancelled {
				s.cancel()
				cancelled = true
				settle()
			}
			// Apply connectivity segments that start now.
			for id, e := range electors {
				segs := plan.timeline[id]
				for applied[id] < len(segs) && segs[applied[id]].from <= now {
					seg := segs[applied[id]]
					e.h.lock.setMode(seg.mode)
					e.h.lock.setLatency(seg.minLat, seg.maxLat)
					applied[id]++
				}
			}
			// Probe every gate.
			for id, e := range electors {
				h := e.h
				if err := h.probe.read(ctx); err != nil {
					t.Errorf("t=%v %s: read failed: %v", now, id, err)
				}
				werr := h.probe.write(ctx)
				switch {
				case werr == nil:
					e.openProbes = append(e.openProbes, now)
					if h.returned() {
						t.Errorf("t=%v %s: write passed after Run returned", now, id)
					}
				case errors.Is(werr, ErrWriteGateClosed):
				default:
					t.Errorf("t=%v %s: write failed with unexpected error: %v", now, id, werr)
				}
				// I4: held writes.
				if e.held != nil {
					if rejected, err := e.held.rejected(); err != nil || rejected {
						// Completed. Only a close can complete it.
						if !errors.Is(err, ErrWriteGateClosed) {
							t.Errorf("t=%v %s: held write (since %v) completed with %v, want ErrWriteGateClosed", now, id, e.heldSince, err)
						}
						e.held = nil
					} else if werr != nil {
						t.Errorf("t=%v %s: gate closed but the write held since %v was not cancelled", now, id, e.heldSince)
						e.held.release()
						e.held = nil
					}
				}
				if e.held == nil && werr == nil && s.now() < propDuration-sec && rngPick(seed, now, id) {
					w := h.probe.startWrite(ctx)
					if rejected, _ := w.rejected(); !rejected {
						e.held = w
						e.heldSince = now
					}
				}
			}
		}
		// Wind down: release held writes, cancel, let everything return.
		for _, e := range electors {
			if e.held != nil {
				e.held.release()
				_ = e.held.wait()
			}
		}
		if !cancelled {
			s.cancel()
		}
		advance(time.Minute)
		close(sampleStop)
		<-sampleDone
		end := s.now()

		// Post-run invariants.
		for id, e := range electors {
			h := e.h
			h.expectReturned(s.t0, "wind down")
			// I8: closed after return.
			if err := h.probe.write(ctx); !errors.Is(err, ErrWriteGateClosed) {
				t.Errorf("%s: gate not closed after Run returned: %v", id, err)
			}
			// I5.
			started := h.rec.startedCount()
			stopped := h.rec.stoppedCount()
			if started > 1 {
				t.Errorf("%s: OnStartedLeading called %d times", id, started)
			}
			offs, ons := 0, 0
			for _, ev := range h.metrics.sequence() {
				if strings.HasPrefix(ev, "off") {
					offs++
				} else {
					ons++
				}
			}
			stoppedEvents := 0
			becameEvents := 0
			for _, ev := range h.lock.eventLog() {
				switch ev {
				case "stopped leading":
					stoppedEvents++
				case "became leader":
					becameEvents++
				}
			}
			if started == 0 {
				if stopped != 1 {
					t.Errorf("%s: never led, OnStoppedLeading called %d times, want 1", id, stopped)
				}
				if ons != 0 || offs != 0 || becameEvents != 0 || stoppedEvents != 0 {
					t.Errorf("%s: never led but metrics/events recorded: ons=%d offs=%d became=%d stopped=%d", id, ons, offs, becameEvents, stoppedEvents)
				}
			} else {
				if stopped != offs || stopped != stoppedEvents || ons != becameEvents || ons != offs {
					t.Errorf("%s: OnStoppedLeading=%d leaderOff=%d stoppedEvents=%d leaderOn=%d becameEvents=%d: want all closes equal and opens equal closes", id, stopped, offs, stoppedEvents, ons, becameEvents)
				}
				closes := 0
				for _, tr := range e.transitions {
					if !tr.open {
						closes++
					}
				}
				if closes > stopped {
					t.Errorf("%s: sampler saw %d gate closes but OnStoppedLeading was called %d times", id, closes, stopped)
				}
				if err := h.rec.leadingCtx(t).Err(); err == nil {
					t.Errorf("%s: OnStartedLeading context live after Run returned", id)
				}
			}
			// I2: every open interval is covered by recent successful responses.
			checkOpenIntervals(t, s.t0, id, e, h, end)
		}
		// I6 liveness and I9 no early give-up (single elector only).
		if plan.bStart == 0 {
			checkLiveness(t, s.t0, plan, electors["a"], cancelled, plan.cancelAt)
			checkNoEarlyGiveUp(t, s.t0, plan, electors["a"], cancelled, plan.cancelAt)
		}
		// I7 deadline.
		if plan.deadline > 0 {
			for id, e := range electors {
				checkDeadline(t, s.t0, plan, id, e, cancelled, plan.cancelAt)
			}
		}
	})
}

// rngPick decides pseudo-randomly (but reproducibly per seed) whether to hold
// a write at this probe.
func rngPick(seed int64, now time.Duration, id string) bool {
	r := rand.New(rand.NewSource(seed*1000003 + int64(now/propProbeEvery)*31 + int64(id[0])))
	return r.Intn(6) == 0
}

// checkOpenIntervals verifies I2 from the sampler's open intervals and the
// lock's successful write responses.
func checkOpenIntervals(t *testing.T, t0 time.Time, id string, e *propElector, h *electorHarness, end time.Duration) {
	t.Helper()
	window := 2*sec + 10*sec // RetryPeriod + RenewDeadline
	// The sampler observes transitions up to one period late.
	const samplerSlack = 10 * msec
	var successes []time.Duration
	for _, op := range h.lock.opLog() {
		if op.err == nil && (op.op == "update" || op.op == "create") {
			successes = append(successes, op.done.Sub(t0))
		}
	}
	slices.Sort(successes)
	var open *time.Duration
	check := func(from, to time.Duration) {
		// Successes in [from-window, to] must start no later than from and
		// never leave a gap larger than window, ending no earlier than to-window.
		last := time.Duration(-1)
		first := true
		for _, sTime := range successes {
			if sTime < from-window || sTime > to {
				continue
			}
			if first {
				if sTime > from {
					t.Errorf("%s: gate open at %v but the first success in the window is at %v", id, from, sTime)
				}
				first = false
			} else if sTime-last > window+samplerSlack {
				t.Errorf("%s: gate open across [%v,%v] but successes at %v and %v are more than %v apart", id, from, to, last, sTime, window)
			}
			last = sTime
		}
		if first {
			t.Errorf("%s: gate open at [%v,%v] with no successful response in [%v,%v]", id, from, to, from-window, to)
			return
		}
		if to-last > window+samplerSlack {
			t.Errorf("%s: gate still open at %v but the last success was at %v", id, to, last)
		}
	}
	for _, tr := range e.transitions {
		if tr.open {
			at := tr.at
			open = &at
			continue
		}
		if open != nil {
			check(*open, tr.at)
			open = nil
		}
	}
	if open != nil {
		check(*open, end)
	}
	// Write probes that passed are also instants at which the gate was open.
	for _, at := range e.openProbes {
		check(at, at)
	}
}

// checkLiveness verifies I6 for a lone elector: after a loss, once
// connectivity is good for RenewDeadline + maxGap, the gate is open again
// (unless the elector gave up or was cancelled).
func checkLiveness(t *testing.T, t0 time.Time, plan propPlan, e *propElector, cancelled bool, cancelAt time.Duration) {
	t.Helper()
	bound := 10*sec + maxGap + jitterSlop
	returnedAt, returned := e.h.returnedAt()
	retOffset := time.Duration(-1)
	if returned {
		retOffset = returnedAt.Sub(t0)
	}
	for _, seg := range plan.timeline["a"] {
		if !seg.good() || seg.to-seg.from < bound+sec {
			continue
		}
		deadline := seg.from + bound
		if returned && retOffset <= deadline {
			continue // gave up (deadline) or cancelled before it could recover
		}
		if cancelled && cancelAt <= deadline {
			continue
		}
		// Was the gate open at some point in [seg.from+bound, seg.to]?
		openInWindow := false
		for _, tr := range e.transitions {
			if tr.open && tr.at <= deadline+propProbeEvery {
				openInWindow = true // opened before or at the bound
			}
			if !tr.open && tr.at <= deadline && tr.at > seg.from {
				openInWindow = false
			}
		}
		if !openInWindow {
			// Fallback: was there a passing write probe by the bound?
			for _, at := range e.openProbes {
				if at >= seg.from && at <= deadline+propProbeEvery {
					openInWindow = true
					break
				}
			}
		}
		if !openInWindow {
			t.Errorf("liveness: connectivity good from %v but gate not open by %v; transitions=%v", seg.from, deadline, e.transitions)
		}
	}
}

// checkDeadline verifies I7: after a loss at L, if connectivity stays bad
// through L+deadline and nothing else ended the run, Run returns by
// L+deadline (+RenewDeadline for the release attempt).
func checkDeadline(t *testing.T, t0 time.Time, plan propPlan, id string, e *propElector, cancelled bool, cancelAt time.Duration) {
	t.Helper()
	returnedAt, returned := e.h.returnedAt()
	if !returned {
		return
	}
	retOffset := returnedAt.Sub(t0)
	for _, stoppedAt := range e.h.rec.stoppedTimes() {
		loss := stoppedAt.Sub(t0)
		if cancelled && cancelAt <= loss {
			continue
		}
		until := loss + plan.deadline
		if until > propDuration || !plan.badThroughout(id, loss, until) {
			continue
		}
		if cancelled && cancelAt < until {
			continue
		}
		bound := until + jitterSlop
		if plan.release {
			bound += 10 * sec
		}
		if retOffset > bound {
			t.Errorf("%s: lost lease at %v with connectivity bad through %v, but Run returned at %v (bound %v)", id, loss, until, retOffset, bound)
		}
	}
}

// checkNoEarlyGiveUp verifies I9 for a lone elector: Run returns only because
// of cancellation or because RecoveryDeadline passed after a loss.
func checkNoEarlyGiveUp(t *testing.T, t0 time.Time, plan propPlan, e *propElector, cancelled bool, cancelAt time.Duration) {
	t.Helper()
	returnedAt, returned := e.h.returnedAt()
	if !returned {
		return
	}
	retOffset := returnedAt.Sub(t0)
	if cancelled && retOffset >= cancelAt {
		return
	}
	if retOffset >= propDuration {
		return // the wind-down cancellation
	}
	if plan.deadline == 0 {
		t.Errorf("Run returned at %v with an unlimited recovery deadline and no cancellation", retOffset)
		return
	}
	var lastLoss time.Duration = -1
	for _, at := range e.h.rec.stoppedTimes() {
		if d := at.Sub(t0); d < retOffset && d > lastLoss {
			lastLoss = d
		}
	}
	if lastLoss < 0 {
		t.Errorf("Run returned at %v without any loss", retOffset)
		return
	}
	if retOffset < lastLoss+plan.deadline {
		t.Errorf("Run returned at %v, before loss %v + deadline %v", retOffset, lastLoss, plan.deadline)
	}
}
