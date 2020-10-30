/*
Copyright 2020 The Kubernetes Authors.

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

package generators

import "k8s.io/gengo/types"

var (
	applyConfiguration    = types.Ref("k8s.io/apimachinery/pkg/runtime", "ApplyConfiguration")
	groupVersionKind      = types.Ref("k8s.io/apimachinery/pkg/runtime/schema", "GroupVersionKind")
	rawExtension          = types.Ref("k8s.io/apimachinery/pkg/runtime", "RawExtension")
	unknown               = types.Ref("k8s.io/apimachinery/pkg/runtime", "Unknown")
	unstructured          = types.Ref("k8s.io/apimachinery/pkg/apis/meta/v1/unstructured", "Unstructured")
	unstructuredConverter = types.Ref("k8s.io/apimachinery/pkg/runtime", "DefaultUnstructuredConverter")
	jsonMarshal           = types.Ref("encoding/json", "Marshal")
	jsonUnmarshal         = types.Ref("encoding/json", "Unmarshal")
)
