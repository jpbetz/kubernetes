/*
Copyright 2015 The Kubernetes Authors.

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

// Package leaderelection implements leader election of a set of endpoints.
// It uses an annotation in the endpoints object to store the record of the
// election state. This implementation does not guarantee that only one
// client is acting as a leader (a.k.a. fencing).
//
// A client only acts on timestamps captured locally to infer the state of the
// leader election. The client does not consider timestamps in the leader
// election record to be accurate because these timestamps may not have been
// produced by a local clock. The implemention does not depend on their
// accuracy and only uses their change to indicate that another client has
// renewed the leader lease. Thus the implementation is tolerant to arbitrary
// clock skew, but is not tolerant to arbitrary clock skew rate.
//
// However the level of tolerance to skew rate can be configured by setting
// RenewDeadline and LeaseDuration appropriately. The tolerance expressed as a
// maximum tolerated ratio of time passed on the fastest node to time passed on
// the slowest node can be approximately achieved with a configuration that sets
// the same ratio of LeaseDuration to RenewDeadline. For example if a user wanted
// to tolerate some nodes progressing forward in time twice as fast as other nodes,
// the user could set LeaseDuration to 60 seconds and RenewDeadline to 30 seconds.
//
// While not required, some method of clock synchronization between nodes in the
// cluster is highly recommended. It's important to keep in mind when configuring
// this client that the tolerance to skew rate varies inversely to master
// availability.
//
// Larger clusters often have a more lenient SLA for API latency. This should be
// taken into account when configuring the client. The rate of leader transitions
// should be monitored and RetryPeriod and LeaseDuration should be increased
// until the rate is stable and acceptably low. It's important to keep in mind
// when configuring this client that the tolerance to API latency varies inversely
// to master availability.
//
// DISCLAIMER: this is an alpha API. This library will likely change significantly
// or even be removed entirely in subsequent releases. Depend on this API at
// your own risk.
package leaderelection

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientfeatures "k8s.io/client-go/features"
	rl "k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/transport"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
)

const (
	JitterFactor = 1.2
)

// NewLeaderElector creates a LeaderElector from a LeaderElectionConfig
func NewLeaderElector(lec LeaderElectionConfig) (*LeaderElector, error) {
	if lec.LeaseDuration <= lec.RenewDeadline {
		return nil, fmt.Errorf("leaseDuration must be greater than renewDeadline")
	}
	if lec.RenewDeadline <= time.Duration(JitterFactor*float64(lec.RetryPeriod)) {
		return nil, fmt.Errorf("renewDeadline must be greater than retryPeriod*JitterFactor")
	}
	if lec.LeaseDuration < 1 {
		return nil, fmt.Errorf("leaseDuration must be greater than zero")
	}
	if lec.RenewDeadline < 1 {
		return nil, fmt.Errorf("renewDeadline must be greater than zero")
	}
	if lec.RetryPeriod < 1 {
		return nil, fmt.Errorf("retryPeriod must be greater than zero")
	}
	if lec.Callbacks.OnStartedLeading == nil {
		return nil, fmt.Errorf("OnStartedLeading callback must not be nil")
	}
	if lec.Callbacks.OnStoppedLeading == nil {
		return nil, fmt.Errorf("OnStoppedLeading callback must not be nil")
	}

	if lec.Lock == nil {
		return nil, fmt.Errorf("Lock must not be nil.")
	}
	id := lec.Lock.Identity()
	if id == "" {
		return nil, fmt.Errorf("Lock identity is empty")
	}
	recoveryEnabled := clientfeatures.FeatureGates().Enabled(clientfeatures.LeaderElectionRecovery)
	if lec.Recovery != nil && recoveryEnabled {
		if lec.Recovery.RecoveryDeadline < 0 {
			return nil, fmt.Errorf("recoveryDeadline must not be negative")
		}
		if lec.Coordinated {
			return nil, fmt.Errorf("recovery is not supported with coordinated leader election")
		}
	}
	if lec.WriteGate == nil {
		lec.WriteGate = NewWriteGate()
	} else if recoveryEnabled && lec.WriteGate.gate == nil {
		return nil, fmt.Errorf("WriteGate was created while the LeaderElectionRecovery feature gate was disabled; create it after feature gates are configured")
	}
	if err := lec.WriteGate.claim(); err != nil {
		return nil, err
	}

	le := LeaderElector{
		config:  lec,
		clock:   clock.RealClock{},
		metrics: globalMetricsFactory.newLeaderMetrics(),
		gate:    lec.WriteGate,
	}
	if recoveryEnabled {
		le.recovery = lec.Recovery
	} else if lec.Recovery != nil {
		klog.Background().Info("Ignoring leader election recovery configuration because the LeaderElectionRecovery feature gate is disabled", "lock", lec.Lock.Describe())
	}
	le.metrics.leaderOff(le.config.Name)
	return &le, nil
}

type LeaderElectionConfig struct {
	// Lock is the resource that will be used for locking
	Lock rl.Interface

	// LeaseDuration is the duration that non-leader candidates will
	// wait to force acquire leadership. This is measured against time of
	// last observed ack.
	//
	// A client needs to wait a full LeaseDuration without observing a change to
	// the record before it can attempt to take over. When all clients are
	// shutdown and a new set of clients are started with different names against
	// the same leader record, they must wait the full LeaseDuration before
	// attempting to acquire the lease. Thus LeaseDuration should be as short as
	// possible (within your tolerance for clock skew rate) to avoid a possible
	// long waits in the scenario.
	//
	// Core clients default this value to 15 seconds.
	LeaseDuration time.Duration
	// RenewDeadline is the duration that the acting master will retry
	// refreshing leadership before giving up.
	//
	// Core clients default this value to 10 seconds.
	RenewDeadline time.Duration
	// RetryPeriod is the duration the LeaderElector clients should wait
	// between tries of actions.
	//
	// Core clients default this value to 2 seconds.
	RetryPeriod time.Duration

	// Callbacks are callbacks that are triggered during certain lifecycle
	// events of the LeaderElector
	Callbacks LeaderCallbacks

	// WatchDog is the associated health checker
	// WatchDog may be null if it's not needed/configured.
	WatchDog *HealthzAdaptor

	// ReleaseOnCancel should be set true if the lock should be released
	// when the run context is cancelled. If you set this to true, you must
	// ensure all code guarded by this lease has successfully completed
	// prior to cancelling the context, or you may have two processes
	// simultaneously acting on the critical path.
	ReleaseOnCancel bool

	// Name is the name of the resource lock for debugging
	Name string

	// Coordinated will use the Coordinated Leader Election feature
	// WARNING: Coordinated leader election is ALPHA.
	Coordinated bool

	// Recovery configures how the elector handles API unavailability. When
	// set, and the LeaderElectionRecovery client-go feature gate is enabled,
	// the elector keeps running after it fails to renew its lease and resumes
	// leading when a later renewal succeeds. See RecoveryConfig.
	//
	// Ignored, including its validation, when the LeaderElectionRecovery
	// feature gate is disabled. Not supported together with Coordinated.
	Recovery *RecoveryConfig

	// WriteGate is the write gate this elector opens while leading and closes
	// otherwise. Set it when the gated clients must be built before the
	// elector exists; see WriteGate. If nil, NewLeaderElector creates one.
	// LeaderElector.WriteGate returns the wrapper in both cases.
	//
	// A WriteGate must not be shared between electors.
	WriteGate *WriteGate
}

// RecoveryConfig configures recovery mode, in which losing the lease does not
// end the election loop.
//
// In recovery mode the write gate (see LeaderElector.WriteGate) closes and
// OnStoppedLeading is called when renewal fails for RenewDeadline, exactly
// where Run would return today. Instead of returning, the elector keeps trying
// to renew with the same identity. If a later renewal succeeds the write gate
// reopens; OnStartedLeading is not called again and its context stays live
// until Run returns.
//
// Recovery only ever renews the lease this elector still holds. Run returns
// when another candidate is observed holding the lease, when the lease turns
// out to have changed hands or been deleted in the meantime (even if the other
// holder has since released it or expired), when the recovery deadline passes,
// or when the context is cancelled. That return, not OnStoppedLeading, is the
// signal that the elector gave up.
type RecoveryConfig struct {
	// RecoveryDeadline limits how long the elector keeps trying to renew after
	// losing the lease due to API unavailability. It is measured from the
	// moment the lease was lost. When it passes, Run returns.
	//
	// Regardless of the deadline, Run returns as soon as another candidate is
	// observed holding the lease.
	//
	// Zero means no limit.
	RecoveryDeadline time.Duration
}

// LeaderCallbacks are callbacks that are triggered during certain
// lifecycle events of the LeaderElector. These are invoked asynchronously.
//
// possible future callbacks:
//   - OnChallenge()
type LeaderCallbacks struct {
	// OnStartedLeading is called when a LeaderElector client starts leading.
	// The context is cancelled when Run returns. In recovery mode it is called
	// only once, when the lease is first acquired; regaining a lost lease
	// reopens the write gate without calling it again.
	OnStartedLeading func(context.Context)
	// OnStoppedLeading is called when a LeaderElector client stops leading.
	// This callback is always called when the LeaderElector exits, even if it did not start leading.
	// Users should not assume that OnStoppedLeading is only called after OnStartedLeading.
	// see: https://github.com/kubernetes/kubernetes/pull/127675#discussion_r1780059887
	//
	// In recovery mode (see RecoveryConfig) it is called every time the client
	// stops leading, which no longer implies that Run is returning: once per
	// lost lease, and once more if the context is cancelled while leading. It
	// is not called again when Run returns after a loss. The write gate is
	// closed before it is called.
	OnStoppedLeading func()
	// OnNewLeader is called when the client observes a leader that is
	// not the previously observed leader. This includes the first observed
	// leader when the client starts.
	OnNewLeader func(identity string)
}

// LeaderElector is a leader election client.
type LeaderElector struct {
	config LeaderElectionConfig
	// internal bookkeeping
	observedRecord    rl.LeaderElectionRecord
	observedRawRecord []byte
	observedTime      time.Time
	// used to implement OnNewLeader(), may lag slightly from the
	// value observedRecord.HolderIdentity if the transition has
	// not yet been reported.
	reportedLeader string

	// clock is wrapper around time to allow for less flaky testing
	clock clock.Clock

	// used to lock the observedRecord and the observedTime
	observedRecordLock sync.RWMutex

	metrics leaderMetricsAdapter

	// gate is the write gate opened while leading. Always set; it passes
	// everything through when the LeaderElectionRecovery feature gate is
	// disabled.
	gate *WriteGate
	// recovery is the effective recovery configuration: config.Recovery when
	// the LeaderElectionRecovery feature gate is enabled, nil otherwise.
	recovery *RecoveryConfig
	// lastAttempt is when the most recent acquire/renew attempt started.
	// Guarded by observedRecordLock.
	lastAttempt time.Time
}

// WriteGate returns the transport wrapper of this elector's write gate: it
// rejects write requests (every method other than GET, HEAD, OPTIONS and
// TRACE) whenever this elector is not leading, and cancels writes that are in
// flight when it stops leading. Rejected and cancelled writes fail with an
// error wrapping ErrWriteGateClosed. Reads always pass. See the WriteGate type.
//
// Apply it to the rest.Config used to build the clients whose writes must only
// happen while leading, for example with rest.Config.Wrap, before those clients
// are built. The clients used by the lock and the event recorder must not be
// gated.
//
// When the LeaderElectionRecovery feature gate is disabled the returned wrapper
// passes every request through.
func (le *LeaderElector) WriteGate() transport.WrapperFunc {
	return le.gate.Wrapper()
}

// Run starts the leader election loop. Run will not return
// before leader election loop is stopped by ctx or it has
// stopped holding the leader lease.
//
// In recovery mode (see RecoveryConfig) losing the lease does not end the
// loop: Run keeps trying to renew and returns only when ctx is cancelled,
// another candidate is observed holding the lease, or the recovery deadline
// passes.
func (le *LeaderElector) Run(ctx context.Context) {
	defer runtime.HandleCrashWithContext(ctx)
	// stopped records that OnStoppedLeading has been delivered for the
	// current state. If Run unwinds with it false (a panic while leading or
	// acquiring), the gate is closed and the callback still fires, as it
	// always has.
	stopped := false
	defer func() {
		if !stopped {
			le.gateClose()
			le.config.Callbacks.OnStoppedLeading()
		}
	}()

	if !le.acquire(ctx) {
		stopped = true
		le.config.Callbacks.OnStoppedLeading()
		return // ctx signalled done
	}
	logger := klog.FromContext(ctx)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	le.gateOpen()
	go le.config.Callbacks.OnStartedLeading(ctx)

	for {
		lost := le.renew(ctx)
		lostAt := time.Now()
		// Stop writes first, then tell everyone.
		le.gateClose()
		le.metrics.leaderOff(le.config.Name)
		le.config.Lock.RecordEvent("stopped leading")
		if !lost || le.recovery == nil {
			break
		}
		stopped = true
		le.config.Callbacks.OnStoppedLeading()
		if !le.recover(ctx, lostAt) {
			// Cancelled, out of time, or superseded. OnStoppedLeading already
			// fired for this loss.
			if le.config.ReleaseOnCancel {
				le.release(logger)
			}
			return
		}
		stopped = false
		le.gateOpen()
	}

	// Exiting: either ctx was cancelled while leading, or (without recovery)
	// renewal failed.
	if le.config.ReleaseOnCancel {
		le.release(logger)
	}
	cancel()
	stopped = true
	le.config.Callbacks.OnStoppedLeading()
}

// gateOpen opens the write gate.
func (le *LeaderElector) gateOpen() { le.gate.open() }

// gateClose closes the write gate. It returns once every in-flight gated
// write has been cancelled.
func (le *LeaderElector) gateClose() { le.gate.close() }

// RunOrDie starts a client with the provided config or panics if the config
// fails to validate. RunOrDie blocks until leader election loop is
// stopped by ctx or it has stopped holding the leader lease. In recovery mode
// (see RecoveryConfig) it keeps running after losing the lease and returns
// only when the elector gives up; that return, not OnStoppedLeading, is the
// signal to exit.
func RunOrDie(ctx context.Context, lec LeaderElectionConfig) {
	le, err := NewLeaderElector(lec)
	if err != nil {
		panic(err)
	}
	if lec.WatchDog != nil {
		lec.WatchDog.SetLeaderElection(le)
	}
	le.Run(ctx)
}

// GetLeader returns the identity of the last observed leader or returns the empty string if
// no leader has yet been observed.
// This function is for informational purposes. (e.g. monitoring, logs, etc.)
// In recovery mode it keeps reporting this client while the lease is lost and
// no other holder has been observed yet; the write gate reflects whether the
// client is actually leading.
func (le *LeaderElector) GetLeader() string {
	return le.getObservedRecord().HolderIdentity
}

// IsLeader returns true if the last observed leader was this client else returns false.
// It reflects the last observed lease record, not whether this client is
// currently allowed to act as leader: in recovery mode it stays true while
// the lease is lost, until another holder is observed. Use the write gate to
// enforce that writes only happen while leading.
func (le *LeaderElector) IsLeader() bool {
	return le.getObservedRecord().HolderIdentity == le.config.Lock.Identity()
}

// acquire loops calling tryAcquireOrRenew and returns true immediately when tryAcquireOrRenew succeeds.
// Returns false if ctx signals done.
func (le *LeaderElector) acquire(ctx context.Context) bool {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	succeeded := false
	desc := le.config.Lock.Describe()
	logger := klog.FromContext(ctx)
	logger.Info("Attempting to acquire leader lease...", "lock", desc)
	wait.JitterUntilWithContext(ctx, func(ctx context.Context) {
		if !le.config.Coordinated {
			succeeded = le.tryAcquireOrRenew(ctx)
		} else {
			succeeded = le.tryCoordinatedRenew(ctx)
		}
		le.maybeReportTransition()
		if !succeeded {
			logger.V(4).Info("Failed to acquire lease", "lock", desc)
			return
		}
		le.config.Lock.RecordEvent("became leader")
		le.metrics.leaderOn(le.config.Name)
		logger.Info("Successfully acquired lease", "lock", desc)
		cancel()
	}, le.config.RetryPeriod, JitterFactor, true)
	return succeeded
}

// renew loops calling tryAcquireOrRenew and returns immediately when tryAcquireOrRenew fails or ctx signals done.
// It returns true if the lease was lost (renewal kept failing for RenewDeadline)
// and false if it returned because ctx is done.
func (le *LeaderElector) renew(parent context.Context) bool {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	logger := klog.FromContext(ctx)
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		err := wait.PollUntilContextTimeout(ctx, le.config.RetryPeriod, le.config.RenewDeadline, true, func(ctx context.Context) (done bool, err error) {
			// PollUntilContextTimeout invokes condition even when the context is canceled when immediate=true.
			// Short-circuit this to prevent unnecessary processing and error log messages.
			if err := ctx.Err(); err != nil {
				return false, err
			}
			if !le.config.Coordinated {
				return le.tryAcquireOrRenew(ctx), nil
			} else {
				return le.tryCoordinatedRenew(ctx), nil
			}
		})
		le.maybeReportTransition()
		desc := le.config.Lock.Describe()
		if err == nil {
			logger.V(5).Info("Successfully renewed lease", "lock", desc)
			return
		}
		logger.Info("Failed to renew lease", "lock", desc, "err", err)
		cancel()
	}, le.config.RetryPeriod)
	return parent.Err() == nil
}

// recover keeps trying to renew a lost lease with the same identity. It
// returns true when a renewal succeeded and the elector leads again. It
// returns false when the elector must exit: ctx is done, the recovery
// deadline (measured from lostAt) passed, another candidate was observed
// holding the lease, or the lease can no longer be renewed because it was
// deleted or changed hands in the meantime.
func (le *LeaderElector) recover(ctx context.Context, lostAt time.Time) bool {
	logger := klog.FromContext(ctx)
	desc := le.config.Lock.Describe()
	if le.recovery.RecoveryDeadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, lostAt.Add(le.recovery.RecoveryDeadline))
		defer cancel()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	logger.Info("Lost lease, attempting to recover it", "lock", desc, "recoveryDeadline", le.recovery.RecoveryDeadline)
	recovered := false
	superseded := false
	// Non-sliding: the next attempt is due RetryPeriod (plus jitter) after the
	// previous one started, however long it took, so attempts start at least
	// every max(RenewDeadline, RetryPeriod*(1+JitterFactor)) and Check can
	// tell a slow loop from a stuck one.
	wait.JitterUntilWithContext(ctx, func(ctx context.Context) {
		// Bound each attempt like the renew loop bounds its whole poll.
		attemptCtx, attemptCancel := context.WithTimeout(ctx, le.config.RenewDeadline)
		succeeded, cannotRenew := le.tryAcquireOrRenewWith(attemptCtx, true)
		attemptCancel()
		le.maybeReportTransition()
		if cannotRenew {
			logger.Info("Giving up on recovering lease, it was deleted or changed hands", "lock", desc)
			superseded = true
			cancel()
			return
		}
		if succeeded {
			if ctx.Err() != nil {
				// A late success after cancellation or the deadline does not
				// make us the leader again.
				return
			}
			recovered = true
			cancel()
			return
		}
		if leader := le.GetLeader(); leader != "" && leader != le.config.Lock.Identity() {
			logger.Info("Giving up on recovering lease, another candidate holds it", "lock", desc, "holder", leader)
			superseded = true
			cancel()
			return
		}
		logger.V(4).Info("Failed to recover lease", "lock", desc)
	}, le.config.RetryPeriod, JitterFactor, false)
	if !recovered {
		if !superseded {
			logger.Info("Giving up on recovering lease", "lock", desc, "err", ctx.Err())
		}
		return false
	}
	le.config.Lock.RecordEvent("became leader")
	le.metrics.leaderOn(le.config.Name)
	logger.Info("Recovered lease", "lock", desc)
	return true
}

// release attempts to release the leader lease if we have acquired it.
// It retries on conflict, which may occur if the context cancellation
// races with an inflight renew() operation. The client will see a ctx
// cancellation error while the apiserver completes the update and bumps
// the resource version.
func (le *LeaderElector) release(logger klog.Logger) bool {
	ctx := klog.NewContext(context.Background(), logger)
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, le.config.RenewDeadline)
	defer timeoutCancel()
	return le.tryRelease(timeoutCtx)
}

func (le *LeaderElector) tryRelease(ctx context.Context) bool {
	logger := klog.FromContext(ctx)
	oldLeaderElectionRecord, _, err := le.config.Lock.Get(ctx)
	if err != nil {
		if !errors.IsNotFound(err) {
			logger.Error(err, "error retrieving resource lock", "lock", le.config.Lock.Describe())
			return false
		}
		logger.Info("lease lock not found", "lock", le.config.Lock.Describe())
		return false
	}

	if !le.IsLeader() {
		return true
	}
	// Decide from the record just fetched, not the last observed one: in
	// recovery mode the observed record can name this client long after
	// another candidate took over.
	if oldLeaderElectionRecord.HolderIdentity != le.config.Lock.Identity() {
		logger.V(4).Info("Not releasing lease held by another candidate", "lock", le.config.Lock.Describe(), "holder", oldLeaderElectionRecord.HolderIdentity)
		return true
	}
	now := metav1.NewTime(le.clock.Now())
	leaderElectionRecord := rl.LeaderElectionRecord{
		LeaderTransitions:    oldLeaderElectionRecord.LeaderTransitions,
		LeaseDurationSeconds: 1,
		RenewTime:            now,
		AcquireTime:          now,
	}
	if err := le.config.Lock.Update(ctx, leaderElectionRecord); err != nil {
		if errors.IsConflict(err) {
			logger.V(4).Info("Conflict when releasing lease, retrying", "lock", le.config.Lock.Describe())
			return le.tryRelease(ctx)
		}
		logger.Error(err, "Failed to release lease", "lock", le.config.Lock.Describe())
		return false
	}

	le.setObservedRecord(&leaderElectionRecord)
	return true
}

// tryCoordinatedRenew checks if it acquired a lease and tries to renew the
// lease if it has already been acquired. Returns true on success else returns
// false.
func (le *LeaderElector) tryCoordinatedRenew(ctx context.Context) bool {
	logger := klog.FromContext(ctx)
	now := metav1.NewTime(le.clock.Now())
	le.noteAttempt(now.Time)
	leaderElectionRecord := rl.LeaderElectionRecord{
		HolderIdentity:       le.config.Lock.Identity(),
		LeaseDurationSeconds: int(le.config.LeaseDuration / time.Second),
		RenewTime:            now,
		AcquireTime:          now,
	}

	// 1. obtain the electionRecord
	oldLeaderElectionRecord, oldLeaderElectionRawRecord, err := le.config.Lock.Get(ctx)
	if err != nil {
		if !errors.IsNotFound(err) {
			logger.Error(err, "Error retrieving lease lock", "lock", le.config.Lock.Describe())
			return false
		}
		logger.Info("Lease lock not found", "lock", le.config.Lock.Describe(), "err", err)
		return false
	}

	// 2. Record obtained, check the Identity & Time
	if !bytes.Equal(le.observedRawRecord, oldLeaderElectionRawRecord) {
		le.setObservedRecord(oldLeaderElectionRecord)

		le.observedRawRecord = oldLeaderElectionRawRecord
	}

	le.observedRecordLock.RLock()
	obsTime := le.observedTime
	le.observedRecordLock.RUnlock()

	hasExpired := obsTime.Add(time.Second * time.Duration(oldLeaderElectionRecord.LeaseDurationSeconds)).Before(now.Time)
	if hasExpired {
		logger.Info("Lease has expired", "lock", le.config.Lock.Describe())
		return false
	}

	if !le.IsLeader() {
		logger.V(6).Info("Lease is held and has not yet expired", "lock", le.config.Lock.Describe(), "holder", oldLeaderElectionRecord.HolderIdentity)
		return false
	}

	// 2b. If the lease has been marked as "end of term", don't renew it
	if le.IsLeader() && oldLeaderElectionRecord.PreferredHolder != "" {
		logger.V(4).Info("Lease is marked as 'end of term'", "lock", le.config.Lock.Describe())
		// TODO: Instead of letting lease expire, the holder may deleted it directly
		// This will not be compatible with all controllers, so it needs to be opt-in behavior.
		// We must ensure all code guarded by this lease has successfully completed
		// prior to releasing or there may be two processes
		// simultaneously acting on the critical path.
		// Usually once this returns false, the process is terminated..
		// xref: OnStoppedLeading
		return false
	}

	// 3. We're going to try to update. The leaderElectionRecord is set to it's default
	// here. Let's correct it before updating.
	if le.IsLeader() {
		leaderElectionRecord.AcquireTime = oldLeaderElectionRecord.AcquireTime
		leaderElectionRecord.LeaderTransitions = oldLeaderElectionRecord.LeaderTransitions
		leaderElectionRecord.Strategy = oldLeaderElectionRecord.Strategy
		le.metrics.slowpathExercised(le.config.Name)
	} else {
		leaderElectionRecord.LeaderTransitions = oldLeaderElectionRecord.LeaderTransitions + 1
	}

	// update the lock itself
	if err = le.config.Lock.Update(ctx, leaderElectionRecord); err != nil {
		logger.Error(err, "Failed to update lock", "lock", le.config.Lock.Describe())
		return false
	}

	le.setObservedRecord(&leaderElectionRecord)
	return true
}

// tryAcquireOrRenew tries to acquire a leader lease if it is not already acquired,
// else it tries to renew the lease if it has already been acquired. Returns true
// on success else returns false.
func (le *LeaderElector) tryAcquireOrRenew(ctx context.Context) bool {
	succeeded, _ := le.tryAcquireOrRenewWith(ctx, false)
	return succeeded
}

// tryAcquireOrRenewWith is tryAcquireOrRenew with an option. With renewOnly,
// it only renews a lease this client already holds and never acquires: if the
// lease does not exist or its holder is not this client, it returns
// cannotRenew=true instead of writing. Recovery uses this so that a lease
// that changed hands while this client was inactive is never taken back.
func (le *LeaderElector) tryAcquireOrRenewWith(ctx context.Context, renewOnly bool) (succeeded bool, cannotRenew bool) {
	logger := klog.FromContext(ctx)
	now := metav1.NewTime(le.clock.Now())
	le.noteAttempt(now.Time)
	leaderElectionRecord := rl.LeaderElectionRecord{
		HolderIdentity:       le.config.Lock.Identity(),
		LeaseDurationSeconds: int(le.config.LeaseDuration / time.Second),
		RenewTime:            now,
		AcquireTime:          now,
	}

	// 1. fast path for the leader to update optimistically assuming that the record observed
	// last time is the current version.
	if le.IsLeader() && le.isLeaseValid(now.Time) {
		oldObservedRecord := le.getObservedRecord()
		leaderElectionRecord.AcquireTime = oldObservedRecord.AcquireTime
		leaderElectionRecord.LeaderTransitions = oldObservedRecord.LeaderTransitions

		err := le.config.Lock.Update(ctx, leaderElectionRecord)
		if err == nil {
			le.setObservedRecord(&leaderElectionRecord)
			return true, false
		}
		logger.V(2).Info("Failed to update lease optimistically, falling back to slow path", "lock", le.config.Lock.Describe(), "err", err)
	}

	// 2. obtain or create the ElectionRecord
	oldLeaderElectionRecord, oldLeaderElectionRawRecord, err := le.config.Lock.Get(ctx)
	if err != nil {
		if !errors.IsNotFound(err) {
			logger.Error(err, "Error retrieving lease lock", "lock", le.config.Lock.Describe())
			return false, false
		}
		if renewOnly {
			logger.Info("Lease lock not found", "lock", le.config.Lock.Describe())
			return false, true
		}
		if err = le.config.Lock.Create(ctx, leaderElectionRecord); err != nil {
			logger.Error(err, "Error initially creating lease lock", "lock", le.config.Lock.Describe())
			return false, false
		}

		le.setObservedRecord(&leaderElectionRecord)

		return true, false
	}

	// 3. Record obtained, check the Identity & Time
	if !bytes.Equal(le.observedRawRecord, oldLeaderElectionRawRecord) {
		le.setObservedRecord(oldLeaderElectionRecord)

		le.observedRawRecord = oldLeaderElectionRawRecord
	}
	if len(oldLeaderElectionRecord.HolderIdentity) > 0 && le.isLeaseValid(now.Time) && !le.IsLeader() {
		logger.V(4).Info("Lease is held by and has not yet expired", "lock", le.config.Lock.Describe(), "holder", oldLeaderElectionRecord.HolderIdentity)
		return false, false
	}

	// 4. We're going to try to update. The leaderElectionRecord is set to it's default
	// here. Let's correct it before updating.
	if le.IsLeader() {
		leaderElectionRecord.AcquireTime = oldLeaderElectionRecord.AcquireTime
		leaderElectionRecord.LeaderTransitions = oldLeaderElectionRecord.LeaderTransitions
		le.metrics.slowpathExercised(le.config.Name)
	} else {
		if renewOnly {
			logger.Info("Lease is no longer held by this client", "lock", le.config.Lock.Describe(), "holder", oldLeaderElectionRecord.HolderIdentity)
			return false, true
		}
		leaderElectionRecord.LeaderTransitions = oldLeaderElectionRecord.LeaderTransitions + 1
	}

	// update the lock itself
	if err = le.config.Lock.Update(ctx, leaderElectionRecord); err != nil {
		logger.Error(err, "Failed to update lease", "lock", le.config.Lock.Describe())
		return false, false
	}

	le.setObservedRecord(&leaderElectionRecord)
	return true, false
}

func (le *LeaderElector) maybeReportTransition() {
	if le.observedRecord.HolderIdentity == le.reportedLeader {
		return
	}
	le.reportedLeader = le.observedRecord.HolderIdentity
	if le.config.Callbacks.OnNewLeader != nil {
		go le.config.Callbacks.OnNewLeader(le.reportedLeader)
	}
}

// Check will determine if the current lease is expired by more than timeout.
//
// In recovery mode, while the lease is lost and the elector is still trying
// to recover it, Check fails only if no attempt has started for more than
// LeaseDuration plus timeout, i.e. the election loop is stuck.
func (le *LeaderElector) Check(maxTolerableExpiredLease time.Duration) error {
	if !le.IsLeader() {
		// Currently not concerned with the case that we are hot standby
		return nil
	}
	le.observedRecordLock.RLock()
	lastObservation := le.observedTime
	lastAttempt := le.lastAttempt
	leaseDuration := le.config.LeaseDuration
	le.observedRecordLock.RUnlock()

	if le.recovery != nil && !le.gate.isOpen() {
		// Not leading, but expected to keep trying. Only a loop that has
		// stopped making attempts is unhealthy.
		if le.clock.Since(lastAttempt) > leaseDuration+maxTolerableExpiredLease {
			return fmt.Errorf("election loop for lease %s stopped attempting to renew leadership", le.config.Name)
		}
		return nil
	}

	// If we are more than timeout seconds after the lease duration that is past the timeout
	// on the lease renew. Time to start reporting ourselves as unhealthy. We should have
	// died but conditions like deadlock can prevent this. (See #70819)
	if le.clock.Since(lastObservation) > leaseDuration+maxTolerableExpiredLease {
		return fmt.Errorf("failed election to renew leadership on lease %s", le.config.Name)
	}

	return nil
}

// noteAttempt records that an acquire or renew attempt started at now.
func (le *LeaderElector) noteAttempt(now time.Time) {
	le.observedRecordLock.Lock()
	defer le.observedRecordLock.Unlock()
	le.lastAttempt = now
}

func (le *LeaderElector) isLeaseValid(now time.Time) bool {
	// Lock to safely read both the time and the record
	le.observedRecordLock.RLock()
	defer le.observedRecordLock.RUnlock()

	return le.observedTime.Add(time.Second * time.Duration(le.observedRecord.LeaseDurationSeconds)).After(now)
}

// setObservedRecord will set a new observedRecord and update observedTime to the current time.
// Protect critical sections with lock.
func (le *LeaderElector) setObservedRecord(observedRecord *rl.LeaderElectionRecord) {
	le.observedRecordLock.Lock()
	defer le.observedRecordLock.Unlock()

	le.observedRecord = *observedRecord
	le.observedTime = le.clock.Now()
}

// getObservedRecord returns observersRecord.
// Protect critical sections with lock.
func (le *LeaderElector) getObservedRecord() rl.LeaderElectionRecord {
	le.observedRecordLock.RLock()
	defer le.observedRecordLock.RUnlock()

	return le.observedRecord
}
