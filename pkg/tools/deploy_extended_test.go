package tools

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// ---------- resourceReadiness tests ----------

func TestResourceReadiness_DeploymentReady(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec":   map[string]interface{}{"replicas": int64(2)},
			"status": map[string]interface{}{"readyReplicas": int64(2)},
		},
	}
	ready, msg := resourceReadiness(obj, "deployments")
	if !ready {
		t.Errorf("expected ready, got msg=%q", msg)
	}
}

func TestResourceReadiness_DeploymentNotReady(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec":   map[string]interface{}{"replicas": int64(3)},
			"status": map[string]interface{}{"readyReplicas": int64(1)},
		},
	}
	ready, msg := resourceReadiness(obj, "deployments")
	if ready {
		t.Error("expected not ready")
	}
	if msg != "1/3 replicas ready" {
		t.Errorf("msg = %q", msg)
	}
}

func TestResourceReadiness_DeploymentZeroReplicas(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec":   map[string]interface{}{},
			"status": map[string]interface{}{"readyReplicas": int64(1)},
		},
	}
	// When replicas is 0 (unset), defaults to 1.
	ready, _ := resourceReadiness(obj, "deployments")
	if !ready {
		t.Error("expected ready when readyReplicas(1) >= default(1)")
	}
}

func TestResourceReadiness_JobComplete(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{
						"type":   "Complete",
						"status": "True",
					},
				},
			},
		},
	}
	ready, _ := resourceReadiness(obj, "jobs")
	if !ready {
		t.Error("expected ready for completed job")
	}
}

func TestResourceReadiness_JobFailed(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{
						"type":   "Failed",
						"status": "True",
					},
				},
			},
		},
	}
	ready, msg := resourceReadiness(obj, "jobs")
	if ready {
		t.Error("expected not ready for failed job")
	}
	if msg != "job failed" {
		t.Errorf("msg = %q", msg)
	}
}

func TestResourceReadiness_JobInProgress(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": map[string]interface{}{},
		},
	}
	ready, msg := resourceReadiness(obj, "jobs")
	if ready {
		t.Error("expected not ready for in-progress job")
	}
	if msg != "job in progress" {
		t.Errorf("msg = %q", msg)
	}
}

func TestResourceReadiness_DefaultPresenceReady(t *testing.T) {
	obj := unstructured.Unstructured{Object: map[string]interface{}{}}

	for _, resource := range []string{"services", "configmaps", "secrets", "networkpolicies", "cronjobs"} {
		ready, _ := resourceReadiness(obj, resource)
		if !ready {
			t.Errorf("expected ready for %s (presence = ready)", resource)
		}
	}
}

// ---------- extractModuleDeps tests ----------

func TestExtractModuleDeps_JsrAndNpm(t *testing.T) {
	cm := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]interface{}{"name": "test-code"},
		"data": map[string]interface{}{
			"workflow.yaml": `
name: test-wf
contract:
  dependencies:
    jsr-dep:
      protocol: jsr
      host: "@std/path"
      version: "1.0.0"
    npm-dep:
      protocol: npm
      host: "lodash"
      version: "4.17.21"
    pg-dep:
      protocol: postgresql
      host: pg.example.com
`,
		},
	}
	manifests := []map[string]interface{}{cm}
	deps := extractModuleDeps(manifests)

	if len(deps) != 2 {
		t.Fatalf("expected 2 module deps, got %d: %v", len(deps), deps)
	}

	// Verify we only got jsr and npm.
	protocols := map[string]bool{}
	for _, d := range deps {
		protocols[d.Protocol] = true
	}
	if !protocols["jsr"] {
		t.Error("expected jsr dep")
	}
	if !protocols["npm"] {
		t.Error("expected npm dep")
	}
}

func TestExtractModuleDeps_NoDeps(t *testing.T) {
	cm := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]interface{}{"name": "test-code"},
		"data": map[string]interface{}{
			"workflow.yaml": `
name: test-wf
contract:
  dependencies:
    pg:
      protocol: postgresql
`,
		},
	}
	deps := extractModuleDeps([]map[string]interface{}{cm})
	if len(deps) != 0 {
		t.Errorf("expected 0 module deps, got %d", len(deps))
	}
}

