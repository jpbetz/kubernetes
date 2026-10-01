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
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage/cacher/delegator"
	"k8s.io/klog/v2"
)

func (c *Cacher) SendFinalBookmark(ctx context.Context) error {
	result, err := c.sendFinalBookmark(ctx)
	if err != nil {
		klog.InfoS("Watch cache could not send its final bookmark", "group", c.groupResource.Group, "resource", c.groupResource.Resource, "err", err)
		return err
	}
	klog.V(2).InfoS("Watch cache sent its final bookmark", "group", c.groupResource.Group, "resource", c.groupResource.Resource,
		"resourceVersion", result.rv, "watchers", result.watchers, "dropped", result.dropped)
	return nil
}

type finalBookmarkRequest struct {
	rv   uint64
	done chan finalBookmarkResult
}

type finalBookmarkResult struct {
	rv       uint64
	watchers int
	dropped  int
	ended    []*cacheWatcher
}

func (c *Cacher) sendFinalBookmark(ctx context.Context) (finalBookmarkResult, error) {
	if _, err := c.ready.check(); err != nil {
		return finalBookmarkResult{}, err
	}
	if !delegator.ConsistentReadSupported() {
		return finalBookmarkResult{}, fmt.Errorf("etcd does not support progress requests")
	}
	rv, err := c.storage.GetCurrentResourceVersion(ctx)
	if err != nil {
		return finalBookmarkResult{}, fmt.Errorf("reading the current resource version: %w", err)
	}
	c.watchCache.config.waitingUntilFresh.Add()
	defer c.watchCache.config.waitingUntilFresh.Remove()
	req := &finalBookmarkRequest{rv: rv, done: make(chan finalBookmarkResult, 1)}
	select {
	case c.finalBookmarkRequests <- req:
	case <-ctx.Done():
		return finalBookmarkResult{}, ctx.Err()
	case <-c.stopCh:
		return finalBookmarkResult{}, fmt.Errorf("cacher stopped")
	}
	var result finalBookmarkResult
	select {
	case result = <-req.done:
	case <-ctx.Done():
		return finalBookmarkResult{}, fmt.Errorf("waiting to reach resource version %d: %w", rv, ctx.Err())
	case <-c.stopCh:
		return finalBookmarkResult{}, fmt.Errorf("cacher stopped")
	}
	if err := wait.PollUntilContextCancel(ctx, 10*time.Millisecond, true, func(context.Context) (bool, error) {
		for _, w := range result.ended {
			select {
			case <-w.done:
			default:
				return false, nil
			}
			if len(w.result) > 0 {
				return false, nil
			}
		}
		return true, nil
	}); err != nil {
		return result, fmt.Errorf("waiting for watchers to receive the final bookmark: %w", err)
	}
	return result, nil
}

func (c *Cacher) dispatchFinalBookmark(rv uint64) finalBookmarkResult {
	event := &watchCacheEvent{
		Type:            watch.Bookmark,
		Object:          c.newFunc(),
		ResourceVersion: rv,
	}
	if err := c.versioner.UpdateObject(event.Object, rv); err != nil {
		klog.Errorf("failure to set resourceVersion to %d on final bookmark event %+v", rv, event.Object)
		return finalBookmarkResult{rv: rv}
	}

	c.Lock()
	c.dispatching = true
	var watchers []*cacheWatcher
	add := func(ws watchersMap) {
		for _, w := range ws {
			if w.allowWatchBookmarks && !w.stopped {
				watchers = append(watchers, w)
			}
		}
	}
	for _, ws := range c.watchers.allWatchers {
		add(ws)
	}
	for _, ws := range c.watchers.valueWatchers {
		add(ws)
	}
	c.Unlock()
	defer c.finishDispatching()

	result := finalBookmarkResult{rv: rv, watchers: len(watchers)}
	for _, w := range watchers {
		if !w.nonblockingAdd(event) {
			result.dropped++
			continue
		}
		w.forget(true)
		result.ended = append(result.ended, w)
	}
	return result
}
