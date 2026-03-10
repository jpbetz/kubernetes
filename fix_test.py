import sys

with open("staging/src/k8s.io/client-go/tools/cache/watchlist_memory_store_test.go", "r") as f:
    content = f.read()

old_test = """	// Create a temporaryStore WITH a transformer (simulating what watchList does)
	transformer := func(obj interface{}) (interface{}, error) {
		pod := obj.(*v1.Pod).DeepCopy()
		if pod.Annotations == nil {
			pod.Annotations = make(map[string]string)
		}
		pod.Annotations["transformed"] = pod.Annotations["transformed"] + "+again"
		return pod, nil
	}
	temporaryStore := NewStore(keyFunc, WithTransformer(transformer))

	// Wrap temporaryStore with watchListMemoryOptimizedStore
	store := newWatchListMemoryOptimizedStore(temporaryStore, clientStore, keyFunc, nil)"""

new_test = """	// Create a temporaryStore WITHOUT a transformer (as reflector.go now does)
	transformer := func(obj interface{}) (interface{}, error) {
		pod := obj.(*v1.Pod).DeepCopy()
		if pod.Annotations == nil {
			pod.Annotations = make(map[string]string)
		}
		pod.Annotations["transformed"] = pod.Annotations["transformed"] + "+again"
		return pod, nil
	}
	temporaryStore := NewStore(keyFunc)

	// Wrap temporaryStore with watchListMemoryOptimizedStore and pass the transformer
	store := newWatchListMemoryOptimizedStore(temporaryStore, clientStore, keyFunc, transformer)"""

if old_test in content:
    content = content.replace(old_test, new_test)
    with open("staging/src/k8s.io/client-go/tools/cache/watchlist_memory_store_test.go", "w") as f:
        f.write(content)
    print("Updated test")
else:
    print("Could not find test")
