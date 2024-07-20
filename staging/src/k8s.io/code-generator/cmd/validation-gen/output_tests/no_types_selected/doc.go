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

// Note: no types match this.
// +k8s:validation-gen=TypeMeta

// This is a test package.
package notypesselected

type T1 struct {
	// +validateTrue="from field T1.S"
	S string
	// +validateTrue="from field T1.T2"
	T2 T2
}

type T2 struct {
	// +validateTrue="from field T2.S"
	S string
}

type private struct {
	// +validateTrue="from field private.S"
	S string
}
