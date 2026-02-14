# Reducing managedFields Memory Overhead in the Watch Cache

## Problem

`managedFields` is a major contributor to kube-apiserver watch cache memory utilization. It can nearly double the size of stored objects, yet Server-Side Apply adoption remains low. The watch cache never uses managedFields for any internal operation (indexing, filtering, predicate matching). It carries them purely as pass-through payload for API response backward compatibility.

## Where Memory Goes

The watch cache stores **deserialized Go structs**, not pre-serialized bytes. `cachingObject` serialization caching is ephemeral (scoped to `dispatchEvent`, then GC'd). Memory is dominated by:

| Component | Description |
|-----------|-------------|
| **Store** | One `runtime.Object` per resource. `ObjectMeta.ManagedFields` included. |
| **Ring buffer** | Up to 102,400 `watchCacheEvent` entries. Each holds `Object` + `PrevObject` pointers that keep old Go struct versions alive on the heap. |
| **Snapshots** | B-tree clones (ListFromCacheSnapshot). Reference same objects as store. |

For 100K Pods with ~10KB managedFields each: store ~2GB, ring buffer ~2GB. Without managedFields: ~1GB + ~1GB. managedFields roughly doubles the footprint.

Beyond managedFields, **strings are also duplicated** across objects. Every Pod in a namespace carries its own copy of the namespace string, every Pod with the same labels carries its own copies of the label keys and values, etc. While individually small, these add up across 100K+ objects and 100K+ ring buffer entries.

## Key Observation

managedFields are only **needed** in two places:

1. **Apply handler** -- reads/writes managedFields via the storage layer (etcd), not the watch cache.
2. **Clients reading API responses** -- backward compatibility. Only clients doing Extract/Modify/Apply workflows actually use the data.

The watch cache itself never reads managedFields. They are dead weight in memory.

## Design

Three complementary mechanisms, deployable in phases.

### Mechanism 1: Interning (memory optimization — low-risk)

Two levels of interning, applied in `processEvent` and `Replace`:

**a) managedFields slice sharing (Phase 0):** Compare the managedFields of the previous and current versions of the same object. If byte-equal, replace the current object's managedFields slice with the previous object's pointer. This deduplicates across the *temporal* dimension — consecutive versions of the same object in the ring buffer sharing identical managedFields.

**b) String interning via `unique.Make` (Phase 0b):** Use Go 1.23's `unique.Make` to intern string fields within managedFields entries (Manager, APIVersion, FieldsType, Subresource, Operation). These strings are highly repetitive across *all* objects of the same kind — e.g., 100K Pods all have Manager="kubectl", APIVersion="v1". `unique.Make` deduplicates across the *cross-object* dimension. This can also be applied to other high-value string fields (Namespace, common label/annotation keys) for additional savings beyond managedFields.

These two levels are complementary:
- Slice sharing eliminates the entire managedFields allocation when unchanged across versions.
- String interning reduces the residual cost when managedFields *are* different, and benefits fields beyond managedFields.

### Mechanism 2: Separated managedFields Store (memory optimization)

Strip managedFields from objects in the watch cache. Store them in a **separate, content-addressed side store** within the cacher.

```
Watch Cache (objects with ManagedFields = nil)
  Store:       100K Pods x ~10KB = ~1GB
  Ring buffer: 102K events x ~10KB = ~1GB

managedFields Side Store (content-addressed)
  Key:   (object key, resourceVersion)
  Value: pointer to deduplicated managedFields
  ~500 distinct values x ~10KB = ~5MB
```

