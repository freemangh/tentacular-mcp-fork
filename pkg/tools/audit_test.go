package tools

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

func newAuditTestClient() *k8s.Client {
	return &k8s.Client{
		Clientset: fake.NewSimpleClientset(),
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}
}

func TestAuditRbacWildcardDetected(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bad-role",
			Namespace: "my-ns",
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"*"},
				Resources: []string{"*"},
				Verbs:     []string{"*"},
			},
		},
	}
	_, err := client.Clientset.RbacV1().Roles("my-ns").Create(ctx, role, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := handleAuditRbac(ctx, client, AuditRbacParams{Namespace: "my-ns"})
	if err != nil {
		t.Fatalf("handleAuditRbac: %v", err)
	}

	if len(result.Findings) == 0 {
		t.Error("expected findings for wildcard role, got none")
	}

	hasHigh := false
	for _, f := range result.Findings {
		if f.Severity == "high" {
			hasHigh = true
			break
		}
	}
	if !hasHigh {
		t.Error("expected at least one high severity finding for wildcard")
	}
}

func TestAuditRbacCleanNamespace(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	result, err := handleAuditRbac(ctx, client, AuditRbacParams{Namespace: "clean-ns"})
	if err != nil {
		t.Fatalf("handleAuditRbac: %v", err)
	}
	if len(result.Findings) != 0 {
		t.Errorf("expected no findings for clean namespace, got %d: %v", len(result.Findings), result.Findings)
	}
}

func TestAuditRbacSensitiveResourceDetected(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "secret-reader", Namespace: "sec-ns"},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "list"},
			},
		},
	}
	client.Clientset.RbacV1().Roles("sec-ns").Create(ctx, role, metav1.CreateOptions{})

	result, err := handleAuditRbac(ctx, client, AuditRbacParams{Namespace: "sec-ns"})
	if err != nil {
		t.Fatalf("handleAuditRbac: %v", err)
	}

	hasMedium := false
	for _, f := range result.Findings {
		if f.Severity == "medium" {
			hasMedium = true
		}
	}
	if !hasMedium {
		t.Error("expected medium severity finding for secrets access")
	}
}

func TestAuditRbacClusterRoleBindingForNamespaceSA(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "crb-ns-sa"},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "cluster-admin",
		},
		Subjects: []rbacv1.Subject{
			{Kind: "ServiceAccount", Name: "mysa", Namespace: "target-ns"},
		},
	}
	client.Clientset.RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{})

	result, err := handleAuditRbac(ctx, client, AuditRbacParams{Namespace: "target-ns"})
	if err != nil {
		t.Fatalf("handleAuditRbac: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if f.Severity == "medium" {
			found = true
		}
	}
	if !found {
		t.Error("expected medium finding for ClusterRoleBinding targeting namespace SA")
	}
}

func TestAuditRbacRoleBindingToClusterRole(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "rb-clusterrole", Namespace: "rb-ns"},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "view",
		},
		Subjects: []rbacv1.Subject{
			{Kind: "ServiceAccount", Name: "viewer", Namespace: "rb-ns"},
		},
	}
	client.Clientset.RbacV1().RoleBindings("rb-ns").Create(ctx, rb, metav1.CreateOptions{})

	result, err := handleAuditRbac(ctx, client, AuditRbacParams{Namespace: "rb-ns"})
	if err != nil {
		t.Fatalf("handleAuditRbac: %v", err)
	}

	hasLow := false
	for _, f := range result.Findings {
		if f.Severity == "low" {
			hasLow = true
		}
	}
	if !hasLow {
		t.Error("expected low severity finding for RoleBinding referencing ClusterRole")
	}
}

func TestAuditRbacEscalationBindVerb(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "escalator", Namespace: "esc-ns"},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"rbac.authorization.k8s.io"},
				Resources: []string{"roles"},
				Verbs:     []string{"bind"},
			},
		},
	}
	client.Clientset.RbacV1().Roles("esc-ns").Create(ctx, role, metav1.CreateOptions{})

	result, err := handleAuditRbac(ctx, client, AuditRbacParams{Namespace: "esc-ns"})
	if err != nil {
		t.Fatalf("handleAuditRbac: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if f.Severity == "high" && strings.Contains(f.Reason, "bind") {
			found = true
			if f.Remediation == "" {
				t.Error("expected remediation text for bind verb finding")
			}
		}
	}
	if !found {
		t.Error("expected high severity finding for bind verb")
	}
}

