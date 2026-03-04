package k8s_test

import (
	"context"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

func TestCreateDefaultDenyPolicy_PolicyTypes(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	if err := k8s.CreateDefaultDenyPolicy(ctx, client, "test-ns"); err != nil {
		t.Fatalf("CreateDefaultDenyPolicy: %v", err)
	}

	pol, err := cs.NetworkingV1().NetworkPolicies("test-ns").Get(ctx, "default-deny", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get NetworkPolicy: %v", err)
	}

	hasIngress, hasEgress := false, false
	for _, pt := range pol.Spec.PolicyTypes {
		if pt == networkingv1.PolicyTypeIngress {
			hasIngress = true
		}
		if pt == networkingv1.PolicyTypeEgress {
			hasEgress = true
		}
	}
	if !hasIngress {
		t.Error("default-deny policy missing Ingress policy type")
	}
	if !hasEgress {
		t.Error("default-deny policy missing Egress policy type")
	}
}

func TestCreateDefaultDenyPolicy_AlreadyExists(t *testing.T) {
	_, client := newFakeK8sClient()
	ctx := context.Background()

	_ = k8s.CreateDefaultDenyPolicy(ctx, client, "test-ns")
	err := k8s.CreateDefaultDenyPolicy(ctx, client, "test-ns")
	if err == nil {
		t.Error("expected error for duplicate default-deny policy, got nil")
	}
}

func TestCreateDNSAllowPolicy_EgressOnly(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	if err := k8s.CreateDNSAllowPolicy(ctx, client, "test-ns"); err != nil {
		t.Fatalf("CreateDNSAllowPolicy: %v", err)
	}

	pol, err := cs.NetworkingV1().NetworkPolicies("test-ns").Get(ctx, "allow-dns", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get NetworkPolicy: %v", err)
	}

	if len(pol.Spec.PolicyTypes) != 1 || pol.Spec.PolicyTypes[0] != networkingv1.PolicyTypeEgress {
		t.Errorf("expected allow-dns to have only Egress policy type, got %v", pol.Spec.PolicyTypes)
	}
}

func TestCreateDNSAllowPolicy_TargetsKubeDNS(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	_ = k8s.CreateDNSAllowPolicy(ctx, client, "test-ns")
	pol, err := cs.NetworkingV1().NetworkPolicies("test-ns").Get(ctx, "allow-dns", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get NetworkPolicy: %v", err)
	}

	if len(pol.Spec.Egress) == 0 {
		t.Fatal("expected at least one egress rule")
	}
	rule := pol.Spec.Egress[0]
	if len(rule.To) == 0 {
		t.Fatal("expected at least one To peer in egress rule")
	}
	peer := rule.To[0]
	if peer.PodSelector == nil {
		t.Fatal("expected pod selector in DNS egress rule")
	}
	if peer.PodSelector.MatchLabels["k8s-app"] != "kube-dns" {
		t.Errorf("expected kube-dns pod selector, got %v", peer.PodSelector.MatchLabels)
	}
	if peer.NamespaceSelector == nil {
		t.Fatal("expected namespace selector in DNS egress rule")
	}
	if peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != "kube-system" {
		t.Errorf("expected kube-system namespace selector, got %v", peer.NamespaceSelector.MatchLabels)
	}
}

func TestCreateDNSAllowPolicy_Port53(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	_ = k8s.CreateDNSAllowPolicy(ctx, client, "test-ns")
	pol, _ := cs.NetworkingV1().NetworkPolicies("test-ns").Get(ctx, "allow-dns", metav1.GetOptions{})

	if len(pol.Spec.Egress) == 0 || len(pol.Spec.Egress[0].Ports) == 0 {
		t.Fatal("expected ports in DNS egress rule")
	}

	hasPort53 := false
	for _, p := range pol.Spec.Egress[0].Ports {
		if p.Port != nil && p.Port.IntValue() == 53 {
			hasPort53 = true
		}
	}
	if !hasPort53 {
		t.Error("expected port 53 in DNS allow policy")
	}
}
