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

with open("staging/src/k8s.io/client-go/tools/cache/reflector.go", "w") as f:
    f.write(content)

with open("staging/src/k8s.io/client-go/tools/cache/watchlist_memory_store.go", "r") as f:
    store_content = f.read()

old_store_struct = """type watchListMemoryOptimizedStore struct {
	Store       // embedded delegate
	clientStore Store
	keyFunc     KeyFunc
}

func newWatchListMemoryOptimizedStore(delegate Store, clientStore Store, keyFunc KeyFunc) Store {
	// keyFunc should match the delegate store's keying function.
	if clientStore == nil {
		return delegate
	}
	return &watchListMemoryOptimizedStore{
		Store:       delegate,
		clientStore: clientStore,
		keyFunc:     keyFunc,
	}
}"""

new_store_struct = """type watchListMemoryOptimizedStore struct {
	Store       // embedded delegate
	clientStore Store
	keyFunc     KeyFunc
	transformer TransformFunc
}

func newWatchListMemoryOptimizedStore(delegate Store, clientStore Store, keyFunc KeyFunc, transformer TransformFunc) Store {
	// keyFunc should match the delegate store's keying function.
	if clientStore == nil {
		// If there is no clientStore, we don't reuse objects.
		// We still need to apply the transformer if provided, so we can wrap it
		// or just let the caller use WithTransformer.
		// However, in reflector.go we omitted WithTransformer, so we must wrap it.
	}
	return &watchListMemoryOptimizedStore{
		Store:       delegate,
		clientStore: clientStore,
		keyFunc:     keyFunc,
		transformer: transformer,
	}
}"""

old_store_add = """func (s *watchListMemoryOptimizedStore) Add(obj interface{}) error {
	return s.Store.Add(s.maybeReuseObject(obj))
}

func (s *watchListMemoryOptimizedStore) Update(obj interface{}) error {
	return s.Store.Update(s.maybeReuseObject(obj))
}

func (s *watchListMemoryOptimizedStore) maybeReuseObject(obj interface{}) interface{} {
	key, err := s.keyFunc(obj)
	if err != nil {
		return obj
	}
	cached, exists, err := s.clientStore.GetByKey(key)
	if err != nil || !exists {
		return obj
	}
	if sameResourceVersion(cached, obj) {
		return cached
	}
	return obj
}"""

new_store_add = """func (s *watchListMemoryOptimizedStore) Add(obj interface{}) error {
	reused, reusedObj := s.maybeReuseObject(obj)
	if reused {
		return s.Store.Add(reusedObj)
	}
	if s.transformer != nil {
		var err error
		obj, err = s.transformer(obj)
		if err != nil {
			return err
		}
	}
	return s.Store.Add(obj)
}

func (s *watchListMemoryOptimizedStore) Update(obj interface{}) error {
	reused, reusedObj := s.maybeReuseObject(obj)
	if reused {
		return s.Store.Update(reusedObj)
	}
	if s.transformer != nil {
		var err error
		obj, err = s.transformer(obj)
		if err != nil {
			return err
		}
	}
	return s.Store.Update(obj)
}

func (s *watchListMemoryOptimizedStore) maybeReuseObject(obj interface{}) (bool, interface{}) {
	if s.clientStore == nil {
		return false, obj
	}
	key, err := s.keyFunc(obj)
	if err != nil {
		return false, obj
	}
	cached, exists, err := s.clientStore.GetByKey(key)
	if err != nil || !exists {
		return false, obj
	}
	if sameResourceVersion(cached, obj) {
		return true, cached
	}
	return false, obj
}"""

if old_store_struct in store_content:
    store_content = store_content.replace(old_store_struct, new_store_struct)
else:
    print("Could not find old store struct")

if old_store_add in store_content:
    store_content = store_content.replace(old_store_add, new_store_add)
else:
    print("Could not find old store add")

with open("staging/src/k8s.io/client-go/tools/cache/watchlist_memory_store.go", "w") as f:
    f.write(store_content)

print("Updated watchlist_memory_store.go")
