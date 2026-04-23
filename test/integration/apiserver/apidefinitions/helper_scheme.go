/*
Copyright The Kubernetes Authors.

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

package apidefinitions

import (
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/apitesting/roundtrip"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
)

// SchemeType describes a API Go type registered a Scheme.
type SchemeType struct {
	GVK      schema.GroupVersionKind
	Type     reflect.Type
	Instance runtime.Object
	Scheme   *runtime.Scheme
}

// IsInternal reports whether the type belongs to a group's internal version.
func (s *SchemeType) IsInternal() bool {
	return s.GVK.Version == runtime.APIVersionInternal
}

// IsList reports whether the kind is a list kind (e.g. PodList).
func (s *SchemeType) IsList() bool {
	return strings.HasSuffix(s.GVK.Kind, "List")
}

// KindString returns a stable human-readable identifier suitable for exemption
// lists.
func (s *SchemeType) KindString() string {
	if s.GVK.Group == "" {
		return s.GVK.Kind + "." + s.GVK.Version
	}
	return s.GVK.Kind + "." + s.GVK.Version + "." + s.GVK.Group
}

// SchemeTypeTestFunc is invoked once per registered scheme type.
type SchemeTypeTestFunc func(t *testing.T, st SchemeType)

// TestAllSchemeTypes iterates every type registered in legacyscheme.Scheme and
// invokes testFunc for each inside a t.Run keyed by GroupVersionKind.
func TestAllSchemeTypes(t *testing.T, testFunc SchemeTypeTestFunc) {

	var ignoreKinds = map[string]struct{}{
		"CreateOptions": {},
		"UpdateOptions": {},
		"PatchOptions":  {},
		"APIVersions":   {},
	}
	nonRoundTrippable := roundtrip.GlobalNonRoundTrippableTypes()
	for gvk, typ := range legacyscheme.Scheme.AllKnownTypes() {
		if strings.HasSuffix(gvk.Kind, "List") {
			continue
		}
		if nonRoundTrippable.Has(gvk.Kind) {
			continue
		}
		if _, ok := ignoreKinds[gvk.Kind]; ok {
			continue
		}
		instance, err := legacyscheme.Scheme.New(gvk)
		if err != nil {
			t.Errorf("Scheme.New(%v) failed: %v", gvk, err)
			continue
		}
		st := SchemeType{
			GVK:      gvk,
			Type:     typ,
			Instance: instance,
			Scheme:   legacyscheme.Scheme,
		}
		t.Run(st.KindString(), func(t *testing.T) {
			testFunc(t, st)
		})
	}
}

// TestSchemeType enforces a code-generator consistency invariant against an
// allowlist of pre-existing exceptions.
func TestSchemeType(t *testing.T, st SchemeType, msg string, conforms bool, allowed sets.Set[string]) {
	t.Helper()
	name := st.KindString()
	if matchesSchemeException(st, allowed) {
		if conforms {
			t.Errorf("%s: %s unexpectedly satisfied. Remove it from the exception list.", name, msg)
		}
		return
	}
	if !conforms {
		t.Errorf("%s: %s", name, msg)
	}
}

func hasMethod(typ reflect.Type, name string) bool {
	if typ.Kind() != reflect.Pointer {
		typ = reflect.PointerTo(typ)
	}
	_, ok := typ.MethodByName(name)
	return ok
}

// matchesSchemeException reports whether a SchemeType matches any entry in
// exceptions of the form:
//
//	"Kind" (all versions, all groups; only useful for core-like Kinds)
//	"Kind.group" (all versions of a group)
//	"Kind.version.group" (a specific GVK)
func matchesSchemeException(st SchemeType, exceptions sets.Set[string]) bool {
	if exceptions.Has(st.KindString()) {
		return true
	}
	grouped := st.GVK.Kind
	if st.GVK.Group != "" {
		grouped = st.GVK.Kind + "." + st.GVK.Group
	}
	if exceptions.Has(grouped) {
		return true
	}
	return exceptions.Has(st.GVK.Kind)
}