func TestAuditRbacEscalationEscalateVerb(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "self-escalator", Namespace: "esc2-ns"},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"rbac.authorization.k8s.io"},
				Resources: []string{"clusterroles"},
				Verbs:     []string{"escalate"},
			},
		},
	}
	client.Clientset.RbacV1().Roles("esc2-ns").Create(ctx, role, metav1.CreateOptions{})

	result, err := handleAuditRbac(ctx, client, AuditRbacParams{Namespace: "esc2-ns"})
	if err != nil {
		t.Fatalf("handleAuditRbac: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if f.Severity == "high" && strings.Contains(f.Reason, "escalate") {
			found = true
		}
	}
	if !found {
		t.Error("expected high severity finding for escalate verb")
	}
}

func TestAuditRbacEscalationImpersonateVerb(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "impersonator", Namespace: "imp-ns"},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"users"},
				Verbs:     []string{"impersonate"},
			},
		},
	}
	client.Clientset.RbacV1().Roles("imp-ns").Create(ctx, role, metav1.CreateOptions{})

	result, err := handleAuditRbac(ctx, client, AuditRbacParams{Namespace: "imp-ns"})
	if err != nil {
		t.Fatalf("handleAuditRbac: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if f.Severity == "high" && strings.Contains(f.Reason, "impersonate") {
			found = true
		}
	}
	if !found {
		t.Error("expected high severity finding for impersonate verb")
	}
}

func TestAuditRbacRemediationOnWildcard(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "wildcard-role", Namespace: "rem-ns"},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"*"},
			},
		},
	}
	client.Clientset.RbacV1().Roles("rem-ns").Create(ctx, role, metav1.CreateOptions{})

	result, err := handleAuditRbac(ctx, client, AuditRbacParams{Namespace: "rem-ns"})
	if err != nil {
		t.Fatalf("handleAuditRbac: %v", err)
	}

	for _, f := range result.Findings {
		if f.Remediation == "" {
			t.Errorf("finding %q missing remediation text", f.Reason)
		}
	}
}

func TestAuditRbacRemediationOnClusterRoleBinding(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "crb-rem"},
		RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: "edit"},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "mysa", Namespace: "rem2-ns"}},
	}
	client.Clientset.RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{})

	result, err := handleAuditRbac(ctx, client, AuditRbacParams{Namespace: "rem2-ns"})
	if err != nil {
		t.Fatalf("handleAuditRbac: %v", err)
	}

	for _, f := range result.Findings {
		if f.Remediation == "" {
			t.Errorf("finding %q missing remediation text", f.Reason)
		}
	}
}

func TestAuditNetpolNoDefaultDeny(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	result, err := handleAuditNetpol(ctx, client, AuditNetpolParams{Namespace: "empty-ns"})
	if err != nil {
		t.Fatalf("handleAuditNetpol: %v", err)
	}
	if result.DefaultDeny {
		t.Error("expected DefaultDeny=false for empty namespace")
	}
	if len(result.Findings) == 0 {
		t.Error("expected findings when no policies exist")
	}
}

func TestAuditNetpolWithDefaultDeny(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-deny",
			Namespace: "secure-ns",
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
		},
	}
	_, err := client.Clientset.NetworkingV1().NetworkPolicies("secure-ns").Create(ctx, policy, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := handleAuditNetpol(ctx, client, AuditNetpolParams{Namespace: "secure-ns"})
	if err != nil {
		t.Fatalf("handleAuditNetpol: %v", err)
	}
	if !result.DefaultDeny {
		t.Error("expected DefaultDeny=true for namespace with default-deny policy")
	}
}

func TestAuditNetpolNoEgressFinding(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	// Ingress-only policy - no egress coverage
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "ingress-only", Namespace: "ing-ns"},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
		},
	}
	client.Clientset.NetworkingV1().NetworkPolicies("ing-ns").Create(ctx, policy, metav1.CreateOptions{})

	result, err := handleAuditNetpol(ctx, client, AuditNetpolParams{Namespace: "ing-ns"})
	if err != nil {
		t.Fatalf("handleAuditNetpol: %v", err)
	}

	hasMedium := false
	for _, f := range result.Findings {
		if f.Severity == "medium" {
			hasMedium = true
		}
	}
	if !hasMedium {
		t.Error("expected medium finding for missing egress NetworkPolicy")
	}
}

func TestAuditNetpolPoliciesListed(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	client.Clientset.NetworkingV1().NetworkPolicies("list-ns").Create(ctx, &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "my-policy", Namespace: "list-ns"},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
		},
	}, metav1.CreateOptions{})

	result, err := handleAuditNetpol(ctx, client, AuditNetpolParams{Namespace: "list-ns"})
	if err != nil {
		t.Fatalf("handleAuditNetpol: %v", err)
	}
	if len(result.Policies) != 1 {
		t.Errorf("expected 1 policy in list, got %d", len(result.Policies))
	}
	if result.Policies[0].Name != "my-policy" {
		t.Errorf("expected policy name=my-policy, got %q", result.Policies[0].Name)
	}
}

