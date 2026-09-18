// Copyright 2025 Palantir Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestGetAuthTokenRejectsRedirects(t *testing.T) {
	for _, statusCode := range []int{
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	} {
		for _, sameOrigin := range []bool{true, false} {
			t.Run(fmt.Sprintf("%d/same_origin=%t", statusCode, sameOrigin), func(t *testing.T) {
				var redirectedRequests atomic.Int32
				redirectHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					redirectedRequests.Add(1)
					_, _ = w.Write([]byte(`{"access_token":"redirected-token"}`))
				})
				target := httptest.NewServer(redirectHandler)
				defer target.Close()

				location := target.URL + "/redirected"
				if sameOrigin {
					location = "/redirected"
				}

				mux := http.NewServeMux()
				mux.HandleFunc("/multipass/api/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, location, statusCode)
				})
				mux.Handle("/redirected", redirectHandler)
				server := httptest.NewServer(mux)
				defer server.Close()

				token, err := GetAuthToken(server.URL+"/multipass/api", "test-client", "test-secret")
				if err == nil {
					t.Error("Expected authentication to fail on a redirect")
				} else if want := fmt.Sprintf("received status code %d from the server", statusCode); err.Error() != want {
					t.Errorf("Expected error %q, got %q", want, err.Error())
				}
				if token != "" {
					t.Errorf("Expected no token, got %q", token)
				}
				if got := redirectedRequests.Load(); got != 0 {
					t.Errorf("Expected no requests to the redirect target, got %d", got)
				}
			})
		}
	}
}

func TestGetAuthTokenSuccess(t *testing.T) {
	const clientID = "test-client"
	const clientSecret = "test-secret&with=special+characters"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/multipass/api/oauth2/token" {
			t.Errorf("Unexpected token endpoint: %s", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("Unexpected Content-Type: %s", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("Failed to parse authentication form: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for key, want := range map[string]string{
			"grant_type":    "client_credentials",
			"client_id":     clientID,
			"client_secret": clientSecret,
		} {
			if got := r.PostForm.Get(key); got != want {
				t.Errorf("Expected %s=%q, got %q", key, want, got)
			}
		}
		_, _ = w.Write([]byte(`{"access_token":"test-token"}`))
	}))
	defer server.Close()

	token, err := GetAuthToken(server.URL+"/multipass/api", clientID, clientSecret)
	if err != nil {
		t.Fatalf("Expected successful authentication, got %v", err)
	}
	if token != "test-token" {
		t.Errorf("Expected test-token, got %q", token)
	}
}
