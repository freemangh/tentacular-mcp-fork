package k8s

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRunWorkflowDirectHTTP verifies that RunWorkflow calls the workflow service
// directly via HTTP and returns the response body.
func TestRunWorkflowDirectHTTP(t *testing.T) {
	var capturedPath string
	var capturedMethod string
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"ok","stories":10}`))
	}))
	defer server.Close()

	client := &Client{HTTP: server.Client()}

	// Override the URL by using a custom transport that routes to our test server.
	// We test the HTTP mechanics; the real URL uses .svc.cluster.local.
	origHTTP := client.HTTP
	client.HTTP = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			// Rewrite the cluster-internal URL to our test server
			req.URL.Scheme = "http"
			req.URL.Host = server.Listener.Addr().String()
			return origHTTP.Transport.RoundTrip(req)
		}),
	}

	output, err := RunWorkflow(context.Background(), client, "tent-dev", "hn-digest", json.RawMessage(`{"filter":"ai"}`))
	if err != nil {
		t.Fatalf("RunWorkflow error: %v", err)
	}

	if capturedPath != "/run" {
		t.Errorf("expected path /run, got %q", capturedPath)
	}
	if capturedMethod != "POST" {
		t.Errorf("expected POST, got %s", capturedMethod)
	}
	if string(capturedBody) != `{"filter":"ai"}` {
		t.Errorf("expected payload {\"filter\":\"ai\"}, got %s", capturedBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}
	if result["result"] != "ok" {
		t.Errorf("expected result=ok, got %v", result["result"])
	}
}

// TestRunWorkflowDefaultPayload verifies that nil input sends an empty JSON object.
func TestRunWorkflowDefaultPayload(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{}`))
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

	_, err := RunWorkflow(context.Background(), client, "ns", "wf", nil)
	if err != nil {
		t.Fatalf("RunWorkflow error: %v", err)
	}

	if string(capturedBody) != `{}` {
		t.Errorf("expected default payload {}, got %s", capturedBody)
	}
}

// TestRunWorkflowHTTPError verifies that HTTP errors are propagated.
func TestRunWorkflowHTTPError(t *testing.T) {
	client := &Client{
		HTTP: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return nil, http.ErrServerClosed
			}),
		},
	}

	_, err := RunWorkflow(context.Background(), client, "ns", "wf", nil)
	if err == nil {
		t.Fatal("expected error for failed HTTP request")
	}
}

// TestRunWorkflowNonTwoxxStatusReturnsError verifies that 4xx/5xx responses are errors.
func TestRunWorkflowNonTwoxxStatusReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"something went wrong"}`)) //nolint:errcheck
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

	_, err := RunWorkflow(context.Background(), client, "ns", "wf", nil)
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
