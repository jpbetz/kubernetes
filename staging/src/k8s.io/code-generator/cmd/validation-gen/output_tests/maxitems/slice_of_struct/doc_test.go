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
package sliceofstruct

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

func Test(t *testing.T) {
	st := localSchemeBuilder.Test(t)

	st.Value(&M{M0: []S{{}, {}}}).
		ExpectInvalid(
			field.TooMany(field.NewPath("m0"), 2, 1),
		)

	st.Value(&M{M0: []S{{}}}).
		ExpectValid()

	st.Value(&M{M1: SSliceLimited{{}}}).
		ExpectValid()

	st.Value(&M{M1: SSliceLimited{{}, {}, {}}}).
		ExpectInvalid(
			field.TooMany(field.NewPath("m1"), 3, 2),
		)
	st.Value(&M{M2: SSlice{}}).
		ExpectValid()

	st.Value(&M{M2: SSlice{{}, {}}}).
		ExpectInvalid(
			field.TooMany(field.NewPath("m2"), 2, 1),
		)

	st.Value(&M{M3: SSliceLimited{}}).
		ExpectValid()

	st.Value(&M{M3: SSliceLimited{{}, {}, {}}}).
		ExpectInvalid(
			field.TooMany(field.NewPath("m3"), 3, 1),
		)

	st.Value(&M{M4: []*S{}}).
		ExpectValid()

	st.Value(&M{M4: []*S{{}, {}, {}}}).
		ExpectInvalid(
			field.TooMany(field.NewPath("m4"), 3, 1),
		)

	st.Value(&M{M5: SPtrSliceLimited{}}).
		ExpectValid()

	st.Value(&M{M5: SPtrSliceLimited{{}, {}, {}}}).
		ExpectInvalid(
			field.TooMany(field.NewPath("m5"), 3, 2),
		)

	st.Value(&M{M6: SPtrSlice{}}).
		ExpectValid()

	st.Value(&M{M6: SPtrSlice{{}, {}, {}}}).
		ExpectInvalid(
			field.TooMany(field.NewPath("m6"), 3, 1),
		)

	st.Value(&M{M7: SPtrSliceLimited{}}).
		ExpectValid()

	st.Value(&M{M7: SPtrSliceLimited{{}, {}, {}}}).
		ExpectInvalid(
			field.TooMany(field.NewPath("m7"), 3, 1),
		)
}
