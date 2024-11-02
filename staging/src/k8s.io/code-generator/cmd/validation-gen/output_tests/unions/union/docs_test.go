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

package union

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

func Test(t *testing.T) {
	st := SchemeBuilder.Test(t)

	st.Value(&U{M1: &M1{S: "x"}}).ExpectValid()
	st.Value(&U{M2: &M2{S: "x"}}).ExpectValid()

	st.Value(&U{M1: &M1{S: "x"}, M2: &M2{S: "x"}}).ExpectInvalid(
		field.Invalid(nil, "{m1, m2}", "must specify exactly one of: `m1`, `m2`"),
	)
	st.Value(&U{}).ExpectInvalid(
		field.Invalid(nil, "", "must specify exactly one of: `m1`, `m2`"),
	)
}
