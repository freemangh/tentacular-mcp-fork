package k8s_test

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

func newFakeK8sClient() (*fake.Clientset, *k8s.Client) {
	cs := fake.NewSimpleClientset()
	client := k8s.NewClientFromConfig(cs, nil, &rest.Config{Host: "https://fake:6443"}, nil)
	return cs, client
}

func TestCreateNamespace_HasManagedByLabel(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	if err := k8s.CreateNamespace(ctx, client, "my-ns"); err != nil {
		t.Fatalf("CreateNamespace: %v", err)
	}

	ns, err := cs.CoreV1().Namespaces().Get(ctx, "my-ns", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get namespace: %v", err)
	}

	if ns.Labels[k8s.ManagedByLabel] != k8s.ManagedByValue {
		t.Errorf("expected label %s=%s, got %q", k8s.ManagedByLabel, k8s.ManagedByValue, ns.Labels[k8s.ManagedByLabel])
	}
}

func TestCreateNamespace_HasPSALabels(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	if err := k8s.CreateNamespace(ctx, client, "psa-ns"); err != nil {
		t.Fatalf("CreateNamespace: %v", err)
	}

	ns, err := cs.CoreV1().Namespaces().Get(ctx, "psa-ns", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get namespace: %v", err)
	}

	if ns.Labels["pod-security.kubernetes.io/enforce"] != "restricted" {
		t.Errorf("expected PSA enforce=restricted, got %q", ns.Labels["pod-security.kubernetes.io/enforce"])
	}
	if ns.Labels["pod-security.kubernetes.io/enforce-version"] != "latest" {
		t.Errorf("expected PSA enforce-version=latest, got %q", ns.Labels["pod-security.kubernetes.io/enforce-version"])
	}
}

func TestCreateNamespace_AlreadyExists(t *testing.T) {
	_, client := newFakeK8sClient()
	ctx := context.Background()

	if err := k8s.CreateNamespace(ctx, client, "dup-ns"); err != nil {
		t.Fatalf("first CreateNamespace: %v", err)
	}
	err := k8s.CreateNamespace(ctx, client, "dup-ns")
	if err == nil {
		t.Error("expected error for duplicate namespace, got nil")
	}
}

func TestListManagedNamespaces_OnlyManaged(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	// Create one managed, one unmanaged namespace.
	managed := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "managed-ns",
			Labels: map[string]string{k8s.ManagedByLabel: k8s.ManagedByValue},
		},
	}
	unmanaged := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "unmanaged-ns"},
	}
	cs.CoreV1().Namespaces().Create(ctx, managed, metav1.CreateOptions{})
	cs.CoreV1().Namespaces().Create(ctx, unmanaged, metav1.CreateOptions{})

	// NOTE: fake client label selector support may return all; we test IsManagedNamespace logic.
	list, err := k8s.ListManagedNamespaces(ctx, client)
	if err != nil {
		t.Fatalf("ListManagedNamespaces: %v", err)
	}
	for _, ns := range list {
		if !k8s.IsManagedNamespace(&ns) {
			t.Errorf("unexpected unmanaged namespace in list: %s", ns.Name)
		}
	}
}

func TestIsManagedNamespace_True(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{k8s.ManagedByLabel: k8s.ManagedByValue},
		},
	}
	if !k8s.IsManagedNamespace(ns) {
		t.Error("expected IsManagedNamespace=true")
	}
}

func TestIsManagedNamespace_False(t *testing.T) {
	cases := []*corev1.Namespace{
		nil,
		{ObjectMeta: metav1.ObjectMeta{Labels: nil}},
		{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"other": "label"}}},
	}
	for _, ns := range cases {
		if k8s.IsManagedNamespace(ns) {
			t.Errorf("expected IsManagedNamespace=false for %+v", ns)
		}
	}
}

func TestDeleteNamespace_Success(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "del-ns"},
	}, metav1.CreateOptions{})

	if err := k8s.DeleteNamespace(ctx, client, "del-ns"); err != nil {
		t.Fatalf("DeleteNamespace: %v", err)
	}
}

func TestDeleteNamespace_NotFound(t *testing.T) {
	_, client := newFakeK8sClient()
	err := k8s.DeleteNamespace(context.Background(), client, "ghost-ns")
	if err == nil {
		t.Error("expected error for non-existent namespace, got nil")
	}
}

func TestCheckManagedNamespace_Managed(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "managed-ns",
			Labels: map[string]string{k8s.ManagedByLabel: k8s.ManagedByValue},
		},
	}, metav1.CreateOptions{})

	if err := k8s.CheckManagedNamespace(ctx, client, "managed-ns"); err != nil {
		t.Errorf("expected no error for managed namespace, got: %v", err)
	}
}

func TestCheckManagedNamespace_Unmanaged(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "unmanaged-ns"},
	}, metav1.CreateOptions{})

	err := k8s.CheckManagedNamespace(ctx, client, "unmanaged-ns")
	if err == nil {
		t.Error("expected error for unmanaged namespace, got nil")
	}
	if !strings.Contains(err.Error(), "not managed by tentacular") {
		t.Errorf("expected adoption hint in error, got: %v", err)
	}
}

func TestCheckManagedNamespace_NotFound(t *testing.T) {
	_, client := newFakeK8sClient()
	err := k8s.CheckManagedNamespace(context.Background(), client, "ghost-ns")
	if err == nil {
		t.Error("expected error for non-existent namespace, got nil")
	}
}
