import sys

with open("staging/src/k8s.io/client-go/tools/cache/watchlist_memory_store.go", "r") as f:
    content = f.read()

old_func = """func newWatchListMemoryOptimizedStore(delegate Store, clientStore Store, keyFunc KeyFunc, transformer TransformFunc) Store {
	// keyFunc should match the delegate store's keying function.
	if clientStore == nil {
		// If there is no clientStore, we don't reuse objects.
		// We still need to apply the transformer if provided, so we can wrap it
		// or just let the caller use WithTransformer.
		// However, in reflector.go we omitted WithTransformer, so we must wrap it.
	}
	return &watchListMemoryOptimizedStore{"""

new_func = """func newWatchListMemoryOptimizedStore(delegate Store, clientStore Store, keyFunc KeyFunc, transformer TransformFunc) Store {
	// keyFunc should match the delegate store's keying function.
	if clientStore == nil && transformer == nil {
		return delegate
	}
	return &watchListMemoryOptimizedStore{"""

if old_func in content:
    content = content.replace(old_func, new_func)
    with open("staging/src/k8s.io/client-go/tools/cache/watchlist_memory_store.go", "w") as f:
        f.write(content)
    print("Updated watchlist_memory_store.go")
else:
    print("Could not find func")
