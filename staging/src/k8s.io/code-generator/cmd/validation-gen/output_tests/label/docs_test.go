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

package label

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestRealistic(t *testing.T) {
	st := localSchemeBuilder.Test(t)

	// ReplicaSet Tests

	// Test valid selector matching
	st.Value(&ReplicaSetExample{
		SelectorMatchLabel: "app-v1",
		Metadata: ObjectMeta{
			Labels: map[string]string{
				"app": "app-v1",
			},
		},
	}).ExpectValid()

	// Test selector mismatch
	st.Value(&ReplicaSetExample{
		SelectorMatchLabel: "app-v1",
		Metadata: ObjectMeta{
			Labels: map[string]string{
				"app": "app-v2",
			},
		},
	}).ExpectInvalid(
		field.Invalid(
			field.NewPath("metadata", "labels").Key("app"),
			"app-v2",
			"must match SelectorMatchLabel (app-v1)"))

	// Job Tests

	// Test valid controller labels
	st.Value(&JobExample{
		Name: "test-job",
		UID:  "12345",
		Metadata: ObjectMeta{
			Labels: map[string]string{
				"controller-uid": "12345",
				"job-name":       "test-job",
			},
		},
	}).ExpectValid()

	// Test missing required labels
	st.Value(&JobExample{
		Name: "test-job",
		UID:  "12345",
		Metadata: ObjectMeta{
			Labels: map[string]string{},
		},
	}).ExpectInvalid(
		field.Required(
			field.NewPath("metadata", "labels").Key("controller-uid"),
			"label controller-uid is required"),
		field.Required(
			field.NewPath("metadata", "labels").Key("job-name"),
			"label job-name is required"))

	// Test mismatched labels
	st.Value(&JobExample{
		Name: "test-job",
		UID:  "12345",
		Metadata: ObjectMeta{
			Labels: map[string]string{
				"controller-uid": "67890",
				"job-name":       "different-job",
			},
		},
	}).ExpectInvalid(
		field.Invalid(
			field.NewPath("metadata", "labels").Key("controller-uid"),
			"67890",
			"must match UID (12345)"),
		field.Invalid(
			field.NewPath("metadata", "labels").Key("job-name"),
			"different-job",
			"must match Name (test-job)"))
}
