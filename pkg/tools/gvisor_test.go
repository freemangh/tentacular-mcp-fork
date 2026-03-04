package tools

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

func newGVisorTestClient() *k8s.Client {
	return &k8s.Client{
		Clientset: fake.NewClientset(),
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}
}

func TestGVisorCheckNotAvailable(t *testing.T) {
	client := newGVisorTestClient()
	ctx := context.Background()

	result, err := handleGVisorCheck(ctx, client)
	if err != nil {
		t.Fatalf("handleGVisorCheck: %v", err)
	}
	if result.Available {
		t.Error("expected Available=false when no RuntimeClass exists")
	}
	if result.Guidance == "" {
		t.Error("expected Guidance to be set when not available")
	}
}

func TestGVisorCheckAvailable(t *testing.T) {
	client := newGVisorTestClient()
	ctx := context.Background()

	rc := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "gvisor"},
		Handler:    "runsc",
	}
	_, err := client.Clientset.NodeV1().RuntimeClasses().Create(ctx, rc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: create RuntimeClass: %v", err)
	}

	result, err := handleGVisorCheck(ctx, client)
	if err != nil {
		t.Fatalf("handleGVisorCheck: %v", err)
	}
	if !result.Available {
		t.Error("expected Available=true when RuntimeClass with runsc handler exists")
	}
	if result.RuntimeClass != "gvisor" {
		t.Errorf("runtime_class: got %q, want %q", result.RuntimeClass, "gvisor")
	}
}

func TestGVisorCheckAvailableGVisorHandler(t *testing.T) {
	client := newGVisorTestClient()
	ctx := context.Background()

	rc := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "sandbox"},
		Handler:    "gvisor",
	}
	_, _ = client.Clientset.NodeV1().RuntimeClasses().Create(ctx, rc, metav1.CreateOptions{})

	result, err := handleGVisorCheck(ctx, client)
	if err != nil {
		t.Fatalf("handleGVisorCheck: %v", err)
	}
	if !result.Available {
		t.Error("expected Available=true when RuntimeClass handler contains 'gvisor'")
	}
	if result.Handler != "gvisor" {
		t.Errorf("expected Handler=gvisor, got %q", result.Handler)
	}
}

func TestGVisorCheckNotAvailableNonGVisorHandler(t *testing.T) {
	client := newGVisorTestClient()
	ctx := context.Background()

	rc := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "kata"},
		Handler:    "kata-containers",
	}
	_, _ = client.Clientset.NodeV1().RuntimeClasses().Create(ctx, rc, metav1.CreateOptions{})

	result, err := handleGVisorCheck(ctx, client)
	if err != nil {
		t.Fatalf("handleGVisorCheck: %v", err)
	}
	if result.Available {
		t.Error("expected Available=false for non-gVisor RuntimeClass")
	}
}

func TestGVisorAnnotateNsUnmanagedNamespaceFails(t *testing.T) {
	client := newGVisorTestClient()
	ctx := context.Background()

	// Create unmanaged namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "unmanaged"},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err = handleGVisorAnnotateNs(ctx, client, GVisorAnnotateNsParams{Namespace: "unmanaged"})
	if err == nil {
		t.Fatal("expected error for unmanaged namespace, got nil")
	}
}

func TestGVisorAnnotateNsNoRuntimeClassFails(t *testing.T) {
	client := newGVisorTestClient()
	ctx := context.Background()

	// Create managed namespace but no RuntimeClass
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "managed-ns",
			Labels: map[string]string{k8s.ManagedByLabel: k8s.ManagedByValue},
		},
	}
	_, _ = client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	_, err := handleGVisorAnnotateNs(ctx, client, GVisorAnnotateNsParams{Namespace: "managed-ns"})
	if err == nil {
		t.Fatal("expected error when no gVisor RuntimeClass exists, got nil")
	}
}

func TestGVisorAnnotateNsSuccess(t *testing.T) {
	client := newGVisorTestClient()
	ctx := context.Background()

	// Create managed namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "gv-ns",
			Labels: map[string]string{k8s.ManagedByLabel: k8s.ManagedByValue},
		},
	}
	_, _ = client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	// Create gVisor RuntimeClass
	rc := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "gvisor"},
		Handler:    "runsc",
	}
	_, _ = client.Clientset.NodeV1().RuntimeClasses().Create(ctx, rc, metav1.CreateOptions{})

	result, err := handleGVisorAnnotateNs(ctx, client, GVisorAnnotateNsParams{Namespace: "gv-ns"})
	if err != nil {
		t.Fatalf("handleGVisorAnnotateNs: %v", err)
	}
	if !result.Applied {
		t.Error("expected Applied=true")
	}
	if result.Namespace != "gv-ns" {
		t.Errorf("expected Namespace=gv-ns, got %q", result.Namespace)
	}
	if result.Annotation == "" {
		t.Error("expected Annotation to be set")
	}
}

func TestGVisorAnnotateNsNonExistentNamespaceFails(t *testing.T) {
	client := newGVisorTestClient()
	ctx := context.Background()

	_, err := handleGVisorAnnotateNs(ctx, client, GVisorAnnotateNsParams{Namespace: "ghost-ns"})
	if err == nil {
		t.Fatal("expected error for non-existent namespace, got nil")
	}
}
