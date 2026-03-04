package k8s_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

func TestCreateWorkflowServiceAccount_Created(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	if err := k8s.CreateWorkflowServiceAccount(ctx, client, "test-ns"); err != nil {
		t.Fatalf("CreateWorkflowServiceAccount: %v", err)
	}

	sa, err := cs.CoreV1().ServiceAccounts("test-ns").Get(ctx, "tentacular-workflow", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get SA: %v", err)
	}
	if sa.Labels[k8s.ManagedByLabel] != k8s.ManagedByValue {
		t.Errorf("SA missing managed-by label")
	}
}

func TestCreateWorkflowServiceAccount_AlreadyExists(t *testing.T) {
	_, client := newFakeK8sClient()
	ctx := context.Background()

	_ = k8s.CreateWorkflowServiceAccount(ctx, client, "test-ns")
	err := k8s.CreateWorkflowServiceAccount(ctx, client, "test-ns")
	if err == nil {
		t.Error("expected error for duplicate SA, got nil")
	}
}

func TestCreateWorkflowRole_CorrectVerbs(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	if err := k8s.CreateWorkflowRole(ctx, client, "test-ns"); err != nil {
		t.Fatalf("CreateWorkflowRole: %v", err)
	}

	role, err := cs.RbacV1().Roles("test-ns").Get(ctx, "tentacular-workflow", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get Role: %v", err)
	}

	// Verify that at least one rule contains create, update, delete verbs (for deployments).
	foundCUD := false
	for _, rule := range role.Rules {
		hasCreate, hasUpdate, hasDelete := false, false, false
		for _, v := range rule.Verbs {
			switch v {
			case "create":
				hasCreate = true
			case "update":
				hasUpdate = true
			case "delete":
				hasDelete = true
			}
		}
		if hasCreate && hasUpdate && hasDelete {
			foundCUD = true
			break
		}
	}
	if !foundCUD {
		t.Error("expected a role rule with create/update/delete verbs")
	}
}

func TestCreateWorkflowRole_ContainsDeployments(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	_ = k8s.CreateWorkflowRole(ctx, client, "test-ns")
	role, err := cs.RbacV1().Roles("test-ns").Get(ctx, "tentacular-workflow", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get Role: %v", err)
	}

	found := false
	for _, rule := range role.Rules {
		for _, res := range rule.Resources {
			if res == "deployments" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected role to contain deployments resource")
	}
}

func TestCreateWorkflowRoleBinding_LinksCorrectly(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	if err := k8s.CreateWorkflowRoleBinding(ctx, client, "test-ns"); err != nil {
		t.Fatalf("CreateWorkflowRoleBinding: %v", err)
	}

	rb, err := cs.RbacV1().RoleBindings("test-ns").Get(ctx, "tentacular-workflow", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get RoleBinding: %v", err)
	}

	if rb.RoleRef.Name != "tentacular-workflow" {
		t.Errorf("expected RoleRef.Name=tentacular-workflow, got %q", rb.RoleRef.Name)
	}
	if len(rb.Subjects) == 0 {
		t.Fatal("expected at least one subject")
	}
	if rb.Subjects[0].Name != "tentacular-workflow" {
		t.Errorf("expected subject Name=tentacular-workflow, got %q", rb.Subjects[0].Name)
	}
	if rb.Subjects[0].Namespace != "test-ns" {
		t.Errorf("expected subject Namespace=test-ns, got %q", rb.Subjects[0].Namespace)
	}
}

func TestCreateWorkflowRoleBinding_AlreadyExists(t *testing.T) {
	_, client := newFakeK8sClient()
	ctx := context.Background()

	_ = k8s.CreateWorkflowRoleBinding(ctx, client, "test-ns")
	err := k8s.CreateWorkflowRoleBinding(ctx, client, "test-ns")
	if err == nil {
		t.Error("expected error for duplicate RoleBinding, got nil")
	}
}
