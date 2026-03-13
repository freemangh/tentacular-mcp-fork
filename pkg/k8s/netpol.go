package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// CreateDefaultDenyPolicy creates a NetworkPolicy that denies all ingress and
// egress traffic in the given namespace.
func CreateDefaultDenyPolicy(ctx context.Context, client *Client, namespace string) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-deny",
			Namespace: namespace,
			Labels: map[string]string{
				ManagedByLabel: ManagedByValue,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
		},
	}

	_, err := client.Clientset.NetworkingV1().NetworkPolicies(namespace).Create(ctx, policy, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("network policy %q already exists in namespace %q: %w", "default-deny", namespace, err)
		}
		return fmt.Errorf("create default-deny network policy in namespace %q: %w", namespace, err)
	}
	return nil
}

// CreateDNSAllowPolicy creates a NetworkPolicy that allows UDP and TCP egress
// on port 53 to kube-dns in the kube-system namespace.
func CreateDNSAllowPolicy(ctx context.Context, client *Client, namespace string) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	dnsPort := intstr.FromInt32(53)
	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP

	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "allow-dns",
			Namespace: namespace,
			Labels: map[string]string{
				ManagedByLabel: ManagedByValue,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &udp, Port: &dnsPort},
						{Protocol: &tcp, Port: &dnsPort},
					},
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": "kube-system",
								},
							},
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"k8s-app": "kube-dns",
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := client.Clientset.NetworkingV1().NetworkPolicies(namespace).Create(ctx, policy, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("network policy %q already exists in namespace %q: %w", "allow-dns", namespace, err)
		}
		return fmt.Errorf("create allow-dns network policy in namespace %q: %w", namespace, err)
	}
	return nil
}
