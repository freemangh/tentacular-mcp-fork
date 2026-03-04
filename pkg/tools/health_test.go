package tools

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

func newHealthTestClient() *k8s.Client {
	return &k8s.Client{
		Clientset: fake.NewClientset(),
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}
}

func TestHealthNodesEmpty(t *testing.T) {
	client := newHealthTestClient()
	ctx := context.Background()

	result, err := handleHealthNodes(ctx, client)
	if err != nil {
		t.Fatalf("handleHealthNodes: %v", err)
	}
	if result.Nodes == nil {
		t.Error("expected nodes slice (may be empty), got nil")
	}
}

func TestHealthNodesWithNode(t *testing.T) {
	client := newHealthTestClient()
	ctx := context.Background()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3500m"),
				corev1.ResourceMemory: resource.MustParse("7Gi"),
			},
			NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.28.0"},
		},
	}
	_, err := client.Clientset.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := handleHealthNodes(ctx, client)
	if err != nil {
		t.Fatalf("handleHealthNodes: %v", err)
	}
	if len(result.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result.Nodes))
	}
	if !result.Nodes[0].Ready {
		t.Error("expected node to be ready")
	}
	if result.Nodes[0].KubeletVersion != "v1.28.0" {
		t.Errorf("kubelet version: got %q, want v1.28.0", result.Nodes[0].KubeletVersion)
	}
}

func TestHealthNodesNotReady(t *testing.T) {
	client := newHealthTestClient()
	ctx := context.Background()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-node"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
			},
		},
	}
	_, _ = client.Clientset.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})

	result, err := handleHealthNodes(ctx, client)
	if err != nil {
		t.Fatalf("handleHealthNodes: %v", err)
	}
	if len(result.Nodes) == 0 {
		t.Fatal("expected at least one node")
	}
	if result.Nodes[0].Ready {
		t.Error("expected Ready=false for node with NodeReady=False condition")
	}
}

func TestHealthNodesCapacityAndAllocatable(t *testing.T) {
	client := newHealthTestClient()
	ctx := context.Background()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "cap-node"},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("8"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("7500m"),
				corev1.ResourceMemory: resource.MustParse("14Gi"),
			},
		},
	}
	_, _ = client.Clientset.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})

	result, err := handleHealthNodes(ctx, client)
	if err != nil {
		t.Fatalf("handleHealthNodes: %v", err)
	}
	if len(result.Nodes) == 0 {
		t.Fatal("expected at least one node")
	}
	n := result.Nodes[0]
	if n.CPUCapacity == "" {
		t.Error("expected CPUCapacity to be set")
	}
	if n.MemCapacity == "" {
		t.Error("expected MemCapacity to be set")
	}
	if n.CPUAllocatable == "" {
		t.Error("expected CPUAllocatable to be set")
	}
	if n.MemAllocatable == "" {
		t.Error("expected MemAllocatable to be set")
	}
}

func TestHealthNsUsageNoQuota(t *testing.T) {
	client := newHealthTestClient()
	ctx := context.Background()

	result, err := handleHealthNsUsage(ctx, client, HealthNsUsageParams{Namespace: "empty-ns"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CPULimit != "unlimited" {
		t.Errorf("expected CPULimit=unlimited, got %q", result.CPULimit)
	}
	if result.MemLimit != "unlimited" {
		t.Errorf("expected MemLimit=unlimited, got %q", result.MemLimit)
	}
	if result.PodLimit != -1 {
		t.Errorf("expected PodLimit=-1 for unlimited, got %d", result.PodLimit)
	}
}

func TestHealthNsUsageWithQuota(t *testing.T) {
	client := newHealthTestClient()
	ctx := context.Background()

	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "my-quota", Namespace: "quota-ns"},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourceLimitsCPU:    resource.MustParse("4"),
				corev1.ResourceLimitsMemory: resource.MustParse("8Gi"),
				corev1.ResourcePods:         resource.MustParse("20"),
			},
			Used: corev1.ResourceList{
				corev1.ResourceLimitsCPU:    resource.MustParse("1"),
				corev1.ResourceLimitsMemory: resource.MustParse("2Gi"),
				corev1.ResourcePods:         resource.MustParse("5"),
			},
		},
	}
	_, _ = client.Clientset.CoreV1().ResourceQuotas("quota-ns").Create(ctx, quota, metav1.CreateOptions{})

	result, err := handleHealthNsUsage(ctx, client, HealthNsUsageParams{Namespace: "quota-ns"})
	if err != nil {
		t.Fatalf("handleHealthNsUsage: %v", err)
	}
	if result.Namespace != "quota-ns" {
		t.Errorf("expected namespace=quota-ns, got %q", result.Namespace)
	}
	if result.CPULimit == "" {
		t.Error("expected CPULimit to be set")
	}
	if result.PodLimit != 20 {
		t.Errorf("expected PodLimit=20, got %d", result.PodLimit)
	}
	if result.PodCount != 5 {
		t.Errorf("expected PodCount=5, got %d", result.PodCount)
	}
	// 5/20 = 25%
	if result.PodPct < 24.9 || result.PodPct > 25.1 {
		t.Errorf("expected PodPct≈25, got %.2f", result.PodPct)
	}
}

func TestHealthClusterSummaryEmpty(t *testing.T) {
	client := newHealthTestClient()
	ctx := context.Background()

	result, err := handleHealthClusterSummary(ctx, client)
	if err != nil {
		t.Fatalf("handleHealthClusterSummary: %v", err)
	}
	if result.TotalNodes != 0 {
		t.Errorf("expected 0 nodes, got %d", result.TotalNodes)
	}
}

func TestHealthClusterSummaryWithNodes(t *testing.T) {
	client := newHealthTestClient()
	ctx := context.Background()

	for i, name := range []string{"node-a", "node-b"} {
		ready := corev1.ConditionTrue
		if i == 1 {
			ready = corev1.ConditionFalse
		}
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: ready},
				},
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3500m"),
					corev1.ResourceMemory: resource.MustParse("7Gi"),
				},
			},
		}
		_, _ = client.Clientset.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	}

	result, err := handleHealthClusterSummary(ctx, client)
	if err != nil {
		t.Fatalf("handleHealthClusterSummary: %v", err)
	}
	if result.TotalNodes != 2 {
		t.Errorf("expected TotalNodes=2, got %d", result.TotalNodes)
	}
	if result.ReadyNodes != 1 {
		t.Errorf("expected ReadyNodes=1, got %d", result.ReadyNodes)
	}
	if result.CPUCapacity == "" {
		t.Error("expected CPUCapacity to be set")
	}
}