func TestExtractModuleDeps_NoConfigMap(t *testing.T) {
	dep := map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]interface{}{"name": "test"},
	}
	deps := extractModuleDeps([]map[string]interface{}{dep})
	if deps != nil {
		t.Errorf("expected nil deps, got %v", deps)
	}
}

func TestExtractModuleDeps_DeduplicatesDeps(t *testing.T) {
	cm := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]interface{}{"name": "test-code"},
		"data": map[string]interface{}{
			"workflow.yaml": `
name: test-wf
contract:
  dependencies:
    dep1:
      protocol: jsr
      host: "@std/path"
      version: "1.0.0"
    dep2:
      protocol: jsr
      host: "@std/path"
      version: "1.0.0"
`,
		},
	}
	deps := extractModuleDeps([]map[string]interface{}{cm})
	if len(deps) != 1 {
		t.Errorf("expected 1 deduplicated dep, got %d", len(deps))
	}
}

func TestExtractModuleDeps_NoContract(t *testing.T) {
	cm := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]interface{}{"name": "test-code"},
		"data": map[string]interface{}{
			"workflow.yaml": `
name: test-wf
triggers:
  - type: http
`,
		},
	}
	deps := extractModuleDeps([]map[string]interface{}{cm})
	if deps != nil {
		t.Errorf("expected nil deps, got %v", deps)
	}
}

// ---------- handleWorkflowStatus tests ----------

func TestHandleWorkflowStatus_Basic(t *testing.T) {
	client := newDeployTestClient()
	ctx := context.Background()

	// Create a deployment in the managed namespace.
	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-wf",
			Namespace: "my-ns",
			Labels: map[string]string{
				k8s.ManagedByLabel:        k8s.ManagedByValue,
				"tentacular.io/release":   "my-wf",
				"app.kubernetes.io/version": "2.0",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"tentacular.io/release": "my-wf"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"tentacular.io/release": "my-wf"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "deno", Image: "denoland/deno:1.40"}},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas: 1,
			ReadyReplicas:     1,
		},
	}
	_, err := client.Clientset.AppsV1().Deployments("my-ns").Create(ctx, dep, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	result, err := handleWorkflowStatus(ctx, client, WorkflowStatusParams{
		Namespace: "my-ns",
		Name:      "my-wf",
	})
	if err != nil {
		t.Fatalf("handleWorkflowStatus: %v", err)
	}

	if result.Name != "my-wf" {
		t.Errorf("Name = %q", result.Name)
	}
	if result.Namespace != "my-ns" {
		t.Errorf("Namespace = %q", result.Namespace)
	}
	if result.Version != "2.0" {
		t.Errorf("Version = %q", result.Version)
	}
	if result.Replicas != 1 {
		t.Errorf("Replicas = %d", result.Replicas)
	}
	if result.Available != 1 {
		t.Errorf("Available = %d", result.Available)
	}
	if !result.Ready {
		t.Error("expected Ready=true")
	}
}

func TestHandleWorkflowStatus_UnmanagedNamespace(t *testing.T) {
	client := newDeployTestClient()
	ctx := context.Background()

	_, err := handleWorkflowStatus(ctx, client, WorkflowStatusParams{
		Namespace: "unmanaged-ns",
		Name:      "my-wf",
	})
	if err == nil {
		t.Error("expected error for unmanaged namespace")
	}
}

func TestHandleWorkflowStatus_WithDetail(t *testing.T) {
	client := newDeployTestClient()
	ctx := context.Background()

	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "detail-wf",
			Namespace: "my-ns",
			Labels: map[string]string{
				k8s.ManagedByLabel:      k8s.ManagedByValue,
				"tentacular.io/release": "detail-wf",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"tentacular.io/release": "detail-wf"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"tentacular.io/release": "detail-wf"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "deno", Image: "deno:latest"}},
				},
			},
		},
		Status: appsv1.DeploymentStatus{AvailableReplicas: 1},
	}
	_, _ = client.Clientset.AppsV1().Deployments("my-ns").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWorkflowStatus(ctx, client, WorkflowStatusParams{
		Namespace: "my-ns",
		Name:      "detail-wf",
		Detail:    true,
	})
	if err != nil {
		t.Fatalf("handleWorkflowStatus: %v", err)
	}

	// With detail=true, Pods and Events slices should be populated (even if empty).
	// The important thing is the code path was exercised.
	if result.Name != "detail-wf" {
		t.Errorf("Name = %q", result.Name)
	}
}
