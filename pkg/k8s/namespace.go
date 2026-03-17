package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateNamespace creates a namespace with tentacular managed-by labels and
// Pod Security Admission labels set to restricted.
func CreateNamespace(ctx context.Context, client *Client, name string) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				ManagedByLabel:                               ManagedByValue,
				"pod-security.kubernetes.io/enforce":         "restricted",
				"pod-security.kubernetes.io/enforce-version": "latest",
			},
		},
	}

	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("namespace %q already exists: %w", name, err)
		}
		return fmt.Errorf("create namespace %q: %w", name, err)
	}
	return nil
}

// DeleteNamespace deletes the named namespace.
func DeleteNamespace(ctx context.Context, client *Client, name string) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	err := client.Clientset.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("namespace %q not found: %w", name, err)
		}
		return fmt.Errorf("delete namespace %q: %w", name, err)
	}
	return nil
}

// GetNamespace retrieves the named namespace.
func GetNamespace(ctx context.Context, client *Client, name string) (*corev1.Namespace, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("namespace %q not found: %w", name, err)
		}
		return nil, fmt.Errorf("get namespace %q: %w", name, err)
	}
	return ns, nil
}

// ListManagedNamespaces returns all namespaces with the tentacular managed-by label.
func ListManagedNamespaces(ctx context.Context, client *Client) ([]corev1.Namespace, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	list, err := client.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: ManagedByLabel + "=" + ManagedByValue,
	})
	if err != nil {
		return nil, fmt.Errorf("list managed namespaces: %w", err)
	}
	return list.Items, nil
}

// IsManagedNamespace returns true if the namespace has the tentacular managed-by label.
func IsManagedNamespace(ns *corev1.Namespace) bool {
	if ns == nil || ns.Labels == nil {
		return false
	}
	return ns.Labels[ManagedByLabel] == ManagedByValue
}

// CheckManagedNamespace fetches the namespace and returns an error if it is
// not managed by tentacular. Use this in write handlers to prevent operating
// on namespaces tentacular did not create (or adopt).
func CheckManagedNamespace(ctx context.Context, client *Client, name string) error {
	ns, err := GetNamespace(ctx, client, name)
	if err != nil {
		return err
	}
	if !IsManagedNamespace(ns) {
		return fmt.Errorf("namespace %q is not managed by tentacular; add label %s=%s to adopt it", name, ManagedByLabel, ManagedByValue)
	}
	return nil
}
