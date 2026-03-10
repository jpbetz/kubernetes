import sys

with open("staging/src/k8s.io/client-go/tools/cache/delta_fifo.go", "r") as f:
    content = f.read()

# Find the Replace function
old_replace = """func (f *DeltaFIFO) Replace(list []interface{}, _ string) error {
	f.lock.Lock()
	defer f.lock.Unlock()
	keys := make(sets.Set[string], len(list))

	// keep backwards compat for old clients
	action := Sync
	if f.emitDeltaTypeReplaced {
		action = Replaced
	}

	// Add Sync/Replaced action for each new item.
	for _, item := range list {
		key, err := f.KeyOf(item)
		if err != nil {
			return KeyError{item, err}
		}
		keys.Insert(key)
		if err := f.queueActionInternalLocked(action, Replaced, item); err != nil {
			return fmt.Errorf("couldn't enqueue object: %v", err)
		}
	}"""

new_replace = """func (f *DeltaFIFO) Replace(list []interface{}, rv string) error {
	return f.replace(list, rv, false)
}

func (f *DeltaFIFO) ReplaceAlreadyTransformed(list []interface{}, rv string) error {
	return f.replace(list, rv, true)
}

func (f *DeltaFIFO) replace(list []interface{}, _ string, alreadyTransformed bool) error {
	f.lock.Lock()
	defer f.lock.Unlock()
	keys := make(sets.Set[string], len(list))

	// keep backwards compat for old clients
	action := Sync
	if f.emitDeltaTypeReplaced {
		action = Replaced
	}

	// Add Sync/Replaced action for each new item.
	for _, item := range list {
		key, err := f.KeyOf(item)
		if err != nil {
			return KeyError{item, err}
		}
		keys.Insert(key)
		internalAction := Replaced
		if alreadyTransformed {
			internalAction = Sync
		}
		if err := f.queueActionInternalLocked(action, internalAction, item); err != nil {
			return fmt.Errorf("couldn't enqueue object: %v", err)
		}
	}"""

if old_replace in content:
    content = content.replace(old_replace, new_replace)
    with open("staging/src/k8s.io/client-go/tools/cache/delta_fifo.go", "w") as f:
        f.write(content)
    print("Updated delta_fifo.go")
else:
    print("Could not find the old Replace function in delta_fifo.go")
