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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"

	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/endpoints/responsewriter"
)

// WithRetryGenerateName decorates a http.Handler with a retry loop that repeats
// create and apply requests that result in a GeneratedNameError.
func WithRetryGenerateName(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// TODO: Only wrap with retry if the request is a create or an apply with an empty name and a non-empty
		// generateName.

		ctx := req.Context()
		requestInfo, found := request.RequestInfoFrom(ctx)
		if !found {
			handleError(w, req, http.StatusInternalServerError, fmt.Errorf("no RequestInfo found in context, handler chain must be wrong"))
			return
		}
		// TODO: Check for apply (verb=patch and content type "application/apply-patch+yaml")
		if !(requestInfo.Verb == "create") {
			handler.ServeHTTP(w, req)
			return
		}

		tracker := &errorTracker{}
		// TODO: Check for name and generateName, in the body, somehow?
		respWriter := decorateGenerateNameResponseWriter(ctx, w, tracker)

		handler.ServeHTTP(respWriter, req)

		// TODO: verify that this is a generate name error
		// tracker.errorBody.String()

		// TODO: retry more than once?
		if tracker.isGeneratedNameError == true {
			handler.ServeHTTP(w, req)
		}
	})
}

func decorateGenerateNameResponseWriter(ctx context.Context, responseWriter http.ResponseWriter, errorTracker *errorTracker) http.ResponseWriter {
	delegate := &generateNameResponseWriter{
		ctx:            ctx,
		ResponseWriter: responseWriter,
		errorTracker:   errorTracker,
	}

	return responsewriter.WrapForHTTP1Or2(delegate)
}

var _ http.ResponseWriter = &generateNameResponseWriter{}
var _ responsewriter.UserProvidedDecorator = &generateNameResponseWriter{}

type errorTracker struct {
	isGeneratedNameError bool
	errorBody            bytes.Buffer
}

// generateNameResponseWriter intercepts WriteHeader, checks if the response is a GeneratedNameError
// error and
type generateNameResponseWriter struct {
	http.ResponseWriter
	ctx          context.Context
	errorTracker *errorTracker
	once         sync.Once
}

func (a *generateNameResponseWriter) Unwrap() http.ResponseWriter {
	return a.ResponseWriter
}

func (a *generateNameResponseWriter) processCode(code int) {
	a.once.Do(func() {
		// TODO: also check that name is empty in error?
		// This is only needed if we do this for all creates,
		// not just creates with empty name and non-empty generateName
		a.errorTracker.isGeneratedNameError = code == 409
	})
}

func (a *generateNameResponseWriter) Write(bs []byte) (int, error) {
	if !a.errorTracker.isGeneratedNameError {
		return a.ResponseWriter.Write(bs)
	} else {
		a.errorTracker.errorBody.Write(bs)
	}
	return len(bs), nil
}

func (a *generateNameResponseWriter) WriteHeader(code int) {
	a.processCode(code)
	if !a.errorTracker.isGeneratedNameError {
		a.ResponseWriter.WriteHeader(code)
	}
}

func (a *generateNameResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	// the outer ResponseWriter object returned by WrapForHTTP1Or2 implements
	// http.Hijacker if the inner object (a.ResponseWriter) implements http.Hijacker.
	return a.ResponseWriter.(http.Hijacker).Hijack()
}