func TestAuditNetpolBroadIngressAllowAll(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	// Policy with an ingress rule that has an empty peer (from: [{}]) — allows all traffic
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "allow-all-ingress", Namespace: "broad-ns"},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{{}},
				},
			},
		},
	}
	client.Clientset.NetworkingV1().NetworkPolicies("broad-ns").Create(ctx, policy, metav1.CreateOptions{})

	result, err := handleAuditNetpol(ctx, client, AuditNetpolParams{Namespace: "broad-ns"})
	if err != nil {
		t.Fatalf("handleAuditNetpol: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if f.Severity == "high" && strings.Contains(f.Message, "allows traffic from all sources") {
			found = true
			if f.Remediation == "" {
				t.Error("expected remediation text for broad ingress finding")
			}
		}
	}
	if !found {
		t.Error("expected high severity finding for allow-all ingress rule")
	}
}

func TestAuditNetpolBroadEgressAllowAll(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "allow-all-egress", Namespace: "broad-eg-ns"},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{
					To: []networkingv1.NetworkPolicyPeer{{}},
				},
			},
		},
	}
	client.Clientset.NetworkingV1().NetworkPolicies("broad-eg-ns").Create(ctx, policy, metav1.CreateOptions{})

	result, err := handleAuditNetpol(ctx, client, AuditNetpolParams{Namespace: "broad-eg-ns"})
	if err != nil {
		t.Fatalf("handleAuditNetpol: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if f.Severity == "high" && strings.Contains(f.Message, "allows traffic to all destinations") {
			found = true
		}
	}
	if !found {
		t.Error("expected high severity finding for allow-all egress rule")
	}
}

func TestAuditNetpolCrossNamespaceIngress(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	// Policy with empty namespaceSelector (matches all namespaces)
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "cross-ns", Namespace: "crossns-ns"},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{},
						},
					},
				},
			},
		},
	}
	client.Clientset.NetworkingV1().NetworkPolicies("crossns-ns").Create(ctx, policy, metav1.CreateOptions{})

	result, err := handleAuditNetpol(ctx, client, AuditNetpolParams{Namespace: "crossns-ns"})
	if err != nil {
		t.Fatalf("handleAuditNetpol: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if f.Severity == "medium" && strings.Contains(f.Message, "all namespaces") {
			found = true
			if f.Remediation == "" {
				t.Error("expected remediation text for cross-namespace finding")
			}
		}
	}
	if !found {
		t.Error("expected medium severity finding for empty namespaceSelector")
	}
}

func TestAuditNetpolRemediationOnMissingDefaultDeny(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	result, err := handleAuditNetpol(ctx, client, AuditNetpolParams{Namespace: "no-policy-ns"})
	if err != nil {
		t.Fatalf("handleAuditNetpol: %v", err)
	}

	for _, f := range result.Findings {
		if f.Remediation == "" {
			t.Errorf("finding %q missing remediation text", f.Message)
		}
	}
}

func TestAuditPsaCompliant(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "compliant-ns",
			Labels: map[string]string{
				"pod-security.kubernetes.io/enforce": "restricted",
				"pod-security.kubernetes.io/audit":   "restricted",
				"pod-security.kubernetes.io/warn":    "restricted",
				k8s.ManagedByLabel:                   k8s.ManagedByValue,
			},
		},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := handleAuditPsa(ctx, client, AuditPsaParams{Namespace: "compliant-ns"})
	if err != nil {
		t.Fatalf("handleAuditPsa: %v", err)
	}
	if !result.Compliant {
		t.Errorf("expected Compliant=true, findings: %v", result.Findings)
	}
	if result.Enforce != "restricted" {
		t.Errorf("enforce: got %q, want restricted", result.Enforce)
	}
}

func TestAuditPsaMissingLabels(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "insecure-ns"},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := handleAuditPsa(ctx, client, AuditPsaParams{Namespace: "insecure-ns"})
	if err != nil {
		t.Fatalf("handleAuditPsa: %v", err)
	}
	if result.Compliant {
		t.Error("expected Compliant=false for namespace with no PSA labels")
	}
	if len(result.Findings) == 0 {
		t.Error("expected findings for missing PSA labels")
	}
}

