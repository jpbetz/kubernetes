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

package enums

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
	pointer "k8s.io/utils/ptr"
)

func Test(t *testing.T) {
	st := localSchemeBuilder.Test(t)

	st.Value(&T1{
		E0:  "",                 // no valid value exists
		PE0: pointer.To(E0("")), // no valid value exists
		E1:  E1V1,
		PE1: pointer.To(E1V1),
		E2:  E2V1,
		PE2: pointer.To(E2V1),
	}).ExpectInvalid(
		field.NotSupported(field.NewPath("e0"), "", []string{}),  // no valid value exists
		field.NotSupported(field.NewPath("pe0"), "", []string{}), // no valid value exists
	)

	st.Value(&T1{
		E0:  "x", // no valid value exists
		PE0: pointer.To(E0("x")),
		E1:  E1("x"),
		PE1: pointer.To(E1("x")),
		E2:  E2("x"),
		PE2: pointer.To(E2("x")),
	}).ExpectInvalid(
		field.NotSupported(field.NewPath("e0"), "x", []string{}),
		field.NotSupported(field.NewPath("pe0"), "x", []string{}),
		field.NotSupported(field.NewPath("e1"), "x", []string{string(E1V1)}),
		field.NotSupported(field.NewPath("pe1"), "x", []string{string(E1V1)}),
		field.NotSupported(field.NewPath("e2"), "x", []string{string(E2V1), string(E2V2)}),
		field.NotSupported(field.NewPath("pe2"), "x", []string{string(E2V1), string(E2V2)}),
	)
}
