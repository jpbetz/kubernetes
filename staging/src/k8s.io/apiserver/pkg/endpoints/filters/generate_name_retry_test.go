/*
Copyright 2023 The Kubernetes Authors.

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

package filters

import (
	"net/http"
	"net/http/httptest"
	"testing"

	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
)

func TestRetryGenerateNameRequest(t *testing.T) {
	testcases := []struct {
		name string
	}{
		{
			name: "retry once",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			var callCount int
			auth := WithRetryGenerateName(
				http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					if callCount == 0 {
						w.WriteHeader(409)
						w.Write([]byte("first body"))
					} else {
						w.WriteHeader(200)
						w.Write([]byte("second body"))
					}
					callCount++
				}),
			)
			r := &http.Request{Method: "POST"}
			r = r.WithContext(genericapirequest.WithRequestInfo(r.Context(), &genericapirequest.RequestInfo{Verb: "create"}))
			recorder := httptest.NewRecorder()
			auth.ServeHTTP(recorder, r)
			body := recorder.Body.String()
			if body != "second body" {
				t.Errorf("Expected 'second body' but got '%s'", body)
			}
			if callCount != 2 {
				t.Errorf("Expected 2 calls but got %d", callCount)
			}
		})
	}
}
