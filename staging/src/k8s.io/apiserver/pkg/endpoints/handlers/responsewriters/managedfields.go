/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package responsewriters

import (
	"io"
	"reflect"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// stripManagedFieldsEncoder encodes objects as its delegate does, but with
// metadata.managedFields omitted. Encoded objects can share memory with the
// watch cache, so stripping always operates on shallow copies and never
// mutates the object passed to Encode.
type stripManagedFieldsEncoder struct {
	delegate runtime.Encoder
}

func (e *stripManagedFieldsEncoder) Encode(obj runtime.Object, w io.Writer) error {
	return e.delegate.Encode(withoutManagedFields(obj), w)
}

// Identifier is distinct from the delegate's so that cached serializations of
// the stripped form are never confused with the full form.
func (e *stripManagedFieldsEncoder) Identifier() runtime.Identifier {
	return runtime.Identifier("drop-managed-fields:" + string(e.delegate.Identifier()))
}

// withoutManagedFields returns obj with metadata.managedFields removed from it
// and, for lists, from each item. It never mutates obj: structs, maps and
// slices on the path to managedFields are shallow-copied and all other state
// is shared with the original. If there is nothing to strip, obj is returned
// unchanged.
func withoutManagedFields(obj runtime.Object) runtime.Object {
	switch t := obj.(type) {
	case *unstructured.Unstructured:
		if content, changed := strippedUnstructuredContent(t.Object); changed {
			return &unstructured.Unstructured{Object: content}
		}
		return obj
	case *unstructured.UnstructuredList:
		var items []unstructured.Unstructured
		for i := range t.Items {
			content, changed := strippedUnstructuredContent(t.Items[i].Object)
			if !changed {
				continue
			}
			if items == nil {
				items = make([]unstructured.Unstructured, len(t.Items))
				copy(items, t.Items)
			}
			items[i].Object = content
		}
		if items != nil {
			return &unstructured.UnstructuredList{Object: t.Object, Items: items}
		}
		return obj
	}
	if _, ok := obj.(runtime.Unstructured); ok {
		// unknown unstructured implementations cannot be safely shallow-copied
		return obj
	}
	if meta.IsListType(obj) {
		return listWithoutManagedFields(obj)
	}
	return objectWithoutManagedFields(obj)
}

func objectWithoutManagedFields(obj runtime.Object) runtime.Object {
	accessor, err := meta.Accessor(obj)
	if err != nil || len(accessor.GetManagedFields()) == 0 {
		return obj
	}
	copied, ok := shallowCopyObject(obj)
	if !ok {
		return obj
	}
	accessor, err = meta.Accessor(copied)
	if err != nil {
		return obj
	}
	// API types embed ObjectMeta by value, so this writes only to the copy.
	accessor.SetManagedFields(nil)
	return copied
}

func listWithoutManagedFields(list runtime.Object) runtime.Object {
	copied, ok := shallowCopyObject(list)
	if !ok {
		return list
	}
	itemsPtr, err := meta.GetItemsPtr(copied)
	if err != nil {
		return list
	}
	items := reflect.ValueOf(itemsPtr)
	if items.Kind() != reflect.Pointer || items.IsNil() || items.Elem().Kind() != reflect.Slice {
		return list
	}
	items = items.Elem()
	stripped := reflect.MakeSlice(items.Type(), items.Len(), items.Len())
	reflect.Copy(stripped, items)
	for i := 0; i < stripped.Len(); i++ {
		item := stripped.Index(i)
		switch item.Kind() {
		case reflect.Struct:
			itemPtr := item.Addr().Interface()
			if u, ok := itemPtr.(runtime.Unstructured); ok {
				if strippedObj := withoutManagedFields(u); strippedObj != runtime.Object(u) {
					item.Set(reflect.ValueOf(strippedObj).Elem())
				}
				continue
			}
			// each element is a fresh struct copy in the new slice, so its
			// embedded ObjectMeta can be cleared without touching the original
			if accessor, err := meta.Accessor(itemPtr); err == nil {
				accessor.SetManagedFields(nil)
			}
		case reflect.Interface, reflect.Pointer:
			// the element still aliases the original item, so replace it with
			// a stripped shallow copy rather than mutating through it
			if obj, ok := item.Interface().(runtime.Object); ok {
				if strippedObj := withoutManagedFields(obj); strippedObj != obj {
					item.Set(reflect.ValueOf(strippedObj))
				}
			}
		}
	}
	items.Set(stripped)
	return copied
}

func shallowCopyObject(obj runtime.Object) (runtime.Object, bool) {
	v := reflect.ValueOf(obj)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return nil, false
	}
	copied := reflect.New(v.Type().Elem())
	copied.Elem().Set(v.Elem())
	return copied.Interface().(runtime.Object), true
}

// strippedUnstructuredContent returns content with metadata.managedFields
// removed from it and from any entries under "items", shallow-copying only the
// maps and slices along the modified paths. It reports whether anything was
// stripped; if not, content is returned as-is.
func strippedUnstructuredContent(content map[string]interface{}) (map[string]interface{}, bool) {
	var out map[string]interface{}
	if metadata, ok := content["metadata"].(map[string]interface{}); ok {
		if _, exists := metadata["managedFields"]; exists {
			strippedMetadata := make(map[string]interface{}, len(metadata)-1)
			for k, v := range metadata {
				if k != "managedFields" {
					strippedMetadata[k] = v
				}
			}
			out = shallowCopyContent(content)
			out["metadata"] = strippedMetadata
		}
	}
	if items, ok := content["items"].([]interface{}); ok {
		var strippedItems []interface{}
		for i, item := range items {
			itemContent, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			strippedItem, changed := strippedUnstructuredContent(itemContent)
			if !changed {
				continue
			}
			if strippedItems == nil {
				strippedItems = make([]interface{}, len(items))
				copy(strippedItems, items)
			}
			strippedItems[i] = strippedItem
		}
		if strippedItems != nil {
			if out == nil {
				out = shallowCopyContent(content)
			}
			out["items"] = strippedItems
		}
	}
	if out == nil {
		return content, false
	}
	return out, true
}

func shallowCopyContent(content map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(content))
	for k, v := range content {
		out[k] = v
	}
	return out
}
