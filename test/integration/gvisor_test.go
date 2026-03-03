//go:build integration

package integration_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

func TestIntegration_GVisorCheckNoRuntime(t *testing.T) {
	client := integrationClient(t)
	ctx := context.Background()

	rcs, err := client.Clientset.NodeV1().RuntimeClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list RuntimeClasses: %v", err)
	}

	for _, rc := range rcs.Items {
		if rc.Handler == "gvisor" || rc.Handler == "runsc" {
			t.Skip("gvisor/runsc runtime found; skipping no-runtime test")
		}
	}

	t.Log("confirmed: no gvisor/runsc RuntimeClass on kind cluster")
}

func TestIntegration_GVisorAnnotateNsRejectsUnmanaged(t *testing.T) {
	client := integrationClient(t)
	ctx := context.Background()

	// Create a namespace without the tentacular managed-by label.
	unmanaged := "tnt-int-gv-unmanaged"
	t.Cleanup(func() {
		_ = client.Clientset.CoreV1().Namespaces().Delete(context.Background(), unmanaged, metav1.DeleteOptions{})
	})

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: unmanaged,
		},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create unmanaged namespace: %v", err)
	}

	// Verify it is not managed.
	got, err := k8s.GetNamespace(ctx, client, unmanaged)
	if err != nil {
		t.Fatalf("GetNamespace: %v", err)
	}
	if k8s.IsManagedNamespace(got) {
		t.Error("expected namespace to be unmanaged")
	}
}
