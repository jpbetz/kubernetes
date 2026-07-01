/*
Copyright 2022 The Kubernetes Authors.

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

package cached

import (
	"crypto/sha512"
	"fmt"
	"sync"

	"weak"
)

// WeakByteBuilder produces the serialized bytes for the current generation. It
// must be deterministic within a generation so the resident etag stays valid
// across a reclaim and rebuild.
type WeakByteBuilder func() ([]byte, error)

// WeakByteCache keeps a content etag resident and holds the serialized bytes
// behind a weak.Pointer, reclaimable under memory pressure and rebuilt on demand.
// A cheap source etag identifies the content generation: while it is unchanged
// Etag serves 304s without a rebuild and Get rebuilds only after a reclaim; when
// it changes the next access rebuilds. Static content uses a constant source etag.
type WeakByteCache struct {
	mu      sync.Mutex // serializes rebuilds (single-flight) and guards the fields below
	srcEtag func() (string, error)
	build   WeakByteBuilder

	wp       weak.Pointer[[]byte]
	wireEtag string // resident etag of the last-built bytes
	builtFor string // source etag the resident bytes correspond to
	built    bool
	err      error
}

// NewWeakByteCache caches static content: built once, then held only weakly.
func NewWeakByteCache(build WeakByteBuilder) *WeakByteCache {
	return NewWeakByteCacheWithSource(func() (string, error) { return "", nil }, build)
}

// NewWeakByteCacheWithSource caches content whose generation is identified by
// srcEtag. srcEtag must be cheap (called on every Get/Etag); build performs the
// deterministic serialization for the current generation.
func NewWeakByteCacheWithSource(srcEtag func() (string, error), build WeakByteBuilder) *WeakByteCache {
	c := &WeakByteCache{srcEtag: srcEtag, build: build}
	c.mu.Lock()
	if cur, err := c.srcEtag(); err == nil {
		_, _ = c.rebuildLocked(cur)
	}
	c.mu.Unlock()
	return c
}

// Etag returns the resident etag without materializing bytes, or false when the
// generation changed (a rebuild is needed) or nothing has been built yet.
func (c *WeakByteCache) Etag() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, err := c.srcEtag()
	if err != nil {
		return "", false
	}
	if c.built && cur == c.builtFor {
		return c.wireEtag, true
	}
	return "", false
}

// Get returns the bytes and etag, rebuilding if the generation changed or the
// bytes were reclaimed.
func (c *WeakByteCache) Get() ([]byte, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, err := c.srcEtag()
	if err != nil {
		return nil, "", err
	}
	if c.built && cur == c.builtFor {
		if p := c.wp.Value(); p != nil {
			return *p, c.wireEtag, c.err
		}
	}
	// Return the built bytes directly; re-reading the weak pointer could race a
	// GC reclaim and return nil.
	b, err := c.rebuildLocked(cur)
	if err != nil {
		return nil, c.wireEtag, err
	}
	return b, c.wireEtag, nil
}

func (c *WeakByteCache) rebuildLocked(srcEtag string) ([]byte, error) {
	b, err := c.build()
	c.err = err
	if err != nil {
		return nil, err
	}
	c.wireEtag = fmt.Sprintf("%X", sha512.Sum512(b)) // %X matches handler.computeETag
	c.builtFor = srcEtag
	c.built = true
	bb := b
	c.wp = weak.Make(&bb)
	return b, nil
}
