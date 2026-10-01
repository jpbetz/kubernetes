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

package apiserver

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apiextensions-apiserver/test/integration/fixtures"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	genericfeatures "k8s.io/apiserver/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/dynamic"
	clientset "k8s.io/client-go/kubernetes"
	watchtools "k8s.io/client-go/tools/watch"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/klog/v2"
	kubeapiservertesting "k8s.io/kubernetes/cmd/kube-apiserver/app/testing"
	"k8s.io/kubernetes/test/integration/framework"
	"k8s.io/kubernetes/test/utils/ktesting"
)

func TestWatchResumesAfterShutdownBookmark(t *testing.T) {
	for _, tc := range []struct {
		name        string
		enabled     bool
		gracePeriod string
	}{
		{name: "enabled", enabled: true, gracePeriod: "0s"},
		{name: "enabled with watch termination grace period", enabled: true, gracePeriod: "5s"},
		{name: "disabled", enabled: false, gracePeriod: "0s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, genericfeatures.WatchCacheShutdownBookmark, tc.enabled)
			tCtx := ktesting.Init(t)

			etcdURL, stopEtcd, err := framework.RunCustomEtcd(klog.FromContext(tCtx).WithName("etcd"), "etcd_shutdown_bookmark", nil)
			if err != nil {
				t.Fatalf("Couldn't start etcd: %v", err)
			}
			defer stopEtcd()
			storageConfig := framework.SharedEtcd()
			storageConfig.Transport.ServerList = []string{etcdURL}
			flags := append(framework.DefaultTestServerFlags(), "--request-timeout=10s", "--shutdown-watch-termination-grace-period="+tc.gracePeriod)

			server := kubeapiservertesting.StartTestServerOrDie(t, nil, flags, storageConfig)
			client := clientset.NewForConfigOrDie(server.ClientConfig)
			dynamicClient := dynamic.NewForConfigOrDie(server.ClientConfig)

			ns := "bookmark"
			if _, err := client.CoreV1().Namespaces().Create(tCtx, &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			crd, err := fixtures.CreateNewV1CustomResourceDefinition(fixtures.NewNoxuV1CustomResourceDefinition(apiextensionsv1.ClusterScoped), apiextensionsclientset.NewForConfigOrDie(server.ClientConfig), dynamicClient)
			if err != nil {
				t.Fatal(err)
			}
			noxus := schema.GroupVersionResource{Group: crd.Spec.Group, Version: crd.Spec.Versions[0].Name, Resource: crd.Spec.Names.Plural}
			if _, err := dynamicClient.Resource(noxus).Create(tCtx, fixtures.NewNoxuInstance("", "foo"), metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}

			configMaps := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
			apiServices := schema.GroupVersionResource{Group: "apiregistration.k8s.io", Version: "v1", Resource: "apiservices"}
			type watchState struct {
				gvr    schema.GroupVersionResource
				cursor string
				last   watch.EventType
				ended  chan struct{}
			}
			var watches []*watchState
			for _, gvr := range []schema.GroupVersionResource{configMaps, noxus, apiServices} {
				namespace := ""
				if gvr == configMaps {
					namespace = ns
				}
				list, err := dynamicClient.Resource(gvr).Namespace(namespace).List(tCtx, metav1.ListOptions{})
				if err != nil {
					t.Fatal(err)
				}
				w, err := dynamicClient.Resource(gvr).Namespace(namespace).Watch(tCtx, metav1.ListOptions{ResourceVersion: list.GetResourceVersion(), AllowWatchBookmarks: true})
				if err != nil {
					t.Fatal(err)
				}
				state := &watchState{gvr: gvr, cursor: list.GetResourceVersion(), ended: make(chan struct{})}
				watches = append(watches, state)
				go func() {
					defer close(state.ended)
					defer w.Stop()
					for event := range w.ResultChan() {
						state.last = event.Type
						if accessor, err := meta.Accessor(event.Object); err == nil {
							state.cursor = accessor.GetResourceVersion()
						}
					}
				}()
			}

			var lastWrite string
			for i := range 5 {
				secret, err := client.CoreV1().Secrets(ns).Create(tCtx, &v1.Secret{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("s-%d", i)}}, metav1.CreateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				lastWrite = secret.ResourceVersion
			}
			server.TearDownFn()

			for _, state := range watches {
				select {
				case <-state.ended:
				case <-time.After(wait.ForeverTestTimeout):
					t.Fatalf("watch of %v did not end when the apiserver shut down", state.gvr)
				}
				t.Logf("watch of %v ended with %v at resourceVersion %s", state.gvr, state.last, state.cursor)
				if !tc.enabled {
					continue
				}
				if state.last != watch.Bookmark {
					t.Errorf("watch of %v ended with %q, want a final bookmark", state.gvr, state.last)
				}
				if parseRV(t, state.cursor) < parseRV(t, lastWrite) {
					t.Errorf("watch of %v ended at resourceVersion %s, want at least %s", state.gvr, state.cursor, lastWrite)
				}
			}

			server = kubeapiservertesting.StartTestServerOrDie(t, nil, flags, storageConfig)
			defer server.TearDownFn()
			dynamicClient = dynamic.NewForConfigOrDie(server.ClientConfig)
			afterRestart := map[schema.GroupVersionResource]*unstructured.Unstructured{
				configMaps: {Object: map[string]interface{}{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]interface{}{"name": "after-restart"}}},
				noxus:      fixtures.NewNoxuInstance("", "after-restart"),
				apiServices: {Object: map[string]interface{}{
					"apiVersion": "apiregistration.k8s.io/v1",
					"kind":       "APIService",
					"metadata":   map[string]interface{}{"name": "v1.after-restart.example.com"},
					"spec":       map[string]interface{}{"group": "after-restart.example.com", "version": "v1", "groupPriorityMinimum": int64(1000), "versionPriority": int64(15)},
				}},
			}
			for _, state := range watches {
				namespace := ""
				if state.gvr == configMaps {
					namespace = ns
				}
				var resumed watch.Interface
				if err := wait.PollUntilContextTimeout(tCtx, 100*time.Millisecond, wait.ForeverTestTimeout, true, func(context.Context) (bool, error) {
					resumed, err = dynamicClient.Resource(state.gvr).Namespace(namespace).Watch(tCtx, metav1.ListOptions{ResourceVersion: state.cursor})
					if apierrors.IsTooManyRequests(err) {
						return false, nil
					}
					return err == nil, err
				}); err != nil {
					t.Fatalf("Watch of %v at %s: %v", state.gvr, state.cursor, err)
				}
				created, err := dynamicClient.Resource(state.gvr).Namespace(namespace).Create(tCtx, afterRestart[state.gvr], metav1.CreateOptions{})
				if err != nil {
					t.Fatal(err)
				}

				ctx, cancel := context.WithTimeout(tCtx, wait.ForeverTestTimeout)
				defer cancel()
				event, err := watchtools.UntilWithoutRetry(ctx, resumed, func(event watch.Event) (bool, error) {
					accessor, err := meta.Accessor(event.Object)
					return event.Type == watch.Error || (err == nil && accessor.GetName() == created.GetName()), nil
				})
				if err != nil {
					t.Fatalf("Waiting for an event on the resumed watch of %v: %v", state.gvr, err)
				}
				if tc.enabled && state.gvr != noxus {
					if event.Type != watch.Added {
						t.Errorf("resumed watch of %v got %v %#v, want ADDED %s", state.gvr, event.Type, event.Object, created.GetName())
					}
				} else if event.Type != watch.Error || !apierrors.IsResourceExpired(apierrors.FromObject(event.Object)) {
					t.Errorf("resumed watch of %v got %v %#v, want 410 Expired", state.gvr, event.Type, event.Object)
				}
			}
		})
	}
}

func parseRV(t *testing.T, rv string) uint64 {
	t.Helper()
	parsed, err := strconv.ParseUint(rv, 10, 64)
	if err != nil {
		t.Fatalf("invalid resourceVersion %q: %v", rv, err)
	}
	return parsed
}
