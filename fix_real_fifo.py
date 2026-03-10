import sys

with open("staging/src/k8s.io/client-go/tools/cache/the_real_fifo.go", "r") as f:
    content = f.read()

old_replace = """func (f *RealFIFO) Replace(newItems []interface{}, resourceVersion string) error {
	f.lock.Lock()
	defer f.lock.Unlock()

	var err error
	if f.emitAtomicEvents {
		err = f.addReplaceToItemsLocked(newItems, resourceVersion)
	} else {
		err = reconcileReplacement(f.items, f.knownObjects, newItems, f.keyOf,
			func(obj DeletedFinalStateUnknown) error {
				return f.addToItems_locked(Deleted, true, obj)
			},
			func(obj interface{}) error {
				return f.addToItems_locked(Replaced, false, obj)
			})
	}
	if err != nil {
		return err
	}"""

new_replace = """func (f *RealFIFO) Replace(newItems []interface{}, resourceVersion string) error {
	return f.replace(newItems, resourceVersion, false)
}

func (f *RealFIFO) ReplaceAlreadyTransformed(newItems []interface{}, resourceVersion string) error {
	return f.replace(newItems, resourceVersion, true)
}

func (f *RealFIFO) replace(newItems []interface{}, resourceVersion string, alreadyTransformed bool) error {
	f.lock.Lock()
	defer f.lock.Unlock()

	var err error
	if f.emitAtomicEvents {
		err = f.addReplaceToItemsLocked(newItems, resourceVersion, alreadyTransformed)
	} else {
		err = reconcileReplacement(f.items, f.knownObjects, newItems, f.keyOf,
			func(obj DeletedFinalStateUnknown) error {
				return f.addToItems_locked(Deleted, true, obj)
			},
			func(obj interface{}) error {
				return f.addToItems_locked(Replaced, alreadyTransformed, obj)
			})
	}
	if err != nil {
		return err
	}"""

old_add = """func (f *RealFIFO) addReplaceToItemsLocked(newItems []interface{}, resourceVersion string) error {
	var replaced []Deltas
	for _, item := range newItems {
		err := f.addToItems_locked(Replaced, false, item)
		if err != nil {
			return err
		}
		replaced = append(replaced, f.items[f.keyFunc(item)])
	}"""

new_add = """func (f *RealFIFO) addReplaceToItemsLocked(newItems []interface{}, resourceVersion string, alreadyTransformed bool) error {
	var replaced []Deltas
	for _, item := range newItems {
		err := f.addToItems_locked(Replaced, alreadyTransformed, item)
		if err != nil {
			return err
		}
		replaced = append(replaced, f.items[f.keyFunc(item)])
	}"""

if old_replace in content:
    content = content.replace(old_replace, new_replace)
    content = content.replace(old_add, new_add)
    with open("staging/src/k8s.io/client-go/tools/cache/the_real_fifo.go", "w") as f:
        f.write(content)
    print("Updated the_real_fifo.go")
else:
    print("Could not find the_real_fifo.go replace")

with open("staging/src/k8s.io/client-go/tools/cache/reflector_test.go", "r") as f:
    test_content = f.read()

old_test = """			// Transformer should have been invoked twice for the initial sync in the informer on the temporary store,
			// then twice on replace, then once on the following update.
			if want, got := 5, int(transformerInvoked.Load()); want != got {"""

new_test = """			// Transformer should have been invoked twice for the initial sync in the informer on the temporary store,
			// then once on the following update. (Replacement skips transform since they are already transformed).
			if want, got := 3, int(transformerInvoked.Load()); want != got {"""

if old_test in test_content:
    test_content = test_content.replace(old_test, new_test)
    with open("staging/src/k8s.io/client-go/tools/cache/reflector_test.go", "w") as f:
        f.write(test_content)
    print("Updated reflector_test.go")
else:
    print("Could not find reflector_test.go want")
