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

package ratcheting

import (
	"testing"

	field "k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"
)

func Test(t *testing.T) {
	st := localSchemeBuilder.Test(t)

	structA := Struct{
		SP: ptr.To("zero"),
		IP: ptr.To(0),
		BP: ptr.To(false),
		FP: ptr.To(0.0),
	}

	// Different data.
	structB := Struct{
		SP: ptr.To("one"),
		IP: ptr.To(1),
		BP: ptr.To(true),
		FP: ptr.To(1.1),
	}

	// ratcheting tolerates unchnaged invalid values
	st.Value(&structA).OldValue(&structA).ExpectValid()

	st.Value(&structA).OldValue(&structB).ExpectInvalid(
		field.Invalid(field.NewPath("sp"), "zero", "forced failure: Struct.SP"),
		field.Invalid(field.NewPath("ip"), 0, "forced failure: Struct.IP"),
		field.Invalid(field.NewPath("bp"), false, "forced failure: Struct.BP"),
		field.Invalid(field.NewPath("fp"), 0.0, "forced failure: Struct.FP"),
	)
}
