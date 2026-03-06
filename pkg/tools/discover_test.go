// Tests for discover.go helper functions and handler paths not covered by workflow_meta_test.go.
//
// workflow_meta_test.go covers the basic wf_list/wf_describe CRUD paths.
// This file adds coverage for:
//   - containsTag: CSV tag matching helper
//   - derefInt32: nil-safe pointer dereference
//   - wrapListError / wrapGetError: error message formatting
//   - deploymentToListEntry: conversion with full metadata and nil annotations
//   - handleWfList: owner filter, tag filter, tag filter with nil annotations
//   - handleWfDescribe: ConfigMap enrichment (nodes, triggers, version), image extraction

package tools

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// --- containsTag ---

func TestContainsTagFound(t *testing.T) {
	if !containsTag("prod,staging,dev", "staging") {
		t.Error("expected containsTag to find 'staging'")
	}
}

func TestContainsTagNotFound(t *testing.T) {
	if containsTag("prod,staging", "dev") {
		t.Error("expected containsTag not to find 'dev'")
	}
}

func TestContainsTagEmpty(t *testing.T) {
	if containsTag("", "prod") {
		t.Error("expected containsTag to return false for empty string")
	}
}

func TestContainsTagSingle(t *testing.T) {
	if !containsTag("prod", "prod") {
		t.Error("expected containsTag to find single tag")
	}
}

func TestContainsTagTrimSpaces(t *testing.T) {
	// containsTag does TrimSpace on each element
	if !containsTag("prod, staging , dev", "staging") {
		t.Error("expected containsTag to trim spaces and find 'staging'")
	}
}

// --- derefInt32 ---

func TestDerefInt32Nil(t *testing.T) {
	if derefInt32(nil) != 0 {
		t.Error("expected 0 for nil pointer")
	}
}

func TestDerefInt32Value(t *testing.T) {
	v := int32(3)
	if derefInt32(&v) != 3 {
		t.Errorf("expected 3, got %d", derefInt32(&v))
	}
}

// --- wrapListError / wrapGetError ---

func TestWrapListErrorWithNamespace(t *testing.T) {
	err := wrapListError("deployments", "my-ns", errFake)
	if !strings.Contains(err.Error(), `namespace "my-ns"`) {
		t.Errorf("expected namespace in error, got: %v", err)
	}
}

func TestWrapListErrorAllNamespaces(t *testing.T) {
	err := wrapListError("deployments", "", errFake)
	if !strings.Contains(err.Error(), "all namespaces") {
		t.Errorf("expected 'all namespaces' in error, got: %v", err)
	}
}

func TestWrapGetError(t *testing.T) {
	err := wrapGetError("deployment", "my-dep", "my-ns", errFake)
	if !strings.Contains(err.Error(), `"my-dep"`) || !strings.Contains(err.Error(), `"my-ns"`) {
		t.Errorf("expected name and namespace in error, got: %v", err)
	}
}

var errFake = &fakeError{}

type fakeError struct{}

func (e *fakeError) Error() string { return "fake error" }

// --- deploymentToListEntry ---

func TestDeploymentToListEntryAllFields(t *testing.T) {
	replicas := int32(2)
	dep := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-wf",
			Namespace: "prod-ns",
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
				k8s.VersionLabel:   "1.2.0",
			},
			Annotations: map[string]string{
				"tentacular.dev/owner":       "alice",
				"tentacular.dev/team":        "platform",
				"tentacular.dev/environment": "prod",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 2,
		},
	}

	entry := deploymentToListEntry(dep)

	if entry.Name != "my-wf" {
		t.Errorf("Name: got %q, want %q", entry.Name, "my-wf")
	}
	if entry.Namespace != "prod-ns" {
		t.Errorf("Namespace: got %q, want %q", entry.Namespace, "prod-ns")
	}
	if entry.Version != "1.2.0" {
		t.Errorf("Version: got %q, want %q", entry.Version, "1.2.0")
	}
	if entry.Owner != "alice" {
		t.Errorf("Owner: got %q, want %q", entry.Owner, "alice")
	}
	if entry.Team != "platform" {
		t.Errorf("Team: got %q, want %q", entry.Team, "platform")
	}
	if entry.Environment != "prod" {
		t.Errorf("Environment: got %q, want %q", entry.Environment, "prod")
	}
	if !entry.Ready {
		t.Error("expected Ready=true when ReadyReplicas >= 1")
	}
	if entry.Age == "" {
		t.Error("expected Age to be set")
	}
}

func TestDeploymentToListEntryNilAnnotations(t *testing.T) {
	dep := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bare-dep",
			Namespace: "default",
		},
	}

	entry := deploymentToListEntry(dep)
	if entry.Owner != "" {
		t.Errorf("expected empty Owner for nil annotations, got %q", entry.Owner)
	}
	if entry.Ready {
		t.Error("expected Ready=false when ReadyReplicas=0")
	}
}

// --- handleWfList (owner and tag filters — not covered by workflow_meta_test.go) ---

