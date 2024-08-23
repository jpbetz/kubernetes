/*
Copyright 2024 The Kubernetes Authors.

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

package validation

import (
	"fmt"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	pointer "k8s.io/utils/ptr"
)

func TestMustSet(t *testing.T) {
	testCases := []struct {
		path  string
		in    runtime.Object
		value any
		want  runtime.Object
	}{
		{
			path:  "b",
			in:    &T1{B: true},
			value: false,
			want:  &T1{B: false},
		},
		{
			path:  "i",
			in:    &T1{I: 1},
			value: 2,
			want:  &T1{I: 2},
		},
		{
			path:  "s",
			in:    &T1{S: "x"},
			value: "y",
			want:  &T1{S: "y"},
		},
		{
			path:  "lm[s:k2]",
			in:    &T3{LM: []T1{{S: "k2", I: 2}}},
			value: T1{S: "k2", I: 3},
			want:  &T3{LM: []T1{{S: "k2", I: 3}}},
		},
		{
			path:  "lm[0]",
			in:    &T3{LM: []T1{{S: "k2", I: 2}}},
			value: T1{S: "k2", I: 3},
			want:  &T3{LM: []T1{{S: "k2", I: 3}}},
		},
		{
			path:  "lm[s:k2].i",
			in:    &T3{LM: []T1{{S: "k2", I: 2}}},
			value: 3,
			want:  &T3{LM: []T1{{S: "k2", I: 3}}},
		},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%v", tc.path), func(t *testing.T) {
			got := MustSet(tc.in, tc.path, tc.value)
			if !reflect.DeepEqual(tc.in, tc.want) {
				t.Errorf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestParsePath(t *testing.T) {
	tests := []struct {
		path string
		want path
	}{
		{
			"a",
			path{part{Key: pointer.To("a")}},
		},
		{
			"a.b",
			path{part{Key: pointer.To("a")}, part{Key: pointer.To("b")}},
		},
		{
			`a["b"]`,
			path{part{Key: pointer.To("a")}, part{Key: pointer.To("b")}},
		},
		{
			`a[1]`,
			path{part{Key: pointer.To("a")}, part{Index: pointer.To(1)}},
		},
		{
			`a[k1:v1,k2:v2]`,
			path{part{Key: pointer.To("a")}, part{ListMapKey: map[string]any{"k1": "v1", "k2": "v2"}}},
		},
		{
			`a[k1:"v1",k2:"v2"]`,
			path{part{Key: pointer.To("a")}, part{ListMapKey: map[string]any{"k1": "v1", "k2": "v2"}}},
		},
		{
			`a[k1:1,k2:2]`,
			path{part{Key: pointer.To("a")}, part{ListMapKey: map[string]any{"k1": 1, "k2": 2}}},
		},
		{
			`a[k1:true,k2:false]`,
			path{part{Key: pointer.To("a")}, part{ListMapKey: map[string]any{"k1": true, "k2": false}}},
		},
		{
			`a[barekey]`,
			path{part{Key: pointer.To("a")}, part{Key: pointer.To("barekey")}},
		},
		{
			`a[b][c]`,
			path{part{Key: pointer.To("a")}, part{Key: pointer.To("b")}, part{Key: pointer.To("c")}},
		},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			got, err := parsePath(test.path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("got %#v, want %#v", got.String(), test.want.String())
			}
		})
	}
}

// TODO:
//   - inline / embedded types
//   - pointers to types
//   - typerefs/aliases
//   - ??
type (
	T1 struct {
		S string  `json:"s"`
		I int     `json:"i"`
		B bool    `json:"b"`
		F float32 `json:"f"`
		U uint    `json:"u"`
	}
	T2 struct {
		T1  T1             `json:"t1"`
		PT1 *T1            `json:"pt1"`
		M1  map[string]int `json:"m1"`
		L1  []int          `json:"l1"`
	}
	T3 struct {
		LM []T1 `json:"lm"`
	}
)

func (t *T1) GetObjectKind() schema.ObjectKind {
	return nil
}

func (t *T1) DeepCopyObject() runtime.Object {
	return t
}

func (t *T2) GetObjectKind() schema.ObjectKind {
	return nil
}

func (t *T2) DeepCopyObject() runtime.Object {
	return t
}

func (t *T3) GetObjectKind() schema.ObjectKind {
	return nil
}

func (t *T3) DeepCopyObject() runtime.Object {
	return t
}

func TestSetAtPath(t *testing.T) {
	testCases := []struct {
		path  path
		in    any
		value any
		want  any
	}{
		{
			path:  path{part{Key: pointer.To("b")}},
			in:    &T1{B: true},
			value: false,
			want:  &T1{B: false},
		},
		{
			path:  path{part{Key: pointer.To("lm")}, part{ListMapKey: map[string]any{"s": "k2"}}, part{Key: pointer.To("i")}},
			in:    &T3{LM: []T1{{S: "k1", I: 1}, {S: "k2", I: 2}}},
			value: 3,
			want:  &T3{LM: []T1{{S: "k1", I: 1}, {S: "k2", I: 3}}},
		},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%v", tc.path), func(t *testing.T) {
			err := setAtPath(tc.in, tc.path, tc.value)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.in, tc.want) {
				t.Errorf("got %#v, want %#v", tc.in, tc.want)
			}
		})
	}
}
