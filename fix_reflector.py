import sys

with open("staging/src/k8s.io/client-go/tools/cache/reflector.go", "r") as f:
    content = f.read()

old_reflector_opts = """	var transformer TransformFunc
	storeOpts := []StoreOption{}
	if tr, ok := r.store.(TransformingStore); ok && tr.Transformer() != nil {
		transformer = tr.Transformer()
		storeOpts = append(storeOpts, WithTransformer(transformer))
	}"""

new_reflector_opts = """	var transformer TransformFunc
	storeOpts := []StoreOption{}
	isMemoryOptimizationEnabled := clientfeatures.FeatureGates().Enabled(clientfeatures.WatchListMemoryOptimization)
	if tr, ok := r.store.(TransformingStore); ok && tr.Transformer() != nil {
		transformer = tr.Transformer()
		if !isMemoryOptimizationEnabled {
			storeOpts = append(storeOpts, WithTransformer(transformer))
		}
	}"""

if old_reflector_opts in content:
    content = content.replace(old_reflector_opts, new_reflector_opts)
else:
    print("Could not find old reflector opts")

old_store_init = """		temporaryStore = NewStore(DeletionHandlingMetaNamespaceKeyFunc, storeOpts...)
		// note when a transformer is configured, reusing cached objects may result in a double-transform.
		// TODO: check ^
		if clientfeatures.FeatureGates().Enabled(clientfeatures.WatchListMemoryOptimization) {
			temporaryStore = newWatchListMemoryOptimizedStore(temporaryStore, r.clientStore, DeletionHandlingMetaNamespaceKeyFunc)
		}"""

new_store_init = """		temporaryStore = NewStore(DeletionHandlingMetaNamespaceKeyFunc, storeOpts...)
		if isMemoryOptimizationEnabled {
			temporaryStore = newWatchListMemoryOptimizedStore(temporaryStore, r.clientStore, DeletionHandlingMetaNamespaceKeyFunc, transformer)
		}"""

if old_store_init in content:
    content = content.replace(old_store_init, new_store_init)
else:
    print("Could not find old store init")

old_replace = "err = r.store.Replace(temporaryStore.List(), lastKnownRV)"
new_replace = """if replaceTransformed, ok := r.store.(interface {
			ReplaceAlreadyTransformed(list []interface{}, rv string) error
		}); ok && transformer != nil {
			err = replaceTransformed.ReplaceAlreadyTransformed(temporaryStore.List(), lastKnownRV)
		} else {
			err = r.store.Replace(temporaryStore.List(), lastKnownRV)
		}"""

if old_replace in content:
    content = content.replace(old_replace, new_replace)
else:
    print("Could not find old Replace call")

with open("staging/src/k8s.io/client-go/tools/cache/reflector.go", "w") as f:
    f.write(content)
print("Updated reflector.go")
