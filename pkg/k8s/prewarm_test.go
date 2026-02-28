package k8s

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestBuildModuleURLJSR verifies the correct URL for a JSR dependency.
func TestBuildModuleURLJSR(t *testing.T) {
	dep := ModuleDep{Protocol: "jsr", Host: "@db/postgres", Version: "0.19.5"}
	url := buildModuleURL("tentacular-support", dep)
	expected := "http://esm-sh.tentacular-support.svc.cluster.local:8080/jsr/@db/postgres@0.19.5"
	if url != expected {
		t.Errorf("expected URL %q, got %q", expected, url)
	}
}

// TestBuildModuleURLNPM verifies the correct URL for an npm dependency.
func TestBuildModuleURLNPM(t *testing.T) {
	dep := ModuleDep{Protocol: "npm", Host: "lodash", Version: "4.17.21"}
	url := buildModuleURL("tentacular-support", dep)
	expected := "http://esm-sh.tentacular-support.svc.cluster.local:8080/lodash@4.17.21"
	if url != expected {
		t.Errorf("expected URL %q, got %q", expected, url)
	}
}

// TestBuildModuleURLNoVersion verifies the URL when version is omitted.
func TestBuildModuleURLNoVersion(t *testing.T) {
	dep := ModuleDep{Protocol: "npm", Host: "react", Version: ""}
	url := buildModuleURL("tentacular-support", dep)
	expected := "http://esm-sh.tentacular-support.svc.cluster.local:8080/react"
	if url != expected {
		t.Errorf("expected URL %q, got %q", expected, url)
	}
}

// TestBuildModuleURLJSRNoVersion verifies the JSR URL when version is omitted.
func TestBuildModuleURLJSRNoVersion(t *testing.T) {
	dep := ModuleDep{Protocol: "jsr", Host: "@std/path", Version: ""}
	url := buildModuleURL("tentacular-support", dep)
	expected := "http://esm-sh.tentacular-support.svc.cluster.local:8080/jsr/@std/path"
	if url != expected {
		t.Errorf("expected URL %q, got %q", expected, url)
	}
}

// TestPrewarmModulesCallsCorrectURLs verifies that PrewarmModules issues GET requests
// to the expected direct HTTP URLs for each dependency.
func TestPrewarmModulesCallsCorrectURLs(t *testing.T) {
	var mu sync.Mutex
	var capturedPaths []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedPaths = append(capturedPaths, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Route all requests to our test server
	client := &Client{
		HTTP: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				req.URL.Scheme = "http"
				req.URL.Host = server.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(req)
			}),
		},
	}

	deps := []ModuleDep{
		{Protocol: "jsr", Host: "@db/postgres", Version: "0.19.5"},
		{Protocol: "npm", Host: "zod", Version: "3.22.0"},
		{Protocol: "jsr", Host: "@std/path", Version: ""},
	}

	err := PrewarmModules(context.Background(), client, "tentacular-support", deps)
	if err != nil {
		t.Fatalf("PrewarmModules returned unexpected error: %v", err)
	}

	// Verify all three paths were called
	expectedPaths := []string{
		"/jsr/@db/postgres@0.19.5",
		"/zod@3.22.0",
		"/jsr/@std/path",
	}

	for _, expected := range expectedPaths {
		found := false
		for _, captured := range capturedPaths {
			if captured == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected path %q to be called, captured paths: %v", expected, capturedPaths)
		}
	}
}

// TestPrewarmModulesEmpty verifies that no requests are made for an empty dep list.
func TestPrewarmModulesEmpty(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{HTTP: server.Client()}

	err := PrewarmModules(context.Background(), client, "tentacular-support", nil)
	if err != nil {
		t.Fatalf("PrewarmModules returned unexpected error: %v", err)
	}
	if requestCount != 0 {
		t.Errorf("expected no requests for empty dep list, got %d", requestCount)
	}
}

// TestPrewarmModulesBestEffort verifies that PrewarmModules does not return an error
// even when some module requests fail (best-effort behavior).
func TestPrewarmModulesBestEffort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &Client{
		HTTP: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				req.URL.Scheme = "http"
				req.URL.Host = server.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(req)
			}),
		},
	}

	deps := []ModuleDep{
		{Protocol: "jsr", Host: "@db/postgres", Version: "0.19.5"},
	}

	err := PrewarmModules(context.Background(), client, "tentacular-support", deps)
	if err != nil {
		t.Errorf("expected nil error from best-effort PrewarmModules, got: %v", err)
	}
}

// TestPrewarmModulesProxyNamespaceInURL verifies that the proxy namespace is encoded in the URL.
func TestPrewarmModulesProxyNamespaceInURL(t *testing.T) {
	var capturedHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{
		HTTP: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				capturedHost = req.URL.Host
				req.URL.Scheme = "http"
				req.URL.Host = server.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(req)
			}),
		},
	}

	deps := []ModuleDep{{Protocol: "npm", Host: "react", Version: "18.0.0"}}
	if err := PrewarmModules(context.Background(), client, "my-proxy-ns", deps); err != nil {
		t.Fatalf("PrewarmModules error: %v", err)
	}

	if !strings.Contains(capturedHost, "my-proxy-ns") {
		t.Errorf("expected proxy namespace in host, got %q", capturedHost)
	}
}
