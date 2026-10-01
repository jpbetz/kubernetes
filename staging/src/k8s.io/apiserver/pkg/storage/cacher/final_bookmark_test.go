/*
Copyright The Kubernetes Authors.

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

package cacher

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/apis/example"
	"k8s.io/apiserver/pkg/features"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/cacher/metrics"
	etcd3testing "k8s.io/apiserver/pkg/storage/etcd3/testing"
	etcdfeature "k8s.io/apiserver/pkg/storage/feature"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	clientfeatures "k8s.io/client-go/features"
	clientfeaturestesting "k8s.io/client-go/features/testing"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	k8smetrics "k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/testutil"
	"k8s.io/utils/clock"
	testingclock "k8s.io/utils/clock/testing"
	"k8s.io/utils/ptr"
)

func TestFinalBookmarks(t *testing.T) {
	forceRequestWatchProgressSupport(t)
	for _, tc := range []struct {
		name              string
		disabled          bool
		allowBookmarks    bool
		indexed           bool
		sendInitialEvents bool
		fillBuffer        bool
		notReady          bool
		oldEtcd           bool
		stop              bool
		registerStopped   bool
		apiServerID       string
		blockedMarker     bool
		wantSent          int
		wantFailed        int
		wantFinalBookmark bool
	}{
		{name: "watcher allows bookmarks", allowBookmarks: true, wantSent: 1, wantFinalBookmark: true},
		{name: "watcher does not allow bookmarks", wantSent: 1},
		{name: "indexed watcher", allowBookmarks: true, indexed: true, wantSent: 1, wantFinalBookmark: true},
		{name: "watcher receiving initial events", allowBookmarks: true, sendInitialEvents: true, wantSent: 1, wantFinalBookmark: true},
		{name: "watcher with a full buffer", allowBookmarks: true, fillBuffer: true, wantSent: 1},
		{name: "cacher not ready", allowBookmarks: true, notReady: true, wantFailed: 1},
		{name: "etcd without progress requests", allowBookmarks: true, oldEtcd: true, wantFailed: 1},
		{name: "cacher stopped", allowBookmarks: true, stop: true},
		{name: "cacher stopped after it was listed", allowBookmarks: true, stop: true, registerStopped: true, wantFailed: 1},
		{name: "feature disabled", disabled: true, allowBookmarks: true},
		{name: "shutdown marker written once to each etcd", allowBookmarks: true, apiServerID: "apiserver-test", wantSent: 3, wantFinalBookmark: true},
		{name: "shutdown marker write blocked on another etcd", allowBookmarks: true, apiServerID: "apiserver-test", blockedMarker: true, wantSent: 3, wantFailed: 1, wantFinalBookmark: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.WatchCacheShutdownBookmark, !tc.disabled)
			finalBookmarks := storage.NewFinalBookmarks(tc.apiServerID)
			setupOpts := []setupOption{withFinalBookmarks(finalBookmarks)}
			if tc.indexed {
				setupOpts = append(setupOpts, withNodeNameAndNamespaceIndex)
			}
			ctx, delegator, server, terminate := testSetupWithEtcdServer(t, setupOpts...)
			t.Cleanup(terminate)
			cacher := delegator.cacher
			var markerServers []*etcd3testing.EtcdTestServer
			if tc.apiServerID != "" {
				finalBookmarks.Register(&markerOnlySender{shutdownMarkerStore: cacher.storage.(shutdownMarkerStore)})
				_, _, otherServer, terminateOther := testSetupWithEtcdServer(t, withFinalBookmarks(finalBookmarks))
				t.Cleanup(terminateOther)
				markerServers = []*etcd3testing.EtcdTestServer{server, otherServer}
			}
			blocked := &blockedMarkerSender{release: make(chan struct{})}
			if tc.blockedMarker {
				finalBookmarks.Register(blocked)
			}

			initialEvents := 0
			if tc.sendInitialEvents {
				initialEvents = 25
				for range initialEvents - 1 {
					createObject(t, ctx, delegator)
				}
			}
			opts := storage.ListOptions{ResourceVersion: createObject(t, ctx, delegator), Predicate: storage.Everything, Recursive: true}
			opts.Predicate.AllowWatchBookmarks = tc.allowBookmarks
			if tc.indexed {
				opts.Predicate.Field = fields.OneTermEqualSelector("spec.nodeName", "")
				opts.Predicate.IndexFields = []string{"spec.nodeName"}
			}
			if tc.sendInitialEvents {
				opts.ResourceVersion = ""
				opts.SendInitialEvents = ptr.To(true)
			}
			w, err := cacher.Watch(ctx, "/pods/default", opts)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(w.Stop)

			added := 1
			if tc.fillBuffer {
				added = 2*cap(w.(*cacheWatcher).input) + 1
			}
			for range added {
				createObject(t, ctx, delegator)
			}
			for i := range 10 {
				if _, err := server.V3Client.Put(ctx, fmt.Sprintf("/unrelated/%d", i), "value"); err != nil {
					t.Fatal(err)
				}
			}
			current, err := cacher.storage.GetCurrentResourceVersion(ctx)
			if err != nil {
				t.Fatal(err)
			}

			if tc.notReady {
				cacher.ready.setError(errors.New("not ready"))
			}
			if tc.oldEtcd {
				resetFeatureSupportCheckerDuringTest(t)
			}
			if tc.stop {
				cacher.Stop()
			}
			if tc.registerStopped {
				finalBookmarks.Register(cacher)
			}
			sendCtx, cancel := context.WithTimeout(ctx, wait.ForeverTestTimeout)
			defer cancel()
			sendDone := make(chan [2]int, 1)
			go func() {
				sent, failed := finalBookmarks.Send(sendCtx)
				sendDone <- [2]int{sent, failed}
			}()
			checkSend := func() {
				t.Helper()
				select {
				case got := <-sendDone:
					if sendCtx.Err() != nil {
						t.Errorf("Send blocked until its context expired")
					}
					if got != [2]int{tc.wantSent, tc.wantFailed} {
						t.Errorf("Send() = %v, want [%d %d]", got, tc.wantSent, tc.wantFailed)
					}
				case <-time.After(2 * wait.ForeverTestTimeout):
					t.Fatal("Send did not return")
				}
			}
			if tc.stop {
				checkSend()
				return
			}
			if tc.wantFinalBookmark {
				if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, wait.ForeverTestTimeout, true, func(context.Context) (bool, error) {
					cacher.Lock()
					defer cacher.Unlock()
					return w.(*cacheWatcher).stopped, nil
				}); err != nil {
					t.Fatalf("the final bookmark did not end the watch: %v", err)
				}
			} else {
				checkSend()
			}

			next := func() watch.Event {
				t.Helper()
				select {
				case event, ok := <-w.ResultChan():
					if !ok {
						t.Fatal("watch closed")
					}
					return event
				case <-time.After(wait.ForeverTestTimeout):
					t.Fatal("timed out waiting for a watch event")
				}
				return watch.Event{}
			}
			for range initialEvents {
				if event := next(); event.Type != watch.Added {
					t.Fatalf("got %v, want an initial ADDED event", event.Type)
				}
			}
			if tc.sendInitialEvents {
				if event := next(); event.Type != watch.Bookmark || !isInitialEventsEndBookmark(t, event) {
					t.Fatalf("got %v, want the initial-events-end bookmark", event.Type)
				}
			}
			for range added {
				if event := next(); event.Type != watch.Added {
					t.Fatalf("got %v, want ADDED", event.Type)
				}
			}
			if tc.wantFinalBookmark {
				select {
				case <-sendDone:
					t.Fatal("Send returned before the watcher received the final bookmark")
				default:
				}
				event := next()
				if event.Type != watch.Bookmark || isInitialEventsEndBookmark(t, event) {
					t.Fatalf("got %v, want the final bookmark", event.Type)
				}
				finalRV := eventResourceVersion(t, event)
				if finalRV < current {
					t.Errorf("final bookmark at resourceVersion %d, want at least %d", finalRV, current)
				}
				select {
				case event, ok := <-w.ResultChan():
					if ok {
						t.Errorf("got %v after the final bookmark, want the watch to end", event.Type)
					}
				case <-time.After(wait.ForeverTestTimeout):
					t.Error("watch did not end after the final bookmark")
				}
				if tc.blockedMarker {
					close(blocked.release)
				}
				checkSend()
				for _, s := range markerServers {
					resp, err := s.V3Client.KV.Get(ctx, etcd3testing.PathPrefix()+"/apiserver_shutdown_marker/"+tc.apiServerID)
					if err != nil {
						t.Fatal(err)
					}
					if len(resp.Kvs) != 1 || resp.Kvs[0].Version != 1 {
						t.Errorf("got shutdown markers %v, want one marker written once", resp.Kvs)
					} else if s == server && uint64(resp.Kvs[0].ModRevision) > finalRV {
						t.Errorf("final bookmark at resourceVersion %d, want at least the shutdown marker's revision %d", finalRV, resp.Kvs[0].ModRevision)
					}
				}
				return
			}
			after := createObject(t, ctx, delegator)
			if event := next(); event.Type != watch.Added || strconv.FormatUint(eventResourceVersion(t, event), 10) != after {
				t.Errorf("got %v at resourceVersion %d, want ADDED at %s", event.Type, eventResourceVersion(t, event), after)
			}
		})
	}
}

func TestShutdownMarkerStart(t *testing.T) {
	forceRequestWatchProgressSupport(t)
	clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.WatchListClient, true)
	const apiServerID = "apiserver-test"
	for _, tc := range []struct {
		name          string
		disabled      bool
		noMarker      bool
		writesAfter   int
		maxRevisions  uint64
		compact       bool
		failExactList bool
		timeout       bool
		wantAtMarker  bool
	}{
		{name: "no marker", noMarker: true, writesAfter: 10},
		{name: "marker at the current revision"},
		{name: "marker within the bound", writesAfter: 10, maxRevisions: 12, wantAtMarker: true},
		{name: "marker beyond the bound", writesAfter: 10, maxRevisions: 11},
		{name: "marker compacted", writesAfter: 10, compact: true},
		{name: "list at the marker fails", writesAfter: 10, failExactList: true},
		{name: "catch-up times out", writesAfter: 10, timeout: true},
		{name: "feature disabled", disabled: true, writesAfter: 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.WatchCacheShutdownBookmark, !tc.disabled)
			ctx := context.Background()
			server, etcdStorage := newEtcdTestStorage(t, etcd3testing.PathPrefix())
			t.Cleanup(func() {
				if err := server.V3Client.Close(); err != nil {
					t.Error(err)
				}
			})
			markers := etcdStorage.(shutdownMarkerStore)
			podKey := func(name string) string {
				return computePodKey(&example.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name}})
			}
			createPod := func(name string) *example.Pod {
				out := &example.Pod{}
				if err := etcdStorage.Create(ctx, podKey(name), &example.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name}}, out, 0); err != nil {
					t.Fatal(err)
				}
				return out
			}

			createPod("old")
			createPod("stays")
			var marker uint64
			if !tc.noMarker {
				rev, err := markers.WriteShutdownMarker(ctx, apiServerID)
				if err != nil {
					t.Fatal(err)
				}
				marker = uint64(rev)
			}
			var deleted, added *example.Pod
			if tc.writesAfter > 0 {
				deleted = &example.Pod{}
				if err := etcdStorage.Delete(ctx, podKey("old"), deleted, nil, storage.ValidateAllObjectFunc, nil, storage.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
				added = createPod("new")
				for i := range tc.writesAfter {
					if _, err := server.V3Client.Put(ctx, fmt.Sprintf("/unrelated/%d", i), "value"); err != nil {
						t.Fatal(err)
					}
				}
			}
			current, err := etcdStorage.GetCurrentResourceVersion(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if tc.compact {
				if _, err := server.V3Client.Compact(ctx, int64(current)); err != nil {
					t.Fatal(err)
				}
			}

			wrapped := &markerTestStorage{Interface: etcdStorage, shutdownMarkerStore: markers, failExactList: tc.failExactList, noProgress: tc.timeout}
			if tc.failExactList {
				wrapped.failWatches.Store(1)
				metrics.WatchCacheInitializationErrors.Reset()
				if err := k8smetrics.NewKubeRegistry().Register(metrics.WatchCacheInitializationErrors); err != nil {
					t.Fatal(err)
				}
			}
			var cacherClock clock.WithTicker = clock.RealClock{}
			fakeClock := testingclock.NewFakeClock(time.Now())
			if tc.timeout {
				cacherClock = fakeClock
			}
			cacher, err := NewCacherFromConfig(Config{
				Storage:             wrapped,
				Versioner:           storage.APIObjectVersioner{},
				GroupResource:       schema.GroupResource{Resource: "pods"},
				EventsHistoryWindow: DefaultEventFreshDuration,
				ResourcePrefix:      "/pods/",
				KeyFunc:             func(obj runtime.Object) (string, error) { return storage.NamespaceKeyFunc("/pods/", obj) },
				GetAttrsFunc:        GetPodAttrs,
				NewFunc:             newPod,
				NewListFunc:         newPodList,
				Codec:               examplev1ProtoCodec,
				Clock:               cacherClock,
				FinalBookmarks:      storage.NewFinalBookmarks(apiServerID),
				maxReplayRevisions:  tc.maxRevisions,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cacher.Stop)
			cacheState := func() (rv, floor uint64) {
				cacher.watchCache.RLock()
				defer cacher.watchCache.RUnlock()
				return cacher.watchCache.resourceVersion, cacher.watchCache.storage.ListResourceVersion()
			}

			if tc.timeout {
				addedRV, err := strconv.ParseUint(added.ResourceVersion, 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, wait.ForeverTestTimeout, true, func(context.Context) (bool, error) {
					rv, _ := cacheState()
					return rv >= addedRV, nil
				}); err != nil {
					t.Fatalf("the watch cache did not replay the events after the marker: %v", err)
				}
				if _, err := cacher.ready.check(); err == nil {
					t.Fatal("the watch cache reported ready before it caught up")
				}
				if err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, wait.ForeverTestTimeout, true, func(context.Context) (bool, error) {
					fakeClock.Step(catchUpTimeout)
					_, err := cacher.ready.check()
					return err == nil, nil
				}); err != nil {
					t.Fatalf("the watch cache did not reinitialize after its catch-up timed out: %v", err)
				}
			}
			if err := cacher.Wait(ctx); err != nil {
				t.Fatal(err)
			}
			if tc.failExactList {
				if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, wait.ForeverTestTimeout, true, func(context.Context) (bool, error) {
					return wrapped.failWatches.Load() < 0, nil
				}); err != nil {
					t.Fatalf("the watch cache did not reinitialize: %v", err)
				}
				if err := cacher.Wait(ctx); err != nil {
					t.Fatal(err)
				}
				if got := wrapped.exactLists.Load(); got != 1 {
					t.Errorf("listed at the marker %d times, want 1", got)
				}
				if got, err := testutil.GetCounterMetricValue(metrics.WatchCacheInitializationErrors.WithLabelValues("", "pods")); err != nil || got != 1 {
					t.Errorf("initialization errors = %v, %v, want 1", got, err)
				}
			}
			var wantWatchLists int32
			if tc.disabled || tc.failExactList || tc.timeout {
				wantWatchLists = 1
			}
			if got := wrapped.watchLists.Load(); got != wantWatchLists {
				t.Errorf("got %d watch-list requests, want %d", got, wantWatchLists)
			}

			rv, floor := cacheState()
			if rv < current {
				t.Errorf("watch cache reported ready at resourceVersion %d, want at least %d", rv, current)
			}
			if tc.wantAtMarker && floor != marker {
				t.Errorf("watch cache starts at %d, want the marker's revision %d", floor, marker)
			}
			if !tc.wantAtMarker && floor < current {
				t.Errorf("watch cache starts at %d, want the latest revision %d or later", floor, current)
			}

			list := &example.PodList{}
			if err := cacher.GetList(ctx, "/pods/ns", storage.ListOptions{ResourceVersion: "0", Predicate: storage.Everything, Recursive: true}, list); err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, pod := range list.Items {
				names = append(names, pod.Name)
			}
			wantNames := []string{"old", "stays"}
			if tc.writesAfter > 0 {
				wantNames = []string{"new", "stays"}
			}
			if !slices.Equal(names, wantNames) {
				t.Errorf("list at resourceVersion 0 returned %v, want %v", names, wantNames)
			}

			if deleted == nil {
				return
			}
			w, err := cacher.Watch(ctx, "/pods/ns", storage.ListOptions{ResourceVersion: deleted.ResourceVersion, Predicate: storage.Everything, Recursive: true})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(w.Stop)
			select {
			case event := <-w.ResultChan():
				if tc.wantAtMarker {
					pod, ok := event.Object.(*example.Pod)
					if event.Type != watch.Added || !ok || pod.Name != "new" || pod.ResourceVersion != added.ResourceVersion {
						t.Errorf("got %v %#v, want ADDED new at %s", event.Type, event.Object, added.ResourceVersion)
					}
				} else if event.Type != watch.Error || !apierrors.IsResourceExpired(apierrors.FromObject(event.Object)) {
					t.Errorf("got %v %#v, want 410 Expired", event.Type, event.Object)
				}
			case <-time.After(wait.ForeverTestTimeout):
				t.Fatal("timed out waiting for a watch event")
			}
		})
	}
}

type markerOnlySender struct {
	shutdownMarkerStore
}

func (*markerOnlySender) SendFinalBookmark(context.Context) error {
	return nil
}

type blockedMarkerSender struct {
	release chan struct{}
}

func (*blockedMarkerSender) ShutdownMarkerScope() string {
	return "blocked"
}

func (s *blockedMarkerSender) WriteShutdownMarker(ctx context.Context, _ string) (int64, error) {
	select {
	case <-s.release:
	case <-ctx.Done():
	}
	return 0, errors.New("etcd unavailable")
}

func (*blockedMarkerSender) SendFinalBookmark(context.Context) error {
	return errors.New("etcd unavailable")
}

type markerTestStorage struct {
	storage.Interface
	shutdownMarkerStore
	failExactList bool
	noProgress    bool
	failWatches   atomic.Int32
	exactLists    atomic.Int32
	watchLists    atomic.Int32
}

func (s *markerTestStorage) GetList(ctx context.Context, key string, opts storage.ListOptions, listObj runtime.Object) error {
	if opts.ResourceVersionMatch == metav1.ResourceVersionMatchExact {
		s.exactLists.Add(1)
		if s.failExactList {
			return apierrors.NewInternalError(errors.New("cannot decode an old revision"))
		}
	}
	return s.Interface.GetList(ctx, key, opts, listObj)
}

func (s *markerTestStorage) Watch(ctx context.Context, key string, opts storage.ListOptions) (watch.Interface, error) {
	if ptr.Deref(opts.SendInitialEvents, false) {
		s.watchLists.Add(1)
	}
	if s.failWatches.Add(-1) >= 0 {
		return nil, errors.New("injected watch error")
	}
	return s.Interface.Watch(ctx, key, opts)
}

func (s *markerTestStorage) RequestWatchProgress(ctx context.Context) error {
	if s.noProgress {
		return nil
	}
	return s.Interface.RequestWatchProgress(ctx)
}

func isInitialEventsEndBookmark(t *testing.T, event watch.Event) bool {
	t.Helper()
	accessor, err := meta.Accessor(event.Object)
	if err != nil {
		t.Fatal(err)
	}
	return accessor.GetAnnotations()[metav1.InitialEventsAnnotationKey] == "true"
}

func eventResourceVersion(t *testing.T, event watch.Event) uint64 {
	t.Helper()
	accessor, err := meta.Accessor(event.Object)
	if err != nil {
		t.Fatal(err)
	}
	rv, err := strconv.ParseUint(accessor.GetResourceVersion(), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return rv
}

func resetFeatureSupportCheckerDuringTest(t *testing.T) {
	t.Helper()
	orig := etcdfeature.DefaultFeatureSupportChecker
	etcdfeature.DefaultFeatureSupportChecker = etcdfeature.NewDefaultFeatureSupportChecker()
	t.Cleanup(func() { etcdfeature.DefaultFeatureSupportChecker = orig })
}
