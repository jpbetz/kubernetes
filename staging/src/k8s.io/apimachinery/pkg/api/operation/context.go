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

package operation

import "k8s.io/apimachinery/pkg/util/sets"

// Context provides contextual information about a validation request and the API
// operation being validated.
// This type is intended for use with generate validation code and may be enhanced
// in the future to include other information needed to validate requests.
type Context struct {
	Operation Operation
	// Options tracks the options enabled for validation.
	Options sets.Set[string]
}

// Operation is the request operation to be validated.
type Operation uint32

const (
	// Create indicates the request being validated is for a resource create operation.
	Create Operation = iota

	// Update indicates the request being validated is for a resource update operation.
	Update
)
