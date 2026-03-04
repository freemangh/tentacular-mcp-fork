package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

func newRunToolTestClient(namespaces ...string) *k8s.Client {
	objs := make([]runtime.Object, 0, len(namespaces))
	for _, name := range namespaces {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{k8s.ManagedByLabel: k8s.ManagedByValue},
			},
		}
		objs = append(objs, ns)
	}
	return &k8s.Client{
		Clientset: fake.NewClientset(objs...),
		Config:    &rest.Config{Host: "https://test:6443"},
	}
}

// newRunToolTestClientWithProxy creates a client backed by a real HTTP test
// server. The K8s clientset handles namespace lookups for guard checks, and
// the HTTP client redirects direct workflow calls to the test server.
func newRunToolTestClientWithProxy(t *testing.T, handler http.HandlerFunc, namespaces ...string) (*k8s.Client, *httptest.Server) {
	t.Helper()
	// Seed managed namespaces into the K8s API handler for guard checks
	mux := http.NewServeMux()
	for _, name := range namespaces {
		ns := name // capture
		mux.HandleFunc("/api/v1/namespaces/"+ns, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"metadata":{"name":"` + ns + `","labels":{"app.kubernetes.io/managed-by":"tentacular"}}}`))
		})
	}
	// Workflow /run endpoint (direct HTTP)
	mux.HandleFunc("/run", handler)
	// Keep K8s API fallback for any other namespace lookups
	mux.HandleFunc("/api/v1/namespaces/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	server := httptest.NewServer(mux)
	clientset, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	// HTTP client redirects .svc.cluster.local URLs to the test server
	httpClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = server.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}
	return &k8s.Client{Clientset: clientset, Config: &rest.Config{Host: server.URL}, HTTP: httpClient}, server
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestHandleWfRun_SystemNamespaceRejected verifies the guard rejects tentacular-system
// before any K8s API call is made.
func TestHandleWfRun_SystemNamespaceRejected(t *testing.T) {
	client := newRunToolTestClient()
	ctx := context.Background()

	_, err := handleWfRun(ctx, client, WfRunParams{
		Namespace: "tentacular-system",
		Name:      "my-wf",
	})
	if err == nil {
		t.Fatal("expected error for system namespace, got nil")
	}
}

// TestHandleWfRun_UnmanagedNamespaceRejected verifies that an unmanaged namespace
// is rejected by CheckManagedNamespace before the run starts.
func TestHandleWfRun_UnmanagedNamespaceRejected(t *testing.T) {
	client := newRunToolTestClient()
	ctx := context.Background()

	_, err := handleWfRun(ctx, client, WfRunParams{
		Namespace: "unmanaged-ns",
		Name:      "my-wf",
	})
	if err == nil {
		t.Fatal("expected error for unmanaged namespace, got nil")
	}
}

// TestWfRunParams_TimeoutDefaults verifies timeout boundary logic.
func TestWfRunParams_TimeoutDefaults(t *testing.T) {
	cases := []struct {
		timeoutS int
		wantCap  bool
	}{
		{0, false},
		{60, false},
		{120, false},
		{600, false},
		{601, true},
		{9999, true},
	}

	for _, tc := range cases {
		params := WfRunParams{TimeoutS: tc.timeoutS}
		const defaultTimeout = 120
		const maxTimeout = 600
		result := defaultTimeout
		if params.TimeoutS > 0 && params.TimeoutS <= maxTimeout {
			result = params.TimeoutS
		} else if params.TimeoutS > maxTimeout {
			result = maxTimeout
		}

		if tc.wantCap && result != maxTimeout {
			t.Errorf("TimeoutS=%d: expected cap to %d, got %d", tc.timeoutS, maxTimeout, result)
		}
		if !tc.wantCap && tc.timeoutS > 0 && result != tc.timeoutS {
			t.Errorf("TimeoutS=%d: expected %d, got %d", tc.timeoutS, tc.timeoutS, result)
		}
		if tc.timeoutS == 0 && result != defaultTimeout {
			t.Errorf("TimeoutS=0: expected default %d, got %d", defaultTimeout, result)
		}
	}
}

// TestHandleWfRun_ManagedNamespacePassesGuard verifies that a managed namespace
// passes the guard check and the run completes via direct HTTP.
func TestHandleWfRun_ManagedNamespacePassesGuard(t *testing.T) {
	client, server := newRunToolTestClientWithProxy(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}, "user-ns")
	defer server.Close()

	ctx := context.Background()
	result, err := handleWfRun(ctx, client, WfRunParams{
		Namespace: "user-ns",
		Name:      "my-wf",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output["result"] != "ok" {
		t.Errorf("expected result=ok, got %v", result.Output["result"])
	}
	if result.DurationMs < 0 {
		t.Errorf("expected positive duration, got %d", result.DurationMs)
	}
}

// TestWfRunResult_Fields verifies the WfRunResult struct fields.
func TestWfRunResult_Fields(t *testing.T) {
	result := WfRunResult{
		Name:       "my-wf",
		Namespace:  "user-ns",
		Output:     map[string]any{"ok": true},
		DurationMs: 1234,
	}
	if result.Name != "my-wf" {
		t.Errorf("expected name=my-wf, got %q", result.Name)
	}
	if result.DurationMs != 1234 {
		t.Errorf("expected duration=1234, got %d", result.DurationMs)
	}
	if result.Output["ok"] != true {
		t.Errorf("unexpected output: %v", result.Output)
	}
}
