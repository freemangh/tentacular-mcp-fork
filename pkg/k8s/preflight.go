package k8s

import (
	"context"
	"fmt"

	authzv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CheckResult holds the result of a single preflight check.
type CheckResult struct {
	Name        string `json:"name"`
	Warning     string `json:"warning,omitempty"`
	Remediation string `json:"remediation,omitempty"`
	Passed      bool   `json:"passed"`
}

// RunPreflightChecks runs a series of validation checks for the given namespace
// and returns the results.
func RunPreflightChecks(ctx context.Context, client *Client, namespace string) ([]CheckResult, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	results := make([]CheckResult, 0, 4)

	results = append(results, checkAPIReachability(ctx, client))
	results = append(results, checkNamespaceExists(ctx, client, namespace))
	results = append(results, checkRBAC(ctx, client, namespace)...)
	results = append(results, checkGVisor(ctx, client))

	return results, nil
}

func checkAPIReachability(_ context.Context, client *Client) CheckResult {
	_, err := client.Clientset.Discovery().ServerVersion()
	if err != nil {
		return CheckResult{
			Name:        "api-reachability",
			Passed:      false,
			Warning:     fmt.Sprintf("cannot reach API server: %v", err),
			Remediation: "Verify the cluster is running and the kubeconfig is correct.",
		}
	}
	return CheckResult{Name: "api-reachability", Passed: true}
}

func checkNamespaceExists(ctx context.Context, client *Client, namespace string) CheckResult {
	_, err := client.Clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return CheckResult{
			Name:        "namespace-exists",
			Passed:      false,
			Warning:     fmt.Sprintf("namespace %q not found: %v", namespace, err),
			Remediation: "Create the namespace with: kubectl create namespace " + namespace,
		}
	}
	return CheckResult{Name: "namespace-exists", Passed: true}
}

func checkRBAC(ctx context.Context, client *Client, namespace string) []CheckResult {
	checks := []struct {
		name     string
		group    string
		resource string
		verb     string
	}{
		{"rbac-create-deployments", "apps", "deployments", "create"},
		{"rbac-create-services", "", "services", "create"},
		{"rbac-create-configmaps", "", "configmaps", "create"},
		{"rbac-create-secrets", "", "secrets", "create"},
		{"rbac-create-cronjobs", "batch", "cronjobs", "create"},
		{"rbac-list-jobs", "batch", "jobs", "list"},
	}

	var results []CheckResult
	for _, c := range checks {
		sar := &authzv1.SelfSubjectAccessReview{
			Spec: authzv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authzv1.ResourceAttributes{
					Namespace: namespace,
					Verb:      c.verb,
					Group:     c.group,
					Resource:  c.resource,
				},
			},
		}

		review, err := client.Clientset.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
		if err != nil {
			results = append(results, CheckResult{
				Name:        c.name,
				Passed:      false,
				Warning:     fmt.Sprintf("failed to check RBAC for %s/%s: %v", c.group, c.resource, err),
				Remediation: "Ensure the service account has the tentacular-workflow role bound.",
			})
			continue
		}

		if !review.Status.Allowed {
			results = append(results, CheckResult{
				Name:        c.name,
				Passed:      false,
				Warning:     fmt.Sprintf("not allowed to %s %s/%s in namespace %s", c.verb, c.group, c.resource, namespace),
				Remediation: "Bind the tentacular-workflow role to the service account.",
			})
		} else {
			results = append(results, CheckResult{Name: c.name, Passed: true})
		}
	}
	return results
}

func checkGVisor(ctx context.Context, client *Client) CheckResult {
	rcs, err := client.Clientset.NodeV1().RuntimeClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return CheckResult{
			Name:    "gvisor-runtime",
			Passed:  true,
			Warning: "Could not list RuntimeClasses; gVisor check skipped.",
		}
	}

	for _, rc := range rcs.Items {
		if rc.Handler == "gvisor" || rc.Handler == "runsc" {
			return CheckResult{Name: "gvisor-runtime", Passed: true}
		}
	}

	return CheckResult{
		Name:        "gvisor-runtime",
		Passed:      true,
		Warning:     "gVisor RuntimeClass not found. Workloads will run without sandbox isolation.",
		Remediation: "Install gVisor and create a RuntimeClass with handler 'runsc' for enhanced isolation.",
	}
}
