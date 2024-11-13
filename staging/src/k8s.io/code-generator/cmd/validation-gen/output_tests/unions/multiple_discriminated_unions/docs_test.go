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

package multiple_discriminated_unions

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

func Test(t *testing.T) {
	st := localSchemeBuilder.Test(t)

	st.Value(&DU{DU1: U1M1, U1M1: &M1{S: "x"}, DU2: U2M1, U2M1: &M1{S: "x"}}).ExpectValid()
	st.Value(&DU{DU1: U1M2, U1M2: &M2{S: "x"}, DU2: U2M2, U2M2: &M2{S: "x"}}).ExpectValid()

	st.Value(&DU{DU1: U1M2, U1M1: &M1{S: "x"}, U1M2: &M2{S: "x"}}).ExpectInvalid(
		field.Invalid(field.NewPath("u1m1"), "", "may only be specified when `du1` is \"U1M1\""),
	)

	st.Value(&DU{DU1: U1M2}).ExpectInvalid(
		field.Invalid(field.NewPath("u1m2"), "", "must be specified when `du1` is \"U1M2\""),
	)
}
