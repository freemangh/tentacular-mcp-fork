package testutil

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ManagedByLabel is the label key applied to all tentacular-managed resources.
	ManagedByLabel = "app.kubernetes.io/managed-by"
	// ManagedByValue is the label value for tentacular-managed resources.
	ManagedByValue = "tentacular"

	// ProtectedNamespace is the system namespace that must be blocked.
	ProtectedNamespace = "tentacular-system"

	// SampleNamespace is a generic test namespace name.
	SampleNamespace = "test-workflow-ns"

	// TestToken is a static bearer token for auth tests.
	TestToken = "test-bearer-token-abc123"
)

// ManagedNamespace returns a Namespace object with the tentacular managed-by label.
func ManagedNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				ManagedByLabel:                               ManagedByValue,
				"pod-security.kubernetes.io/enforce":         "restricted",
				"pod-security.kubernetes.io/enforce-version": "latest",
			},
		},
		Status: corev1.NamespaceStatus{
			Phase: corev1.NamespaceActive,
		},
	}
}

// UnmanagedNamespace returns a Namespace object without managed-by label.
func UnmanagedNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: corev1.NamespaceStatus{
			Phase: corev1.NamespaceActive,
		},
	}
}
