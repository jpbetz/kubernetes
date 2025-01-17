/*
Copyright 2025 The Kubernetes Authors.

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

package typed

import (
	"sync"

	"sigs.k8s.io/structured-merge-diff/v4/fieldpath"
	"sigs.k8s.io/structured-merge-diff/v4/schema"
	"sigs.k8s.io/structured-merge-diff/v4/value"
)

var mPool = sync.Pool{
	New: func() interface{} { return &markerExtractorWalker{} },
}

func (tv TypedValue) markerExtractorWalker() *markerExtractorWalker {
	v := mPool.Get().(*markerExtractorWalker)
	v.value = tv.value
	v.schema = tv.schema
	v.typeRef = tv.typeRef
	v.unsetMarkers = &fieldpath.Set{}
	v.fieldsToClear = &fieldpath.Set{}
	v.allocator = value.NewFreelistAllocator()
	return v
}

func (v *markerExtractorWalker) finished() {
	v.schema = nil
	v.typeRef = schema.TypeRef{}
	v.path = nil
	v.unsetMarkers = nil
	v.fieldsToClear = nil
	mPool.Put(v)
}

type markerExtractorWalker struct {
	value   value.Value
	schema  *schema.Schema
	typeRef schema.TypeRef

	unsetMarkers  *fieldpath.Set
	fieldsToClear *fieldpath.Set

	path fieldpath.Path

	// Allocate only as many walkers as needed for the depth by storing them here.
	spareWalkers *[]*markerExtractorWalker
	allocator    value.Allocator
}

func (v *markerExtractorWalker) prepareDescent(pe fieldpath.PathElement, tr schema.TypeRef) *markerExtractorWalker {
	if v.spareWalkers == nil {
		// first descent.
		v.spareWalkers = &[]*markerExtractorWalker{}
	}
	var v2 *markerExtractorWalker
	if n := len(*v.spareWalkers); n > 0 {
		v2, *v.spareWalkers = (*v.spareWalkers)[n-1], (*v.spareWalkers)[:n-1]
	} else {
		v2 = &markerExtractorWalker{}
	}
	*v2 = *v
	v2.typeRef = tr
	v2.path = append(v2.path, pe)
	return v2
}

func (v *markerExtractorWalker) finishDescent(v2 *markerExtractorWalker) {
	// if the descent caused a realloc, ensure that we reuse the buffer
	// for the next sibling.
	v.path = v2.path[:len(v2.path)-1]
	*v.spareWalkers = append(*v.spareWalkers, v2)
}

func (v *markerExtractorWalker) extractMarkers() ValidationErrors {
	return resolveSchema(v.schema, v.typeRef, v.value, v)
}

func (v *markerExtractorWalker) doScalar(t *schema.Scalar) ValidationErrors {
	if isUnsetMarker(v.value) {
		v.unsetMarkers.Insert(v.path)
		return nil
	}
	return nil
}

func (v *markerExtractorWalker) visitListItems(t *schema.List, list value.List) (errs ValidationErrors) {
	size := list.Length()
	if size == 0 {
		return nil
	}
	for i := 0; i < size; i++ {
		child := list.At(i)
		pe, _ := listItemToPathElement(v.allocator, v.schema, t, child)

		v2 := v.prepareDescent(pe, t.ElementType)
		v2.value = child
		errs = append(errs, v2.extractMarkers()...)
		v.finishDescent(v2)

		if v2.unsetMarkers.Has(v2.path) {
			size--
		}
	}
	if size == 0 {
		v.fieldsToClear.Insert(v.path)
	}
	return errs
}

func isUnsetMarker(v value.Value) bool {
	if v.IsMap() {
		m := v.AsMap()
		if maybeUnset, ok := m.Get("__k8s_io_value__"); ok && maybeUnset.IsString() {
			if maybeUnset.AsString() == "unset" {
				return true
			}
		}
	}
	return false
}

func (v *markerExtractorWalker) doList(t *schema.List) (errs ValidationErrors) {

	list, _ := listValue(v.allocator, v.value)
	if list != nil {
		defer v.allocator.Free(list)
	}
	if t.ElementRelationship == schema.Atomic {
		if isUnsetMarker(v.value) {
			v.unsetMarkers.Insert(v.path)
		}
		return nil
	}

	if list == nil {
		return nil
	}

	errs = v.visitListItems(t, list)

	return errs
}

func (v *markerExtractorWalker) visitMapItems(t *schema.Map, m value.Map) (errs ValidationErrors) {
	size := m.Length()
	if size == 0 {
		return nil
	}
	m.Iterate(func(key string, val value.Value) bool {
		pe := fieldpath.PathElement{FieldName: &key}

		tr := t.ElementType
		if sf, ok := t.FindField(key); ok {
			tr = sf.Type
		}
		v2 := v.prepareDescent(pe, tr)
		v2.value = val
		errs = append(errs, v2.extractMarkers()...)
		v.finishDescent(v2)

		if v2.unsetMarkers.Has(v2.path) {
			size--
		}

		return true
	})
	if size == 0 {
		v.fieldsToClear.Insert(v.path)
	}
	return errs
}

func (v *markerExtractorWalker) doMap(t *schema.Map) (errs ValidationErrors) {
	if isUnsetMarker(v.value) {
		v.unsetMarkers.Insert(v.path)
		return nil
	}

	m, _ := mapValue(v.allocator, v.value)
	if m != nil {
		defer v.allocator.Free(m)
	}
	if t.ElementRelationship == schema.Atomic {
		return nil
	}

	if m == nil {
		return nil
	}

	errs = v.visitMapItems(t, m)

	return errs
}