func TestWfListFilterByOwner(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	for _, dep := range []struct {
		name  string
		owner string
	}{
		{"wf-alice", "alice"},
		{"wf-bob", "bob"},
	} {
		d := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dep.name,
				Namespace: "ns-a",
				Labels:    map[string]string{k8s.ManagedByLabel: k8s.ManagedByValue},
				Annotations: map[string]string{
					"tentacular.dev/owner": dep.owner,
				},
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": dep.name}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": dep.name}},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "img:v1"}}},
				},
			},
		}
		_, _ = client.Clientset.AppsV1().Deployments("ns-a").Create(ctx, d, metav1.CreateOptions{})
	}

	result, err := handleWfList(ctx, client, WfListParams{Namespace: "ns-a", Owner: "alice"})
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}
	if len(result.Workflows) != 1 {
		t.Fatalf("expected 1 workflow for alice, got %d", len(result.Workflows))
	}
	if result.Workflows[0].Name != "wf-alice" {
		t.Errorf("expected wf-alice, got %q", result.Workflows[0].Name)
	}
}

func TestWfListFilterByTag(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tagged-wf",
			Namespace: "ns-a",
			Labels:    map[string]string{k8s.ManagedByLabel: k8s.ManagedByValue},
			Annotations: map[string]string{
				"tentacular.dev/tags": "ml,gpu",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "tagged-wf"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "tagged-wf"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "img:v1"}}},
			},
		},
	}
	_, _ = client.Clientset.AppsV1().Deployments("ns-a").Create(ctx, dep, metav1.CreateOptions{})

	// Filter by matching tag
	result, err := handleWfList(ctx, client, WfListParams{Namespace: "ns-a", Tag: "ml"})
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}
	if len(result.Workflows) != 1 {
		t.Fatalf("expected 1 workflow for tag 'ml', got %d", len(result.Workflows))
	}

	// Filter by non-matching tag
	result, err = handleWfList(ctx, client, WfListParams{Namespace: "ns-a", Tag: "cpu"})
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}
	if len(result.Workflows) != 0 {
		t.Errorf("expected 0 workflows for tag 'cpu', got %d", len(result.Workflows))
	}
}

func TestWfListFilterByTagNoAnnotations(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-ann-wf",
			Namespace: "ns-a",
			Labels:    map[string]string{k8s.ManagedByLabel: k8s.ManagedByValue},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "no-ann-wf"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "no-ann-wf"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "img:v1"}}},
			},
		},
	}
	_, _ = client.Clientset.AppsV1().Deployments("ns-a").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfList(ctx, client, WfListParams{Namespace: "ns-a", Tag: "ml"})
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}
	if len(result.Workflows) != 0 {
		t.Errorf("expected 0 workflows when no annotations, got %d", len(result.Workflows))
	}
}

// --- handleWfDescribe (ConfigMap enrichment — not covered by workflow_meta_test.go) ---

func TestWfDescribeWithConfigMapEnrichment(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "enriched-wf",
			Namespace: "ns-a",
			Labels:    map[string]string{k8s.ManagedByLabel: k8s.ManagedByValue},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "enriched-wf"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "enriched-wf"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "engine", Image: "img:v1"}}},
			},
		},
	}
	_, _ = client.Clientset.AppsV1().Deployments("ns-a").Create(ctx, dep, metav1.CreateOptions{})

	// Create the code ConfigMap with workflow.yaml
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "enriched-wf-code",
			Namespace: "ns-a",
		},
		Data: map[string]string{
			"workflow.yaml": `name: enriched-wf
version: "3.0.0"
triggers:
  - type: cron
    schedule: "*/5 * * * *"
  - type: webhook
nodes:
  fetch:
    path: ./fetch.ts
  process:
    path: ./process.ts
`,
		},
	}
	_, _ = client.Clientset.CoreV1().ConfigMaps("ns-a").Create(ctx, cm, metav1.CreateOptions{})

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{Namespace: "ns-a", Name: "enriched-wf"})
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}

	// Version should be enriched from ConfigMap
	if result.Version != "3.0.0" {
		t.Errorf("Version: got %q, want %q (enriched from ConfigMap)", result.Version, "3.0.0")
	}
	// Nodes should be populated
	if len(result.Nodes) != 2 {
		t.Errorf("Nodes: got %d, want 2", len(result.Nodes))
	}
	// Triggers should be populated
	if len(result.Triggers) != 2 {
		t.Errorf("Triggers: got %d, want 2", len(result.Triggers))
	}
}

func TestWfDescribeImageFromContainerSpec(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "img-wf",
			Namespace: "ns-a",
			Labels:    map[string]string{k8s.ManagedByLabel: k8s.ManagedByValue},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "img-wf"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "img-wf"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "engine", Image: "registry.io/app:v5"}}},
			},
		},
	}
	_, _ = client.Clientset.AppsV1().Deployments("ns-a").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{Namespace: "ns-a", Name: "img-wf"})
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}
	if result.Image != "registry.io/app:v5" {
		t.Errorf("Image: got %q, want %q", result.Image, "registry.io/app:v5")
	}
}