**Why content-addressed:** managedFields rarely change across updates (status updates, label changes, annotation edits don't touch them). Across the ring buffer, thousands of versions of the same object share identical managedFields. Further, objects of the same kind created by the same controller (e.g., all Pods from one Deployment) share near-identical managedFields. Content-addressing deduplicates both dimensions.

**Hydration path:** When serializing an API response, look up managedFields from the side store by object key + resourceVersion and inject them into the serialized output. The API response is byte-identical to today.

**Lifecycle:** The side store is keyed by resourceVersion. When the ring buffer evicts an event (startIndex advances), the corresponding side store entry's refcount drops. When no ring buffer event or store entry references a managedFields value, it is removed.

### Mechanism 3: `excludeManagedFields` Query Parameter (wire optimization)

Add an **additive** query parameter `excludeManagedFields=true` to GET, LIST, and WATCH requests. Default is `false` (GA backward compatible -- no change to existing behavior).

When a client opts out:
- The serialization path skips managedFields hydration entirely -- no side store lookup, no serialization cost, smaller response.
- Watch events are smaller, meaning the ring buffer's event history covers a longer time window at the same memory budget, or equivalently, the history window can be shortened for the same coverage.

**Internal controller opt-out:** kube-controller-manager, kube-scheduler, kube-proxy, and other in-tree controllers never use managedFields. They can adopt `excludeManagedFields=true` immediately via client-go changes, reducing wire bandwidth for the highest-volume watchers.

## Phased Rollout

| Phase | Change | Benefit |
|-------|--------|---------|
| **0** | managedFields slice interning in `processEvent` (compare prev/cur, share pointer if equal) | 90% ring buffer pointer sharing. Simple, low-risk, immediate. |
| **0b** | String interning via `unique.Make` for managedFields string fields + apply interning in `Replace` | Cross-object string dedup. Covers initial list/relist gap. |
| **1** | Separated side store with content-addressed deduplication | Watch cache memory ~= "as if managedFields didn't exist." |
| **2** | `excludeManagedFields` query parameter (additive API) | Wire savings for opted-out clients. Serialization skipped entirely. |
| **3** | In-tree controllers opt out via client-go | Majority of watch traffic stops carrying managedFields on the wire. |
| **4** | client-go defaults to `excludeManagedFields=true` | Ecosystem-wide wire reduction. Clients needing managedFields explicitly request them. |

Phase 0+0b are simple, self-contained changes in `watch_cache.go` that can ship immediately while the larger architecture is developed.

## Feature Gating

**Status: Implemented**

All interning logic (Phase 0 + 0b) is gated behind the `WatchCacheManagedFieldsInterning` feature gate. This allows:

- **A/B comparison** without code changes: disable the gate to measure baseline, enable to measure with interning.
- **Production safety**: the gate can be disabled if unexpected issues arise.
- **Gradual rollout**: starts as Beta (default true) in 1.36.

### Files changed

- `staging/src/k8s.io/apiserver/pkg/features/kube_features.go`
  - Added `WatchCacheManagedFieldsInterning` feature constant.
  - Added versioned spec: `{Version: 1.36, Default: true, PreRelease: Beta}`.
- `staging/src/k8s.io/apiserver/pkg/storage/cacher/watch_cache.go`
  - Gated all three interning call sites with `utilfeature.DefaultFeatureGate.Enabled(features.WatchCacheManagedFieldsInterning)`.
- `staging/src/k8s.io/apiserver/pkg/storage/cacher/watch_cache_test.go`
  - `TestProcessEventInternsManagedFields` and `TestReplaceInternsManagedFieldsStrings` explicitly enable the gate.
  - `TestWatchCacheMemoryInterning` toggles the gate to compare enabled vs disabled heap usage.

## Phase 0: managedFields Slice Interning

**Status: Implemented**

In `watchCache.processEvent`, after fetching the previous object from the store, compare managedFields. If equal, replace the new object's managedFields with the old object's pointer:

```go
// watch_cache.go, in processEvent, inside the `if exists` block
internManagedFields(previousElem.Object, elem.Object)
```

Comparison: field-by-field on ManagedFieldsEntry (Manager, Operation, APIVersion, FieldsType, Subresource strings + `bytes.Equal` on FieldsV1.Raw + Time equality). This is safe because managedFields are never mutated in-place after deserialization.

### Files changed

- `staging/src/k8s.io/apiserver/pkg/storage/cacher/watch_cache.go`
  - Added `internManagedFields(previous, current runtime.Object)` — extracts ObjectMeta from both objects, compares managedFields, and shares the pointer if equal.
  - Added `managedFieldsEqual(a, b []metav1.ManagedFieldsEntry) bool` — direct field-by-field comparison avoiding reflection.
  - Called `internManagedFields` in `processEvent` after fetching the previous object from the store.
- `staging/src/k8s.io/apiserver/pkg/storage/cacher/watch_cache_test.go`
  - `TestManagedFieldsEqual` — table-driven tests for the comparison function.
  - `TestInternManagedFields` — unit tests for pointer sharing behavior.
  - `TestProcessEventInternsManagedFields` — integration test through `processEvent`.

### Known gap

Phase 0 only fires when a previous version exists in the store (`if exists`). On initial Add events and during `Replace` (relist), there is no previous version to compare against, so no interning occurs. Phase 0b addresses this.

## Phase 0b: String Interning and Replace Coverage

**Status: Implemented**

Two changes:

### 1. Use `unique.Make` for managedFields string fields

Added `internManagedFieldsStrings(obj runtime.Object)` which uses Go's `unique` package to intern string fields within managedFields entries. This deduplicates strings across all objects globally, not just across versions of the same object.

### 2. Apply interning in both `processEvent` and `Replace`

- `processEvent`: calls `internManagedFieldsStrings` on `elem.Object` unconditionally (before the `if exists` check), so it applies to both Add and Update events.
- `Replace`: calls `internManagedFieldsStrings` on each object during the replace loop, covering initial list and relist.

### Files changed

- `staging/src/k8s.io/apiserver/pkg/storage/cacher/watch_cache.go`
  - Added `internManagedFieldsStrings(obj runtime.Object)` — interns Manager, APIVersion, FieldsType, Subresource, and Operation strings via `unique.Make`.
  - Called in `processEvent` before the store lookup (unconditional — covers Add and Update).
  - Called in `Replace` for each object in the replace loop.
- `staging/src/k8s.io/apiserver/pkg/storage/cacher/watch_cache_test.go`
  - `TestInternManagedFieldsStrings` — verifies strings from different objects share backing memory after interning.
  - `TestInternManagedFieldsStringsNilManagedFields` — verifies no panic on nil managedFields.
  - `TestReplaceInternsManagedFieldsStrings` — integration test verifying Replace interns strings across objects.

### Why not full reflection-based interning?

A colleague prototyped a generic `internStrings(obj interface{})` that walks the entire object tree via reflection and interns every string with `unique.Make`. While this captures more strings (labels, annotations, namespace, etc.), it has drawbacks:

- **Reflection overhead per event:** Every processEvent and Replace iteration pays the cost of a full object tree walk. For high-churn resources this adds up.
- **Map handling complexity:** Interning map keys requires delete-and-reinsert (~60 lines of complex code), creating allocations that partially offset the savings.
- **Diminishing returns:** The highest-value strings (managedFields fields) are a small number of well-known fields. Targeted interning captures most of the benefit at a fraction of the cost.

The targeted approach can be extended incrementally if profiling shows other fields warrant interning.

## Benchmarks (Phase 0+0b)

**Status: Implemented** — see [BENCHMARKS.md](BENCHMARKS.md) for full results and raw output.

### Measured results (1000 pods × 10 versions)

| Metric | With interning | Without interning |
|--------|---------------|-------------------|
| Heap used by cache | 37 MB | 89 MB |
| Ring buffer pointer sharing | 90% | 0% |
| **Memory saved** | **52 MB (58%)** | — |

### CPU overhead

| Operation | Per-event cost | % of processEvent |
|-----------|---------------|-------------------|
| managedFieldsEqual comparison | ~120 ns | ~1.2% |
| internManagedFieldsStrings | ~450 ns | ~4.3% |
| **Total interning overhead** | **~570 ns** | **~5.5%** |

### Benchmarks and tests added

- `BenchmarkManagedFieldsEqual` — comparison cost (equal, different string, different FieldsV1).
- `BenchmarkInternManagedFieldsStrings` — `unique.Make` cost per object.
- `BenchmarkProcessEvent` — end-to-end processEvent throughput (same/different/no managedFields).
- `BenchmarkReplace` — bulk Replace with 1000 pods (with/without managedFields).
- `TestWatchCacheMemoryInterning` — heap measurement comparing interning vs. no interning, reports memory savings and pointer sharing ratio.

## Phase 1: Side Store Implementation

Key changes to `staging/src/k8s.io/apiserver/pkg/storage/cacher/`:

1. **On event ingestion** (`processEvent`): strip managedFields from the object, store them in the side store keyed by `(key, resourceVersion)`, with content-addressing for deduplication.
2. **On API response serialization** (`convertToWatchEvent` / `CacheEncode`): hydrate managedFields from the side store before encoding.
3. **On ring buffer eviction** (`updateCache` when `startIndex++`): release the side store reference for the evicted event.
4. **On Replace** (relist): rebuild the side store from the fresh object set.

The side store is internal to the cacher -- no API changes, no storage format changes, no migration.

## Phase 2: `excludeManagedFields` Query Parameter

**Status: Implemented**

Adds an opt-in `excludeManagedFields=true` query parameter to GET, LIST, and WATCH requests. When set, managedFields are omitted from responses entirely — no hydration, no serialization cost, smaller wire payloads. Default is `false`, existing behavior unchanged.

### Approach

Rather than modifying `metav1.ListOptions` / `metav1.GetOptions` (which would require protobuf regeneration and API review), `excludeManagedFields` is parsed directly from the URL query string — similar to the deprecated `export` parameter. The flag is passed through context to the storage layer.

### Feature Gate

`ExcludeManagedFields` — Alpha, default false, 1.36. When disabled, the query parameter is silently ignored.

### Files changed

- `staging/src/k8s.io/apiserver/pkg/features/kube_features.go`
  - Added `ExcludeManagedFields` feature constant and versioned spec (Alpha, default false, 1.36).
- `staging/src/k8s.io/apiserver/pkg/endpoints/request/context.go`
  - Added `excludeManagedFieldsKey` to the iota block.
  - Added `WithExcludeManagedFields(ctx)` and `ExcludeManagedFieldsFrom(ctx)` helpers.
- `staging/src/k8s.io/apiserver/pkg/endpoints/handlers/get.go`
  - `getResourceHandler`: parses `excludeManagedFields=true` from URL, sets context flag, strips MF from GET result.
  - `ListResource`: parses `excludeManagedFields=true` from URL, sets context flag, strips MF from LIST result. Watch path carries the flag via context.
  - Added `clearManagedFields` and `clearManagedFieldsFromList` helper functions.
- `staging/src/k8s.io/apiserver/pkg/storage/cacher/managed_fields_store.go`
  - Added `clearManagedFields(obj)` and `clearManagedFieldsFromList(listObj)` helpers.
- `staging/src/k8s.io/apiserver/pkg/storage/cacher/cacher.go`
  - `Get`: skips `HydrateManagedFields` when `ExcludeManagedFieldsFrom(ctx)` is true.
  - `GetList`: skips `HydrateManagedFields` for each item when `ExcludeManagedFieldsFrom(ctx)` is true.
  - `Watch`: sets `excludeManagedFields` on `cacheWatcher` when context flag is set.
- `staging/src/k8s.io/apiserver/pkg/storage/cacher/cache_watcher.go`
  - Added `excludeManagedFields` field to `cacheWatcher`.
  - Added `getMutableObjectExcludingManagedFields` — unwraps CacheableObject wrappers and returns plain objects without MF.
  - Updated `convertToWatchEvent` to use the exclude path for Added/Modified/Deleted events.

### Watch path design

When `excludeManagedFields` is true, `convertToWatchEvent` returns plain `runtime.Object` instances (not `CacheableObject` wrappers) with managedFields stripped. This is necessary because `hydratingObject.CacheEncode` injects managedFields before serialization and the result is cached per `runtime.Identifier` — sharing it across excludeMF and non-excludeMF watchers would produce incorrect output. The trade-off is that excludeMF watchers lose CacheEncode caching (each serializes independently), but the bandwidth savings from omitting MF (~40-50% smaller events) far outweighs this.

### Tests added

- `TestClearManagedFields` / `TestClearManagedFieldsFromList` — unit tests for strip helpers (managed_fields_store_test.go).
- `TestConvertToWatchEventExcludeManagedFields` — Added events with hydratingObject and cachingObject return plain objects without MF (managed_fields_store_test.go).
- `TestConvertToWatchEventExcludeManagedFieldsDelete` — Delete events preserve correct resourceVersion (managed_fields_store_test.go).
- `TestCacheWatcherExcludeManagedFieldsInitialEvents` — Initial events from ring buffer omit MF (managed_fields_store_test.go).
- `TestClearManagedFieldsHelper` / `TestClearManagedFieldsFromListHelper` — handler-level strip helpers (get_test.go).
- `TestExcludeManagedFieldsQueryParam` — parameter parsed from URL with feature gate enabled/disabled (get_test.go).
- `TestExcludeManagedFieldsFeatureGateDisabled` — parameter ignored when gate disabled (get_test.go).

## Expected Impact

| Metric | Phase 0+0b (measured) | Phase 0+0b+1 | Phase 0+0b+1+2+3 |
|--------|----------------------|--------------|-------------------|
| Watch cache memory | **58% reduction** (89→37 MB at 1K pods; projects to ~5.2 GB saved at 100K pods) | ~50% total reduction (store + ring buffer) | ~50% total reduction |
| CPU overhead | ~570 ns/event (~5.5% of processEvent) | slight increase (hydration on serialize) | net decrease (opted-out clients skip) |
| Wire bandwidth (internal controllers) | unchanged | unchanged | ~40-50% reduction |
| Wire bandwidth (external clients) | unchanged | unchanged | opt-in reduction |

## Open Questions

1. **Hydration granularity:** Should hydration happen at the Go struct level (set managedFields before encoding) or at the serialized byte level (splice managedFields bytes into the encoded output)? Byte-level splicing avoids constructing Go objects but is more complex.
2. **Side store data structure:** Simple map with refcounting vs. content-addressed pool with hash-based dedup. The latter is more memory-efficient but adds hashing overhead per event.
3. ~~**Feature gating:** Should the side store be behind a feature gate, or is it purely an internal optimization that can be enabled unconditionally?~~ **Resolved:** Phase 0+0b gated behind `WatchCacheManagedFieldsInterning` (Beta, default true, 1.36). Future phases can reuse the same gate or introduce separate ones.
4. **Broader string interning scope:** Should `unique.Make` interning be extended beyond managedFields to other high-value fields (Namespace, label keys, annotation keys)? Profiling data needed to justify the added complexity.
