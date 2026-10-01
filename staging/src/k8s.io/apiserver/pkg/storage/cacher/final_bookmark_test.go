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
	"strconv"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/features"
	"k8s.io/apiserver/pkg/storage"
	etcdfeature "k8s.io/apiserver/pkg/storage/feature"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.WatchCacheShutdownBookmark, !tc.disabled)
			finalBookmarks := storage.NewFinalBookmarks()
			setupOpts := []setupOption{withFinalBookmarks(finalBookmarks)}
			if tc.indexed {
				setupOpts = append(setupOpts, withNodeNameAndNamespaceIndex)
			}
			ctx, delegator, server, terminate := testSetupWithEtcdServer(t, setupOpts...)
			t.Cleanup(terminate)
			cacher := delegator.cacher

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
				if rv := eventResourceVersion(t, event); rv < current {
					t.Errorf("final bookmark at resourceVersion %d, want at least %d", rv, current)
				}
				select {
				case event, ok := <-w.ResultChan():
					if ok {
						t.Errorf("got %v after the final bookmark, want the watch to end", event.Type)
					}
				case <-time.After(wait.ForeverTestTimeout):
					t.Error("watch did not end after the final bookmark")
				}
				checkSend()
				return
			}
			after := createObject(t, ctx, delegator)
			if event := next(); event.Type != watch.Added || strconv.FormatUint(eventResourceVersion(t, event), 10) != after {
				t.Errorf("got %v at resourceVersion %d, want ADDED at %s", event.Type, eventResourceVersion(t, event), after)
			}
		})
	}
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
