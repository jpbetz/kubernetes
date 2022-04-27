/*
Copyright 2022 The Kubernetes Authors.

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

package validating

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	"github.com/stretchr/testify/assert"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	webhooktesting "k8s.io/apiserver/pkg/admission/plugin/webhook/testing"
	auditinternal "k8s.io/apiserver/pkg/apis/audit"
)

type testRuntime struct{}

func (t *testRuntime) Eval(program cel.Program, object runtime.Object) (ref.Val, error) {
	result, _, err := program.Eval(map[string]interface{}{})
	return result, err
}

func (t *testRuntime) Compile(rule string, kind schema.GroupVersionKind) (cel.Program, error) {
	env, err := cel.NewEnv()
	if err != nil {
		return nil, err
	}
	ast, issues := env.Compile(rule)
	if len(issues.Errors()) > 0 {
		return nil, issues.Err()
	}
	return env.Program(ast)
}

// TestValidate tests that ValidatingWebhook#Validate works as expected
func TestValidate(t *testing.T) {
	testServer := webhooktesting.NewTestServer(t)
	testServer.StartTLS()
	defer testServer.Close()

	objectInterfaces := webhooktesting.NewObjectInterfacesForTest()

	serverURL, err := url.ParseRequestURI(testServer.URL)
	if err != nil {
		t.Fatalf("this should never happen? %v", err)
	}

	stopCh := make(chan struct{})
	defer close(stopCh)

	for _, tt := range webhooktesting.NewNonMutatingRuleTestCases(serverURL) {
		t.Run(tt.Name, func(t *testing.T) {
			wh, err := NewValidatingAdmissionRules(nil)
			if err != nil {
				t.Errorf("%s: failed to create validating webhook: %v", tt.Name, err)
				return
			}

			ns := "rule-test"
			client, informer := webhooktesting.NewFakeValidatingRuleDataSource(ns, tt.Rules, stopCh)

			wh.SetExternalKubeClientSet(client)
			wh.SetExternalKubeInformerFactory(informer)
			wh.SetExpressionRuntime(&testRuntime{})

			informer.Start(stopCh)
			informer.WaitForCacheSync(stopCh)

			if err = wh.ValidateInitialization(); err != nil {
				t.Errorf("%s: failed to validate initialization: %v", tt.Name, err)
				return
			}

			attr := webhooktesting.NewAttribute(ns, nil, tt.IsDryRun)
			err = wh.Validate(context.TODO(), attr, objectInterfaces)
			if tt.ExpectAllow != (err == nil) {
				t.Errorf("%s: expected allowed=%v, but got err=%v", tt.Name, tt.ExpectAllow, err)
			}
			// ErrWebhookRejected is not an error for our purposes
			if tt.ErrorContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.ErrorContains) {
					t.Errorf("%s: expected an error saying %q, but got %v", tt.Name, tt.ErrorContains, err)
				}
			}
			fakeAttr, ok := attr.(*webhooktesting.FakeAttributes)
			if !ok {
				t.Errorf("Unexpected error, failed to convert attr to webhooktesting.FakeAttributes")
				return
			}
			if len(tt.ExpectAnnotations) == 0 {
				assert.Empty(t, fakeAttr.GetAnnotations(auditinternal.LevelMetadata), tt.Name+": annotations not set as expected.")
			} else {
				assert.Equal(t, tt.ExpectAnnotations, fakeAttr.GetAnnotations(auditinternal.LevelMetadata), tt.Name+": annotations not set as expected.")
			}
		})
	}
}
