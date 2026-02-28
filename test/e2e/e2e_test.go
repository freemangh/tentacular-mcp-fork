//go:build e2e

package e2e_test

import (
	"context"
	"os"
	"testing"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// e2eClient loads a real Kubernetes client from the TENTACULAR_E2E_KUBECONFIG
// environment variable. Tests are skipped when the variable is unset.
func e2eClient(t *testing.T) *k8s.Client {
	t.Helper()

	kubeconfigPath := os.Getenv("TENTACULAR_E2E_KUBECONFIG")
	if kubeconfigPath == "" {
		t.Skip("TENTACULAR_E2E_KUBECONFIG not set, skipping e2e test")
	}

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		t.Fatalf("load e2e kubeconfig: %v", err)
	}

	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("create e2e clientset: %v", err)
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("create dynamic client: %v", err)
	}
	return k8s.NewClientFromConfig(cs, dyn, cfg, nil)
}

func TestE2E_NamespaceLifecycle(t *testing.T) {
	client := e2eClient(t)
	ctx := context.Background()
	nsName := "tentacular-e2e-lifecycle"

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
}

func TestE2E_WorkflowRBAC(t *testing.T) {
	client := e2eClient(t)
	ctx := context.Background()
	nsName := "tentacular-e2e-rbac"

	t.Cleanup(func() {
		_ = k8s.DeleteNamespace(context.Background(), client, nsName)
	})

	if err := k8s.CreateNamespace(ctx, client, nsName); err != nil {
		t.Fatalf("CreateNamespace: %v", err)
	}
	if err := k8s.CreateWorkflowServiceAccount(ctx, client, nsName); err != nil {
		t.Fatalf("CreateWorkflowServiceAccount: %v", err)
	}
	if err := k8s.CreateWorkflowRole(ctx, client, nsName); err != nil {
		t.Fatalf("CreateWorkflowRole: %v", err)
	}
	if err := k8s.CreateWorkflowRoleBinding(ctx, client, nsName); err != nil {
		t.Fatalf("CreateWorkflowRoleBinding: %v", err)
	}
}
