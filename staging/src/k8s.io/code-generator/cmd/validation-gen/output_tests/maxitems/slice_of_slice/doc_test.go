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
package sliceofslice

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

func Test(t *testing.T) {
	st := localSchemeBuilder.Test(t)

	st.Value(&M{M0: [][]int{{0, 0}, {0, 0}}}).
		ExpectInvalid(
			field.TooMany(field.NewPath("m0"), 2, 1),
		)

	st.Value(&M{M0: [][]int{{0, 0}}}).
		ExpectValid()

	st.Value(&M{M1: []IntSlice{{0}}}).
		ExpectValid()

	st.Value(&M{M1: []IntSlice{{}, {}}}).
		ExpectInvalid(
			field.TooMany(field.NewPath("m1"), 2, 1),
		)
	st.Value(&M{M2: [][]*int{}}).
		ExpectValid()

	st.Value(&M{M2: [][]*int{{}, {}}}).
		ExpectInvalid(
			field.TooMany(field.NewPath("m2"), 2, 1),
		)

	st.Value(&M{M3: []IntPtrSlice{}}).
		ExpectValid()

	st.Value(&M{M3: []IntPtrSlice{{}, {}}}).
		ExpectInvalid(
			field.TooMany(field.NewPath("m3"), 2, 1),
		)

}
