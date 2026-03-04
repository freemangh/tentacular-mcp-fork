package tools

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
	"github.com/randybias/tentacular-mcp/pkg/proxy"
)

func newProxyTestReconciler(namespace string) *proxy.Reconciler {
	cs := fake.NewClientset()
	client := &k8s.Client{
		Clientset: cs,
		Config:    &rest.Config{Host: "https://test:6443"},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return proxy.NewReconciler(client, proxy.Options{Namespace: namespace}, logger)
}

// TestRegisterProxyTools_NoPanic verifies registerProxyTools does not panic.
func TestRegisterProxyTools_NoPanic(t *testing.T) {
	srv := newTestServer()
	reconciler := newProxyTestReconciler("tentacular-support")
	// Should not panic
	registerProxyTools(srv, reconciler)
}

// TestProxyStatus_NotInstalled verifies proxy_status returns not installed
// when the reconciler has not run yet.
func TestProxyStatus_NotInstalled(t *testing.T) {
	reconciler := newProxyTestReconciler("tentacular-support")
	ctx := context.Background()

	st := reconciler.GetStatus(ctx)
	result := ProxyStatusResult{
		Installed: st.Installed,
		Ready:     st.Ready,
		Namespace: reconciler.Namespace(),
		Image:     st.Image,
		Storage:   st.Storage,
	}

	if result.Installed {
		t.Error("expected installed=false before reconcile")
	}
	if result.Namespace != "tentacular-support" {
		t.Errorf("expected namespace=tentacular-support, got %q", result.Namespace)
	}
}

// TestProxyStatus_InstalledAfterReconcile verifies proxy_status reflects
// installed state after reconciliation.
func TestProxyStatus_InstalledAfterReconcile(t *testing.T) {
	reconciler := newProxyTestReconciler("tentacular-support")
	ctx := context.Background()

	// Trigger reconciliation directly
	reconciler.Run(cancelAfterReconcile(ctx))

	st := reconciler.GetStatus(ctx)
	result := ProxyStatusResult{
		Installed: st.Installed,
		Ready:     st.Ready,
		Namespace: reconciler.Namespace(),
		Image:     st.Image,
		Storage:   st.Storage,
	}

	if !result.Installed {
		t.Error("expected installed=true after reconcile")
	}
	if result.Image == "" {
		t.Error("expected non-empty image")
	}
	if result.Storage != "emptydir" {
		t.Errorf("expected storage=emptydir, got %q", result.Storage)
	}
}

// TestProxyStatus_CustomNamespace verifies namespace propagation.
func TestProxyStatus_CustomNamespace(t *testing.T) {
	reconciler := newProxyTestReconciler("my-custom-ns")
	ctx := context.Background()

	st := reconciler.GetStatus(ctx)
	result := ProxyStatusResult{
		Installed: st.Installed,
		Namespace: reconciler.Namespace(),
	}

	if result.Namespace != "my-custom-ns" {
		t.Errorf("expected namespace=my-custom-ns, got %q", result.Namespace)
	}
}

// cancelAfterReconcile returns a context that is cancelled immediately,
// causing the reconciler's Run loop to exit after the initial reconcile.
func cancelAfterReconcile(parent context.Context) context.Context {
	ctx, cancel := context.WithCancel(parent)
	cancel() // cancel immediately so Run exits after first reconcileOnce
	return ctx
}
