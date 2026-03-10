import sys

with open("staging/src/k8s.io/client-go/tools/cache/reflector.go", "r") as f:
    content = f.read()

old_replace = "if err := r.store.Replace(temporaryStore.List(), resourceVersion); err != nil {"
new_replace = """	if replaceTransformed, ok := r.store.(interface {
		ReplaceAlreadyTransformed(list []interface{}, rv string) error
	}); ok && transformer != nil {
		err = replaceTransformed.ReplaceAlreadyTransformed(temporaryStore.List(), resourceVersion)
	} else {
		err = r.store.Replace(temporaryStore.List(), resourceVersion)
	}
	if err != nil {"""

if old_replace in content:
    content = content.replace(old_replace, new_replace)
    with open("staging/src/k8s.io/client-go/tools/cache/reflector.go", "w") as f:
        f.write(content)
    print("Updated Replace call")
else:
    print("Could not find Replace call")
