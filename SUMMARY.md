# Scheduler View Types: Subset Generation for Reduced Memory

## What We Did

We proved an end-to-end pattern where a Kubernetes component (the scheduler) generates
**view types** — struct definitions containing only the fields it actually needs — and
uses them in place of full API types to reduce informer cache memory.

### The Problem

The scheduler's PodTopologySpread plugin reads `Spec.Selector` from `ReplicaSet`, but the
standard informer caches the entire `apps/v1.ReplicaSet` object — dozens of fields the
scheduler never touches. At scale (tens of thousands of ReplicaSets), this wastes
significant memory.

### The Solution

We used `subset-gen` (a new code generator in `k8s.io/code-generator`) to generate a
**view type** that contains only the fields the scheduler needs:

```go
// Generated view type — only the fields the scheduler reads
type ReplicaSet struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec              ReplicaSetSpec `json:"spec,omitempty"`
}

type ReplicaSetSpec struct {
    Selector *metav1.LabelSelector `json:"selector"`
}
```

The generator reads a simple config:

```yaml
# pkg/scheduler/apis/views/apps/v1/config.yaml
ReplicaSet:
  - spec.selector
```

And produces the view type plus all the generated code needed to use it:
types, deepcopy, register, clientset, listers, and informers.

### How It's Wired Up

The PodTopologySpread plugin creates a **view informer factory** backed by a view
clientset. The view informer talks to the same API server endpoint (`/apis/apps/v1/replicasets`)
but deserializes responses into the view type — automatically dropping all unused fields
before storing objects in the informer cache.

```
API Server  ──GET /apis/apps/v1/replicasets──►  View Clientset
                                                     │
                                              deserialize into
                                              view type (subset)
                                                     │
                                                     ▼
                                              View Informer Cache
                                              (only Selector stored)
                                                     │
                                                     ▼
                                              View ReplicaSetLister
                                                     │
                                              PodTopologySpread plugin
                                              reads rs.Spec.Selector
```

## Files Changed

### New: View type generation pipeline
- `pkg/scheduler/apis/views/apps/v1/config.yaml` — subset-gen config
- `pkg/scheduler/apis/views/hack/update-codegen.sh` — standalone codegen script
- `pkg/scheduler/apis/views/apps/v1/` — generated types, deepcopy, register
- `pkg/scheduler/apis/views/clientset/` — generated view clientset
- `pkg/scheduler/apis/views/listers/` — generated view listers
- `pkg/scheduler/apis/views/informers/` — generated view informers

### Modified: Scheduler wiring
- `pkg/scheduler/framework/plugins/helper/spread.go` — `DefaultSelector` accepts view lister
- `pkg/scheduler/framework/plugins/podtopologyspread/plugin.go` — creates view informer factory
- `pkg/scheduler/framework/plugins/helper/spread_test.go` — uses view types in tests
- `pkg/scheduler/framework/plugins/podtopologyspread/scoring_test.go` — uses view types in tests

### Modified: Codegen infrastructure
- `hack/update-codegen.sh` — `codegen::subsets` generates scheduler views

### Removed: Prototype
- `staging/src/k8s.io/subset-client/` — replaced by in-tree view generation

## Benchmarks

### Reproduction

```bash
go test -bench=BenchmarkReplicaSet -benchmem -count=3 \
    ./pkg/scheduler/apis/views/apps/v1/...
```

Benchmark file: `pkg/scheduler/apis/views/apps/v1/memory_test.go`

### Results

#### Heap Memory (cache.Store with realistic objects)

| Scale | Type | Heap per Object | Total Heap | Savings |
|-------|------|----------------|------------|---------|
| 1,000 | Full | 9,634 B | 9.2 MiB | — |
| 1,000 | View | 2,290 B | 2.2 MiB | **76.2%** |
| 10,000 | Full | 9,639 B | 91.9 MiB | — |
| 10,000 | View | 2,281 B | 21.7 MiB | **76.3%** |

At 10,000 ReplicaSets, view types save **~70 MiB** of heap memory.

#### Per-Object Construction (allocations)

| Type | Struct Size | Allocs/Object | Bytes/Object |
|------|-------------|--------------|-------------|
| Full | 1,144 B | 60 | 9,512 B |
| View | 272 B | 26 | 2,161 B |

The view type is **76% smaller** in shallow struct size and uses **77% fewer** allocated
bytes per object (including all referenced heap data like maps, slices, strings).

#### Allocation Overhead (cache.Store insert)

The cache.Store itself adds ~1,027 allocs per 1,000 objects for map bookkeeping. This
overhead is identical for both types — the savings come entirely from the objects stored,
not the store machinery.

### Methodology

Each benchmark constructs "realistic" ReplicaSet objects:
- **ObjectMeta**: name, namespace, UID, resourceVersion, 4 labels, 4 annotations,
  1 ownerReference, 2 managedFields entries with FieldsV1 data
- **Full type adds**: Spec.Replicas, Spec.Template (PodTemplateSpec with 2 containers,
  each with env vars, resource requests/limits, volume mounts, probes; 3 volumes),
  Status (replica counts, observedGeneration, conditions)
- **View type**: same ObjectMeta + only Spec.Selector (3 matchLabels)

The heap benchmark uses `runtime.ReadMemStats` with forced GC before/after to measure
actual retained heap. The alloc benchmark uses standard `b.ReportAllocs()`.

### Existing relevant benchmarks

The PodTopologySpread scoring benchmark exercises the plugin's full path:

```bash
go test -bench=BenchmarkTestPodTopologySpreadScore -benchmem \
    ./pkg/scheduler/framework/plugins/podtopologyspread/...
```

The scheduler cache benchmark operates at scale (1k nodes, 30k pods):

```bash
go test -bench=BenchmarkUpdate1kNodes30kPods -benchmem \
    ./pkg/scheduler/backend/cache/...
```

## What's Next

- **More view types**: The same pattern works for StatefulSet (also only `Spec.Selector`),
  Services (only `Spec.Selector`), and any other type where the scheduler reads a small
  subset of fields.
- **Server-side field selection**: Future API server support for returning only requested
  fields would compound the savings — less data over the wire AND less data in cache.
- **Other components**: Any controller or operator that reads a subset of fields from a
  resource type can use this pattern to reduce memory.
