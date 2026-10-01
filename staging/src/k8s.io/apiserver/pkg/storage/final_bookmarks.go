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
)

type FinalBookmarkSender interface {
	SendFinalBookmark(ctx context.Context) error
}

type FinalBookmarks struct {
	lock    sync.Mutex
	senders map[FinalBookmarkSender]struct{}
}

func NewFinalBookmarks() *FinalBookmarks {
	return &FinalBookmarks{senders: map[FinalBookmarkSender]struct{}{}}
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

	var sentCount, failedCount atomic.Int64
	var wg sync.WaitGroup
	for _, s := range senders {
		wg.Go(func() {
			if err := s.SendFinalBookmark(ctx); err != nil {
				failedCount.Add(1)
			} else {
				sentCount.Add(1)
			}
		})
	}
	wg.Wait()
	return int(sentCount.Load()), int(failedCount.Load())
}
