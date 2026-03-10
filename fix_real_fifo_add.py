import sys

with open("staging/src/k8s.io/client-go/tools/cache/the_real_fifo.go", "r") as f:
    content = f.read()

old_add = """func (f *RealFIFO) addReplaceToItemsLocked(objs []interface{}, resourceVersion string) error {
	// Replaced items must be transformed before being added to the queue. These objects must
	// all be objects that have not been transformed yet.
	if f.transformer != nil {"""

new_add = """func (f *RealFIFO) addReplaceToItemsLocked(objs []interface{}, resourceVersion string, alreadyTransformed bool) error {
	// Replaced items must be transformed before being added to the queue. These objects must
	// all be objects that have not been transformed yet.
	if !alreadyTransformed && f.transformer != nil {"""

if old_add in content:
    content = content.replace(old_add, new_add)
else:
    print("Could not find old add 1")

old_add_loop = """	var replaced []Deltas
	for _, item := range objs {
		err := f.addToItems_locked(Replaced, true, item)"""

new_add_loop = """	var replaced []Deltas
	for _, item := range objs {
		// If they were already transformed externally, they don't need it.
		// If we transformed them in the block above, they also don't need it.
		err := f.addToItems_locked(Replaced, true, item)"""

if old_add_loop in content:
    content = content.replace(old_add_loop, new_add_loop)
else:
    print("Could not find old add loop")

with open("staging/src/k8s.io/client-go/tools/cache/the_real_fifo.go", "w") as f:
    f.write(content)
print("Updated addReplaceToItemsLocked")

