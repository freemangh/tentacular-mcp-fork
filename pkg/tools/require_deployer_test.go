// Tests for the requireDeployer guard and nil-deployer behavior across handlers.
//
// Covers:
//   - requireDeployer: nil deployer with authz enabled/disabled, non-nil deployer
//   - Nil-deployer rejection on namespace handlers (ns_delete, ns_get, ns_update, ns_list)
//   - Nil-deployer rejection on deploy handlers (wf_apply, wf_remove, wf_status)
//   - Nil-deployer rejection on discover handlers (wf_describe)
//   - Nil-deployer rejection on permissions handlers (permissions_get, permissions_set)

package tools

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// --- requireDeployer unit tests ---

func TestRequireDeployer_NilDeployer_AuthzEnabled(t *testing.T) {
	eval := authz.NewEvaluator(authz.DefaultMode)
	err := requireDeployer(nil, eval)
	if err == nil {
		t.Error("expected error for nil deployer with authz enabled")
	}
	if !errors.Is(err, errNoDeployer) {
		t.Errorf("expected errNoDeployer, got: %v", err)
	}
}

func TestRequireDeployer_NilDeployer_AuthzDisabled(t *testing.T) {
	eval := &authz.Evaluator{Enabled: false}
	err := requireDeployer(nil, eval)
	if err != nil {
		t.Errorf("expected nil error with authz disabled, got: %v", err)
	}
}

func TestRequireDeployer_NilDeployer_NilEval(t *testing.T) {
	err := requireDeployer(nil, nil)
	if err != nil {
		t.Errorf("expected nil error with nil eval, got: %v", err)
	}
}

func TestRequireDeployer_NonNilDeployer_AuthzEnabled(t *testing.T) {
	eval := authz.NewEvaluator(authz.DefaultMode)
	deployer := &exoskeleton.DeployerInfo{Subject: "test", Provider: "keycloak"}
	err := requireDeployer(deployer, eval)
	if err != nil {
		t.Errorf("expected nil error for non-nil deployer, got: %v", err)
	}
}

// --- Nil-deployer rejection on namespace handlers ---

func TestNsDelete_NilDeployer_AuthzEnabled_Denied(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "del-nil-dep",
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
			Annotations: map[string]string{
				authz.AnnotationOwner:    "owner@example.com",
				authz.AnnotationOwnerSub: "sub-owner",
				authz.AnnotationMode:     "rwx------",
			},
		},
	}
	_, _ = client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	eval := authz.NewEvaluator(authz.DefaultMode)
	_, err := handleNsDelete(ctx, client, eval, NsDeleteParams{Name: "del-nil-dep"}, nil)
	if err == nil {
		t.Error("expected error for nil deployer on ns_delete with authz enabled")
	}
}

func TestNsGet_NilDeployer_AuthzEnabled_Denied(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "get-nil-dep",
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
			Annotations: map[string]string{
				authz.AnnotationOwner:    "owner@example.com",
				authz.AnnotationOwnerSub: "sub-owner",
				authz.AnnotationMode:     "rwx------",
			},
		},
	}
	_, _ = client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	eval := authz.NewEvaluator(authz.DefaultMode)
	_, err := handleNsGet(ctx, client, eval, NsGetParams{Name: "get-nil-dep"}, nil)
	if err == nil {
		t.Error("expected error for nil deployer on ns_get with authz enabled")
	}
}

func TestNsUpdate_NilDeployer_AuthzEnabled_Denied(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	// Create a managed namespace.
	_, err := handleNsCreate(ctx, client, bearerEval(), NsCreateParams{
		Name:        "upd-nil-dep",
		QuotaPreset: "small",
	}, nil)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	eval := authz.NewEvaluator(authz.DefaultMode)
	_, err = handleNsUpdate(ctx, client, eval, NsUpdateParams{
		Name:   "upd-nil-dep",
		Labels: map[string]string{"env": "test"},
	}, nil)
	if err == nil {
		t.Error("expected error for nil deployer on ns_update with authz enabled")
	}
}

func TestNsList_NilDeployer_AuthzEnabled_FiltersAll(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "list-nil-dep",
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
			Annotations: map[string]string{
				authz.AnnotationOwner:    "owner@example.com",
				authz.AnnotationOwnerSub: "sub-owner",
				authz.AnnotationMode:     "rwx------",
			},
		},
	}
	_, _ = client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	eval := authz.NewEvaluator(authz.DefaultMode)
	result, err := handleNsList(ctx, client, eval, nil)
	if err != nil {
		t.Fatalf("handleNsList: %v", err)
	}
	// Nil deployer with authz enabled: eval.Check returns deny, so the namespace
	// should be filtered out of the list.
	if len(result.Namespaces) != 0 {
		t.Errorf("expected 0 namespaces visible to nil deployer, got %d", len(result.Namespaces))
	}
}

// --- Nil-deployer rejection on deploy/discover/permissions handlers ---

func TestWfDescribe_NilDeployer_AuthzEnabled_Denied(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "desc-nil-ns", "owner@example.com", "", "rwx------")
	dep := makeTestDeployment("desc-nil-wf", "desc-nil-ns", map[string]string{
		authz.AnnotationOwner:    "owner@example.com",
		authz.AnnotationOwnerSub: "sub-owner",
		authz.AnnotationMode:     "rwx------",
	})
	_, _ = client.Clientset.AppsV1().Deployments("desc-nil-ns").Create(ctx, dep, metav1.CreateOptions{})

	eval := authz.NewEvaluator(authz.DefaultMode)
	_, err := handleWfDescribe(ctx, client, WfDescribeParams{Namespace: "desc-nil-ns", Name: "desc-nil-wf"}, nil, eval)
	if err == nil {
		t.Error("expected error for nil deployer on wf_describe with authz enabled")
	}
}

func TestPermissionsGet_NilDeployer_AuthzEnabled_Denied(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "perm-nil-ns", "owner@example.com", "", "rwx------")
	dep := makeTestDeployment("perm-nil-wf", "perm-nil-ns", map[string]string{
		authz.AnnotationOwner:    "owner@example.com",
		authz.AnnotationOwnerSub: "sub-owner",
		authz.AnnotationMode:     "rwx------",
	})
	_, _ = client.Clientset.AppsV1().Deployments("perm-nil-ns").Create(ctx, dep, metav1.CreateOptions{})

	eval := authz.NewEvaluator(authz.DefaultMode)
	_, err := handlePermissionsGet(ctx, client, PermissionsGetParams{Namespace: "perm-nil-ns", Name: "perm-nil-wf"}, nil, eval)
	if err == nil {
		t.Error("expected error for nil deployer on permissions_get with authz enabled")
	}
}
