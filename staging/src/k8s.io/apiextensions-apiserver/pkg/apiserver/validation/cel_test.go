/*
Copyright 2017 The Kubernetes Authors.

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
	"testing"
)

func TestCelValueValidator(t *testing.T) {
	cases := []struct{
		name string
		input map[string]interface{} // TODO: switch to using
		isValid bool
	}{
		{
			name: "valid",
			input: map[string]interface{}{
				"minReplicas": 5,
				"maxReplicas": 10,
			},
			isValid: true,
		},
		{
			name: "invalid",
			input: map[string]interface{}{
				"minReplicas": 11,
				"maxReplicas": 10,
			},
			isValid: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			validator, err := NewCelValueValidator("minReplicas < maxReplicas")
			if err != nil {
				t.Fatalf("Unexpected compilation error: %v", err)
			}
			result := validator.Validate(tc.input)
			if result.IsValid() != tc.isValid {
				t.Fatalf("Expected isValid=%t, but got %t", tc.isValid, result.IsValid())
			}
		})
	}
}