func TestAuditPsaNonRestrictedLevel(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "baseline-ns",
			Labels: map[string]string{"pod-security.kubernetes.io/enforce": "baseline"},
		},
	}
	client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	result, err := handleAuditPsa(ctx, client, AuditPsaParams{Namespace: "baseline-ns"})
	if err != nil {
		t.Fatalf("handleAuditPsa: %v", err)
	}
	if result.Compliant {
		t.Error("expected Compliant=false for non-restricted enforce level")
	}
	if result.Enforce != "baseline" {
		t.Errorf("expected enforce=baseline, got %q", result.Enforce)
	}

	hasMedium := false
	for _, f := range result.Findings {
		if f.Severity == "medium" {
			hasMedium = true
		}
	}
	if !hasMedium {
		t.Error("expected medium finding for non-restricted PSA level")
	}
}

func TestAuditPsaPrivilegedIsHighSeverity(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "priv-ns",
			Labels: map[string]string{"pod-security.kubernetes.io/enforce": "privileged"},
		},
	}
	client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	result, err := handleAuditPsa(ctx, client, AuditPsaParams{Namespace: "priv-ns"})
	if err != nil {
		t.Fatalf("handleAuditPsa: %v", err)
	}
	if result.Compliant {
		t.Error("expected Compliant=false for privileged enforce level")
	}

	hasHigh := false
	for _, f := range result.Findings {
		if f.Severity == "high" && strings.Contains(f.Message, "privileged") {
			hasHigh = true
		}
	}
	if !hasHigh {
		t.Error("expected high severity finding for privileged enforce level")
	}
}

func TestAuditPsaAuditLevelMismatch(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mismatch-audit-ns",
			Labels: map[string]string{
				"pod-security.kubernetes.io/enforce": "restricted",
				"pod-security.kubernetes.io/audit":   "baseline",
				"pod-security.kubernetes.io/warn":    "restricted",
			},
		},
	}
	client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	result, err := handleAuditPsa(ctx, client, AuditPsaParams{Namespace: "mismatch-audit-ns"})
	if err != nil {
		t.Fatalf("handleAuditPsa: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if f.Severity == "medium" && strings.Contains(f.Message, "audit level") {
			found = true
			if f.Remediation == "" {
				t.Error("expected remediation text for audit mismatch finding")
			}
		}
	}
	if !found {
		t.Error("expected medium finding for audit level weaker than enforce")
	}
}

func TestAuditPsaWarnLevelMismatch(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mismatch-warn-ns",
			Labels: map[string]string{
				"pod-security.kubernetes.io/enforce": "restricted",
				"pod-security.kubernetes.io/audit":   "restricted",
				"pod-security.kubernetes.io/warn":    "privileged",
			},
		},
	}
	client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	result, err := handleAuditPsa(ctx, client, AuditPsaParams{Namespace: "mismatch-warn-ns"})
	if err != nil {
		t.Fatalf("handleAuditPsa: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if f.Severity == "medium" && strings.Contains(f.Message, "warn level") {
			found = true
		}
	}
	if !found {
		t.Error("expected medium finding for warn level weaker than enforce")
	}
}

func TestAuditPsaNoMismatchWhenAllRestricted(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "all-restricted-ns",
			Labels: map[string]string{
				"pod-security.kubernetes.io/enforce": "restricted",
				"pod-security.kubernetes.io/audit":   "restricted",
				"pod-security.kubernetes.io/warn":    "restricted",
			},
		},
	}
	client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	result, err := handleAuditPsa(ctx, client, AuditPsaParams{Namespace: "all-restricted-ns"})
	if err != nil {
		t.Fatalf("handleAuditPsa: %v", err)
	}
	if !result.Compliant {
		t.Errorf("expected Compliant=true, findings: %v", result.Findings)
	}
	if len(result.Findings) != 0 {
		t.Errorf("expected no findings for fully compliant namespace, got %d: %v", len(result.Findings), result.Findings)
	}
}

func TestAuditPsaRemediationPresent(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "rem-psa-ns"},
	}
	client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	result, err := handleAuditPsa(ctx, client, AuditPsaParams{Namespace: "rem-psa-ns"})
	if err != nil {
		t.Fatalf("handleAuditPsa: %v", err)
	}

	for _, f := range result.Findings {
		if f.Remediation == "" {
			t.Errorf("finding %q missing remediation text", f.Message)
		}
	}
}

func TestAuditPsaNonExistentNamespace(t *testing.T) {
	client := newAuditTestClient()
	ctx := context.Background()

	_, err := handleAuditPsa(ctx, client, AuditPsaParams{Namespace: "ghost-ns"})
	if err == nil {
		t.Fatal("expected error for non-existent namespace, got nil")
	}
}
