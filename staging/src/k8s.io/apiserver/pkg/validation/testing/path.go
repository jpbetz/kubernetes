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

package testing

import (
	"fmt"
	"strings"
)

// FieldPathToJSONPointer converts a Kubernetes field path to a JSON pointer.
// Field paths use dot notation with array indices in square brackets (e.g. spec.containers[0].name)
// JSON pointers use forward slashes with numeric indices (e.g. /spec/containers/0/name)
func FieldPathToJSONPointer(fieldPath string) (string, error) {
	if fieldPath == "" {
		return "", fmt.Errorf("field path cannot be empty")
	}

	// Split the field path into components
	parts := strings.Split(fieldPath, ".")

	// Build the JSON pointer
	var result strings.Builder
	for _, part := range parts {
		// Handle array index notation [n]
		if strings.Contains(part, "[") && strings.HasSuffix(part, "]") {
			base := part[:strings.Index(part, "[")]
			indexStr := strings.TrimSuffix(strings.TrimPrefix(part[strings.Index(part, "["):], "["), "]")
			result.WriteString("/")
			result.WriteString(base)
			result.WriteString("/")
			result.WriteString(indexStr)
		} else {
			result.WriteString("/")
			result.WriteString(part)
		}
	}

	return result.String(), nil
}
