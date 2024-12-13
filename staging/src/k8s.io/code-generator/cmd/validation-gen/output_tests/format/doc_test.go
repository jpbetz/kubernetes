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
package format

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

func Test(t *testing.T) {
	st := localSchemeBuilder.Test(t)

	st.Value(&T{
		IPField:       "1.2.3.4",
		DNSLabelField: "foo-bar",
	}).ExpectValid()

	st.Value(&T{
		IPField:       "",
		DNSLabelField: "",
	}).ExpectInvalid(
		field.Invalid(field.NewPath("ipField"), "", "must be a valid IP address (e.g. 10.9.8.7 or 2001:db8::ffff)"),
		field.Invalid(field.NewPath("dnsLabelField"), "", "must start and end with lower-case alphanumeric characters"),
		field.Invalid(field.NewPath("dnsLabelField"), "", "must consist of lower-case alphanumeric characters or '-'"),
	)
}
