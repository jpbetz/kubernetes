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

package storage

import (
	"context"
	"sync"
	"sync/atomic"

	"k8s.io/klog/v2"
)

type FinalBookmarkSender interface {
	SendFinalBookmark(ctx context.Context) error
}

type shutdownMarkerWriter interface {
	ShutdownMarkerScope() string
	WriteShutdownMarker(ctx context.Context, apiServerID string) (int64, error)
}

type FinalBookmarks struct {
	apiServerID string
	lock        sync.Mutex
	senders     map[FinalBookmarkSender]struct{}
}

func NewFinalBookmarks(apiServerID string) *FinalBookmarks {
	return &FinalBookmarks{apiServerID: apiServerID, senders: map[FinalBookmarkSender]struct{}{}}
}

func (f *FinalBookmarks) APIServerID() string {
	return f.apiServerID
}

func (f *FinalBookmarks) Register(s FinalBookmarkSender) {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.senders[s] = struct{}{}
}

func (f *FinalBookmarks) Unregister(s FinalBookmarkSender) {
	f.lock.Lock()
	defer f.lock.Unlock()
	delete(f.senders, s)
}

func (f *FinalBookmarks) Send(ctx context.Context) (sent, failed int) {
	f.lock.Lock()
	senders := make([]FinalBookmarkSender, 0, len(f.senders))
	for s := range f.senders {
		senders = append(senders, s)
	}
	f.lock.Unlock()

	scopes := map[string][]FinalBookmarkSender{}
	for _, s := range senders {
		var scope string
		if w, ok := s.(shutdownMarkerWriter); ok && f.apiServerID != "" {
			scope = w.ShutdownMarkerScope()
		}
		scopes[scope] = append(scopes[scope], s)
	}

	var sentCount, failedCount atomic.Int64
	var wg sync.WaitGroup
	for scope, group := range scopes {
		wg.Go(func() {
			if scope != "" {
				if rev, err := group[0].(shutdownMarkerWriter).WriteShutdownMarker(ctx, f.apiServerID); err != nil {
					klog.InfoS("Could not write the shutdown marker", "apiServerID", f.apiServerID, "scope", scope, "err", err)
				} else {
					klog.V(1).InfoS("Wrote the shutdown marker", "apiServerID", f.apiServerID, "scope", scope, "revision", rev)
				}
			}
			for _, s := range group {
				wg.Go(func() {
					if err := s.SendFinalBookmark(ctx); err != nil {
						failedCount.Add(1)
					} else {
						sentCount.Add(1)
					}
				})
			}
		})
	}
	wg.Wait()
	return int(sentCount.Load()), int(failedCount.Load())
}
