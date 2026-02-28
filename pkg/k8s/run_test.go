package k8s

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// TestRunWorkflowUsesServiceProxy verifies that RunWorkflow calls the
// K8s API service proxy path and returns the response body.
func TestRunWorkflowUsesServiceProxy(t *testing.T) {
	// Start a fake API server that captures the proxy request
	var capturedPath string
	var capturedMethod string
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		capturedBody, _ = readAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"result":"ok","stories":10}`))
	}))
	defer server.Close()

	clientset, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Clientset: clientset, Config: &rest.Config{Host: server.URL}}

	output, err := RunWorkflow(context.Background(), client, "tent-dev", "hn-digest", json.RawMessage(`{"filter":"ai"}`))
	if err != nil {
		t.Fatalf("RunWorkflow error: %v", err)
	}

	// Verify the proxy path
	expectedPath := "/api/v1/namespaces/tent-dev/services/hn-digest:8080/proxy/run"
	if capturedPath != expectedPath {
		t.Errorf("expected path %q, got %q", expectedPath, capturedPath)
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
		capturedBody, _ = readAll(r.Body)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	clientset, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Clientset: clientset, Config: &rest.Config{Host: server.URL}}

	_, err = RunWorkflow(context.Background(), client, "ns", "wf", nil)
	if err != nil {
		t.Fatalf("RunWorkflow error: %v", err)
	}

	if string(capturedBody) != `{}` {
		t.Errorf("expected default payload {}, got %s", capturedBody)
	}
}

// TestRunWorkflowProxyError verifies that API server errors are propagated.
func TestRunWorkflowProxyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"message":"service unavailable"}`))
	}))
	defer server.Close()

	clientset, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Clientset: clientset, Config: &rest.Config{Host: server.URL}}

	_, err = RunWorkflow(context.Background(), client, "ns", "wf", nil)
	if err == nil {
		t.Fatal("expected error for 502 response")
	}
}

func readAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var buf []byte
	tmp := make([]byte, 1024)
	for {
		n, err := r.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return buf, nil
}
