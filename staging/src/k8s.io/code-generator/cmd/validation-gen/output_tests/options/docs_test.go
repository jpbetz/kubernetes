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

package options

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"
)

func Test(t *testing.T) {
	st := SchemeBuilder.Test(t)

	st.Value(&T1{S1: ""}).
		// All ifOptionDisabled validations should fail
		ExpectValidateFalse("field T1.S2", "field T1.S3.FeatureY")

	st.Value(&T1{S1: ""}).Opts(sets.New("FeatureX", "FeatureY")).
		// All ifOptionEnabled validations should fail
		ExpectValidateFalse("field T1.S1", "field T1.S3.FeatureX")

	st.Value(&T1{S1: ""}).Opts(sets.New("FeatureX")).
		// ifOptionEnabled=FeatureX validations should fail
		ExpectValidateFalse("field T1.S1", "field T1.S3.FeatureX", "field T1.S3.FeatureY")

	st.Value(&T1{S1: ""}).Opts(sets.New("FeatureY")).
		// ifOptionDisabled=FeatureY validations should fail
		ExpectValidateFalse("field T1.S2")

}
