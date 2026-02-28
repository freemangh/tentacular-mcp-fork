//go:build integration

package integration_test

import (
	"context"
	"os"
	"testing"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// integrationClient returns a real k8s.Client from the TENTACULAR_INT_KUBECONFIG env var,
// falling back to the default kubeconfig. Tests are skipped if the cluster is unreachable.
func integrationClient(t *testing.T) *k8s.Client {
	t.Helper()

	kubeconfigPath := os.Getenv("TENTACULAR_INT_KUBECONFIG")
	if kubeconfigPath == "" {
		kubeconfigPath = os.ExpandEnv("${HOME}/.kube/config")
	}

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		t.Skipf("skip integration test: cannot load kubeconfig: %v", err)
	}

	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Skipf("skip integration test: cannot create clientset: %v", err)
	}

	// Smoke-test connectivity.
	if _, err := cs.Discovery().ServerVersion(); err != nil {
		t.Skipf("skip integration test: cluster unreachable: %v", err)
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("create dynamic client: %v", err)
	}
	return k8s.NewClientFromConfig(cs, dyn, cfg, nil)
}

func TestIntegration_CreateAndDeleteNamespace(t *testing.T) {
	client := integrationClient(t)
	ctx := context.Background()
	nsName := "tentacular-int-test-ns"

	t.Cleanup(func() {
		_ = k8s.DeleteNamespace(context.Background(), client, nsName)
	})

	if err := k8s.CreateNamespace(ctx, client, nsName); err != nil {
		t.Fatalf("CreateNamespace: %v", err)
	}

	ns, err := k8s.GetNamespace(ctx, client, nsName)
	if err != nil {
		t.Fatalf("GetNamespace: %v", err)
	}

	if !k8s.IsManagedNamespace(ns) {
		t.Error("expected namespace to be managed")
	}

	if err := k8s.DeleteNamespace(ctx, client, nsName); err != nil {
		t.Fatalf("DeleteNamespace: %v", err)
	}
}
