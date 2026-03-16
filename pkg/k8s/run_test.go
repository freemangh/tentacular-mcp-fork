package k8s

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

	var result map[string]any
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

// TestRunWorkflowRetriesOnDialError verifies that dial errors are retried until
// the context is canceled, and that the call succeeds once the server is up.
func TestRunWorkflowRetriesOnDialError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	var attempts atomic.Int32
	client := &Client{
		HTTP: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				n := attempts.Add(1)
				if n < 3 {
					// Simulate connection refused for the first two attempts.
					return nil, &net.OpError{Op: "dial", Err: &net.AddrError{Err: "connection refused"}}
				}
				req.URL.Scheme = "http"
				req.URL.Host = server.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(req)
			}),
		},
	}

	output, err := RunWorkflow(context.Background(), client, "ns", "wf", nil)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}

	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}
	if result["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", result["status"])
	}
}

// TestRunWorkflowContextCancelledDuringRetry verifies that a canceled context
// stops the retry loop and returns an error.
func TestRunWorkflowContextCancelledDuringRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var attempts atomic.Int32
	client := &Client{
		HTTP: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				attempts.Add(1)
				cancel() // cancel after first attempt
				return nil, &net.OpError{Op: "dial", Err: &net.AddrError{Err: "connection refused"}}
			}),
		},
	}

	_, err := RunWorkflow(ctx, client, "ns", "wf", nil)
	if err == nil {
		t.Fatal("expected error after context cancellation")
	}
	if attempts.Load() != 1 {
		t.Errorf("expected 1 attempt before cancel, got %d", attempts.Load())
	}
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
