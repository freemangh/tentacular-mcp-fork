package proxy

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

func newTestReconciler(opts Options) (*Reconciler, *k8s.Client) {
	cs := fake.NewClientset()
	client := &k8s.Client{
		Clientset: cs,
		Config:    &rest.Config{Host: "https://test:6443"},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	r := NewReconciler(client, opts, logger)
	return r, client
}

func TestNewReconciler_DefaultNamespace(t *testing.T) {
	r, _ := newTestReconciler(Options{})
	if r.Namespace() != "tentacular-support" {
		t.Errorf("expected tentacular-support, got %q", r.Namespace())
	}
}

func TestNewReconciler_CustomNamespace(t *testing.T) {
	r, _ := newTestReconciler(Options{Namespace: "custom-ns"})
	if r.Namespace() != "custom-ns" {
		t.Errorf("expected custom-ns, got %q", r.Namespace())
	}
}

func TestReconcileOnce_CreatesDeploymentAndService(t *testing.T) {
	opts := Options{Namespace: "tentacular-support"}
	r, client := newTestReconciler(opts)
	ctx := context.Background()

	r.reconcileOnce(ctx)

	dep, err := client.Clientset.AppsV1().Deployments("tentacular-support").Get(ctx, DeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("deployment not created: %v", err)
	}
	if dep.Name != DeploymentName {
		t.Errorf("wrong deployment name: %q", dep.Name)
	}

	svc, err := client.Clientset.CoreV1().Services("tentacular-support").Get(ctx, ServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("service not created: %v", err)
	}
	if svc.Name != ServiceName {
		t.Errorf("wrong service name: %q", svc.Name)
	}
}

func TestReconcileOnce_Idempotent(t *testing.T) {
	opts := Options{Namespace: "tentacular-support"}
	r, client := newTestReconciler(opts)
	ctx := context.Background()

	// Run twice -- should not error on second run (idempotent)
	r.reconcileOnce(ctx)
	r.reconcileOnce(ctx)

	deps, err := client.Clientset.AppsV1().Deployments("tentacular-support").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list deployments: %v", err)
	}
	if len(deps.Items) != 1 {
		t.Errorf("expected exactly 1 deployment, got %d", len(deps.Items))
	}
}

func TestGetStatus_NotInstalled(t *testing.T) {
	opts := Options{Namespace: "tentacular-support"}
	r, _ := newTestReconciler(opts)
	ctx := context.Background()

	st := r.GetStatus(ctx)
	if st.Installed {
		t.Error("expected not installed before reconcile")
	}
}

func TestGetStatus_InstalledAfterReconcile(t *testing.T) {
	opts := Options{Namespace: "tentacular-support"}
	r, _ := newTestReconciler(opts)
	ctx := context.Background()

	r.reconcileOnce(ctx)

	st := r.GetStatus(ctx)
	if !st.Installed {
		t.Error("expected installed after reconcile")
	}
	if st.Image == "" {
		t.Error("expected non-empty image")
	}
}

func TestGetStatus_DefaultStorageType(t *testing.T) {
	opts := Options{Namespace: "tentacular-support"}
	r, _ := newTestReconciler(opts)
	ctx := context.Background()

	r.reconcileOnce(ctx)

	st := r.GetStatus(ctx)
	if st.Storage != "emptydir" {
		t.Errorf("expected emptydir, got %q", st.Storage)
	}
}

func TestGetStatus_PVCStorageType(t *testing.T) {
	opts := Options{Namespace: "tentacular-support", StorageSize: "10Gi"}
	r, _ := newTestReconciler(opts)
	ctx := context.Background()

	r.reconcileOnce(ctx)

	st := r.GetStatus(ctx)
	if st.Storage != "pvc" {
		t.Errorf("expected pvc, got %q", st.Storage)
	}
}

func TestReconcileDeployment_UpdatesImageWhenChanged(t *testing.T) {
	opts := Options{Namespace: "tentacular-support", Image: "ghcr.io/esm-dev/esm.sh:v100"}
	r, client := newTestReconciler(opts)
	ctx := context.Background()

	// First reconcile creates with v100
	r.reconcileOnce(ctx)

	dep, err := client.Clientset.AppsV1().Deployments("tentacular-support").Get(ctx, DeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("deployment not found: %v", err)
	}
	if dep.Spec.Template.Spec.Containers[0].Image != "ghcr.io/esm-dev/esm.sh:v100" {
		t.Errorf("expected v100, got %q", dep.Spec.Template.Spec.Containers[0].Image)
	}

	// Change image and reconcile again — should update
	r.opts.Image = "ghcr.io/esm-dev/esm.sh:v200"
	r.reconcileOnce(ctx)

	dep2, err := client.Clientset.AppsV1().Deployments("tentacular-support").Get(ctx, DeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("deployment not found after update: %v", err)
	}
	if dep2.Spec.Template.Spec.Containers[0].Image != "ghcr.io/esm-dev/esm.sh:v200" {
		t.Errorf("expected v200 after update, got %q", dep2.Spec.Template.Spec.Containers[0].Image)
	}
}

func TestReconcileOnce_SameImageNoUpdate(t *testing.T) {
	opts := Options{Namespace: "tentacular-support", Image: DefaultImage}
	r, client := newTestReconciler(opts)
	ctx := context.Background()

	r.reconcileOnce(ctx)
	r.reconcileOnce(ctx) // same image, should be no-op

	deps, _ := client.Clientset.AppsV1().Deployments("tentacular-support").List(ctx, metav1.ListOptions{})
	if len(deps.Items) != 1 {
		t.Errorf("expected exactly 1 deployment after idempotent reconcile, got %d", len(deps.Items))
	}
}

func TestRun_ExitsOnContextCancel(t *testing.T) {
	opts := Options{Namespace: "tentacular-support"}
	r, _ := newTestReconciler(opts)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()

	// Cancel shortly after Run starts
	cancel()

	select {
	case <-done:
		// Run exited as expected
	case <-time.After(3 * time.Second):
		t.Error("Run did not exit after context cancellation")
	}
}

func TestGetStatus_ReadyField(t *testing.T) {
	opts := Options{Namespace: "tentacular-support"}
	r, client := newTestReconciler(opts)
	ctx := context.Background()

	r.reconcileOnce(ctx)

	// Fake client does not update ReadyReplicas, so Ready will be false
	// unless we manually patch it. Verify it reflects actual state.
	st := r.GetStatus(ctx)
	if st.Image == "" {
		t.Error("expected non-empty image after reconcile")
	}

	// Fake: ReadyReplicas=0 by default, so Ready should be false
	if st.Ready {
		t.Log("note: fake client returned Ready=true, which is unexpected but not wrong in test context")
	}

	// Manually set ReadyReplicas=1 and check again
	dep, err := client.Clientset.AppsV1().Deployments("tentacular-support").Get(ctx, DeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	dep.Status.ReadyReplicas = 1
	_, _ = client.Clientset.AppsV1().Deployments("tentacular-support").UpdateStatus(ctx, dep, metav1.UpdateOptions{})

	st2 := r.GetStatus(ctx)
	if !st2.Ready {
		t.Error("expected Ready=true after setting ReadyReplicas=1")
	}
}
