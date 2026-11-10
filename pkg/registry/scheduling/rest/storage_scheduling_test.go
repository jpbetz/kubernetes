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

package rest

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	schedulingapiv1 "k8s.io/kubernetes/pkg/apis/scheduling/v1"
)

func TestEnsureSystemPriorityClasses(t *testing.T) {
	systemClasses := schedulingapiv1.SystemPriorityClasses() // system-node-critical, system-cluster-critical
	tests := []struct {
		name string
		// existing are the PriorityClasses in the cluster before ensureSystemPriorityClasses runs.
		existing []runtime.Object
		// listErr and createErr fail every List or Create request.
		listErr, createErr error
		// wantRetry means ensureSystemPriorityClasses reports that it must run again.
		wantRetry bool
		// wantErr means ensureSystemPriorityClasses gives up with an error.
		wantErr bool
		// wantRequests are the requests ensureSystemPriorityClasses sends, in order.
		wantRequests []string
	}{
		{
			name: "empty cluster",
			wantRequests: []string{
				"list priorityclasses",
				"create priorityclasses/system-node-critical",
				"create priorityclasses/system-cluster-critical",
			},
		},
		{
			name:     "only the missing class is created",
			existing: []runtime.Object{systemClasses[0]},
			wantRequests: []string{
				"list priorityclasses",
				"create priorityclasses/system-cluster-critical",
			},
		},
		{
			// When every class exists, the List is the only request.
			name:         "already bootstrapped",
			existing:     []runtime.Object{systemClasses[0], systemClasses[1]},
			wantRequests: []string{"list priorityclasses"},
		},
		{
			// Another apiserver can create a class after this one lists.
			name:      "a class created after the List counts as created",
			createErr: apierrors.NewAlreadyExists(schema.GroupResource{}, ""),
			wantRequests: []string{
				"list priorityclasses",
				"create priorityclasses/system-node-critical",
				"create priorityclasses/system-cluster-critical",
			},
		},
		{
			name:         "a failed List is retried",
			listErr:      errors.New("list failed"),
			wantRetry:    true,
			wantRequests: []string{"list priorityclasses"},
		},
		{
			name:      "a Create is retried while the apiserver is unavailable",
			createErr: apierrors.NewServiceUnavailable("storage version update in progress"),
			wantRetry: true,
			wantRequests: []string{
				"list priorityclasses",
				"create priorityclasses/system-node-critical",
			},
		},
		{
			name:      "any other Create error is fatal",
			createErr: errors.New("create failed"),
			wantErr:   true,
			wantRequests: []string{
				"list priorityclasses",
				"create priorityclasses/system-node-critical",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewClientset(tc.existing...)
			client.PrependReactor("*", "priorityclasses", func(action clienttesting.Action) (bool, runtime.Object, error) {
				switch {
				case action.GetVerb() == "list" && tc.listErr != nil:
					return true, nil, tc.listErr
				case action.GetVerb() == "create" && tc.createErr != nil:
					return true, nil, tc.createErr
				}
				return false, nil, nil
			})
			wantDone := !tc.wantRetry && !tc.wantErr
			if done, err := ensureSystemPriorityClasses(client.SchedulingV1()); done != wantDone || (err != nil) != tc.wantErr {
				t.Fatalf("ensureSystemPriorityClasses() = (%v, %v), want done=%v, error=%v", done, err, wantDone, tc.wantErr)
			}
			var requests []string
			for _, action := range client.Actions() {
				request := action.GetVerb() + " " + action.GetResource().Resource
				if create, ok := action.(clienttesting.CreateAction); ok {
					request += "/" + create.GetObject().(metav1.Object).GetName()
				}
				requests = append(requests, request)
			}
			if diff := cmp.Diff(tc.wantRequests, requests); diff != "" {
				t.Errorf("requests (-want +got):\n%s", diff)
			}
		})
	}
}

func BenchmarkEnsureSystemPriorityClasses(b *testing.B) {
	b.Run("empty-cluster", func(b *testing.B) {
		b.ReportAllocs()
		requests := 0
		for n := 0; n < b.N; n++ {
			b.StopTimer()
			client := fake.NewClientset()
			b.StartTimer()
			_, _ = ensureSystemPriorityClasses(client.SchedulingV1())
			requests += len(client.Actions())
		}
		b.ReportMetric(float64(requests)/float64(b.N), "requests/op")
	})
	b.Run("already-bootstrapped", func(b *testing.B) {
		client := fake.NewClientset()
		_, _ = ensureSystemPriorityClasses(client.SchedulingV1())
		b.ReportAllocs()
		b.ResetTimer()
		requests := 0
		for n := 0; n < b.N; n++ {
			client.ClearActions()
			_, _ = ensureSystemPriorityClasses(client.SchedulingV1())
			requests += len(client.Actions())
		}
		b.ReportMetric(float64(requests)/float64(b.N), "requests/op")
	})
}
