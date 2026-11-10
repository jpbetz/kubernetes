/*
Copyright 2026 The Kubernetes Authors.

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

package reconciliation

import (
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

// TestCreateInNamespace checks which requests creating a Role or RoleBinding
// sends, depending on whether its namespace exists.
func TestCreateInNamespace(t *testing.T) {
	meta := metav1.ObjectMeta{Namespace: "ns", Name: "name"}
	createRole := func(client *fake.Clientset) error {
		modifier := RoleModifier{Client: client.RbacV1(), NamespaceClient: client.CoreV1().Namespaces()}
		_, err := modifier.Create(RoleRuleOwner{Role: &rbacv1.Role{ObjectMeta: meta}})
		return err
	}
	createRoleBinding := func(client *fake.Clientset) error {
		adapter := RoleBindingClientAdapter{Client: client.RbacV1(), NamespaceClient: client.CoreV1().Namespaces()}
		_, err := adapter.Create(RoleBindingAdapter{RoleBinding: &rbacv1.RoleBinding{ObjectMeta: meta}})
		return err
	}
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns"}}

	tests := []struct {
		name string
		// existing are the objects in the cluster before create runs.
		existing []runtime.Object
		create   func(*fake.Clientset) error
		// wantRequests are the requests create sends, in order.
		wantRequests []string
	}{
		{
			// A namespace that exists is not read.
			name:         "role in an existing namespace",
			existing:     []runtime.Object{namespace},
			create:       createRole,
			wantRequests: []string{"create roles"},
		},
		{
			name:         "role in a missing namespace",
			create:       createRole,
			wantRequests: []string{"create roles", "get namespaces", "create namespaces", "create roles"},
		},
		{
			name:         "rolebinding in an existing namespace",
			existing:     []runtime.Object{namespace},
			create:       createRoleBinding,
			wantRequests: []string{"create rolebindings"},
		},
		{
			name:         "rolebinding in a missing namespace",
			create:       createRoleBinding,
			wantRequests: []string{"create rolebindings", "get namespaces", "create namespaces", "create rolebindings"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewClientset(tc.existing...)
			// Reject creates in a namespace that does not exist, as the apiserver's
			// NamespaceLifecycle admission plugin does. The namespace is read from
			// the tracker because calling the fake client from inside a reactor deadlocks.
			client.PrependReactor("create", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
				if action.GetNamespace() == "" {
					return false, nil, nil
				}
				namespaces := corev1.SchemeGroupVersion.WithResource("namespaces")
				if _, err := client.Tracker().Get(namespaces, "", action.GetNamespace()); apierrors.IsNotFound(err) {
					return true, nil, apierrors.NewNotFound(namespaces.GroupResource(), action.GetNamespace())
				}
				return false, nil, nil
			})

			if err := tc.create(client); err != nil {
				t.Fatalf("create: %v", err)
			}
			var requests []string
			for _, action := range client.Actions() {
				requests = append(requests, action.GetVerb()+" "+action.GetResource().Resource)
			}
			if !slices.Equal(requests, tc.wantRequests) {
				t.Errorf("requests = %q, want %q", requests, tc.wantRequests)
			}
		})
	}
}
