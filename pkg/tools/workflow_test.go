package tools

import (
	"context"
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

func newWfTestClient() *k8s.Client {
	return &k8s.Client{
		Clientset: fake.NewClientset(),
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}
}

func TestWfPods(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-1",
			Namespace: "my-ns",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Image: "myapp:v1"},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	_, err := client.Clientset.CoreV1().Pods("my-ns").Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := handleWfPods(ctx, client, WfPodsParams{Namespace: "my-ns"})
	if err != nil {
		t.Fatalf("handleWfPods: %v", err)
	}
	if len(result.Pods) != 1 {
		t.Errorf("expected 1 pod, got %d", len(result.Pods))
	}
	if result.Pods[0].Name != "worker-1" {
		t.Errorf("pod name: got %q, want %q", result.Pods[0].Name, "worker-1")
	}
	if result.Pods[0].Phase != "Running" {
		t.Errorf("pod phase: got %q, want %q", result.Pods[0].Phase, "Running")
	}
}

func TestWfPodsReportsImages(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "img-pod", Namespace: "img-ns"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "c1", Image: "nginx:1.25"},
				{Name: "c2", Image: "redis:7"},
			},
		},
	}
	_, _ = client.Clientset.CoreV1().Pods("img-ns").Create(ctx, pod, metav1.CreateOptions{})

	result, err := handleWfPods(ctx, client, WfPodsParams{Namespace: "img-ns"})
	if err != nil {
		t.Fatalf("handleWfPods: %v", err)
	}
	if len(result.Pods) == 0 {
		t.Fatal("expected at least one pod")
	}
	if len(result.Pods[0].Images) != 2 {
		t.Errorf("expected 2 images, got %d: %v", len(result.Pods[0].Images), result.Pods[0].Images)
	}
}

func TestWfPodsReportsRestarts(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "restarting", Namespace: "rst-ns"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "myapp:latest"}}},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "app", RestartCount: 5},
			},
		},
	}
	_, _ = client.Clientset.CoreV1().Pods("rst-ns").Create(ctx, pod, metav1.CreateOptions{})

	result, err := handleWfPods(ctx, client, WfPodsParams{Namespace: "rst-ns"})
	if err != nil {
		t.Fatalf("handleWfPods: %v", err)
	}
	if len(result.Pods) == 0 {
		t.Fatal("expected at least one pod")
	}
	if result.Pods[0].Restarts != 5 {
		t.Errorf("expected 5 restarts, got %d", result.Pods[0].Restarts)
	}
}

func TestWfPodsReadyFlag(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "ready-pod", Namespace: "ready-ns"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "app:v1"}}},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
	_, _ = client.Clientset.CoreV1().Pods("ready-ns").Create(ctx, pod, metav1.CreateOptions{})

	result, err := handleWfPods(ctx, client, WfPodsParams{Namespace: "ready-ns"})
	if err != nil {
		t.Fatalf("handleWfPods: %v", err)
	}
	if len(result.Pods) == 0 {
		t.Fatal("expected at least one pod")
	}
	if !result.Pods[0].Ready {
		t.Error("expected Ready=true for pod with PodReady condition")
	}
}

func TestWfPodsEmpty(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	result, err := handleWfPods(ctx, client, WfPodsParams{Namespace: "empty-ns"})
	if err != nil {
		t.Fatalf("handleWfPods: %v", err)
	}
	if result.Pods == nil {
		t.Error("expected non-nil pods slice for empty namespace")
	}
	if len(result.Pods) != 0 {
		t.Errorf("expected 0 pods, got %d", len(result.Pods))
	}
}

func TestWfEvents(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "evt-1",
			Namespace: "my-ns",
		},
		Type:    "Warning",
		Reason:  "OOMKilled",
		Message: "pod ran out of memory",
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod",
			Name: "worker-1",
		},
		Count: 3,
	}
	_, err := client.Clientset.CoreV1().Events("my-ns").Create(ctx, event, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := handleWfEvents(ctx, client, WfEventsParams{Namespace: "my-ns"})
	if err != nil {
		t.Fatalf("handleWfEvents: %v", err)
	}
	if len(result.Events) != 1 {
		t.Errorf("expected 1 event, got %d", len(result.Events))
	}
}

func TestWfEventsObjectFormat(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	_, _ = client.Clientset.CoreV1().Events("fmt-ns").Create(ctx, &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "e1", Namespace: "fmt-ns"},
		Type:           "Normal",
		Reason:         "Started",
		Message:        "container started",
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "mypod"},
		Count:          1,
	}, metav1.CreateOptions{})

	result, err := handleWfEvents(ctx, client, WfEventsParams{Namespace: "fmt-ns"})
	if err != nil {
		t.Fatalf("handleWfEvents: %v", err)
	}
	if len(result.Events) == 0 {
		t.Fatal("expected at least one event")
	}
	if result.Events[0].Object != "Pod/mypod" {
		t.Errorf("expected Object='Pod/mypod', got %q", result.Events[0].Object)
	}
	if result.Events[0].Count != 1 {
		t.Errorf("expected Count=1, got %d", result.Events[0].Count)
	}
}

func TestWfEventsEmpty(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	result, err := handleWfEvents(ctx, client, WfEventsParams{Namespace: "empty-ns"})
	if err != nil {
		t.Fatalf("handleWfEvents on empty namespace: %v", err)
	}
	if result.Events == nil {
		t.Error("expected non-nil events slice")
	}
}

func TestWfJobs(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "data-processor",
			Namespace: "my-ns",
		},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}
	_, err := client.Clientset.BatchV1().Jobs("my-ns").Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup job: %v", err)
	}

	schedule := "0 2 * * *"
	cj := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nightly-cleanup",
			Namespace: "my-ns",
		},
		Spec: batchv1.CronJobSpec{
			Schedule: schedule,
		},
	}
	_, err = client.Clientset.BatchV1().CronJobs("my-ns").Create(ctx, cj, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup cronjob: %v", err)
	}

	result, err := handleWfJobs(ctx, client, WfJobsParams{Namespace: "my-ns"})
	if err != nil {
		t.Fatalf("handleWfJobs: %v", err)
	}
	if len(result.Jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(result.Jobs))
	}
	if result.Jobs[0].Status != "Complete" {
		t.Errorf("job status: got %q, want Complete", result.Jobs[0].Status)
	}
	if len(result.CronJobs) != 1 {
		t.Errorf("expected 1 cronjob, got %d", len(result.CronJobs))
	}
	if result.CronJobs[0].Schedule != schedule {
		t.Errorf("cronjob schedule: got %q, want %q", result.CronJobs[0].Schedule, schedule)
	}
}

func TestWfJobsFailedStatus(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "failed-job", Namespace: "fail-ns"},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobFailed, Status: corev1.ConditionTrue},
			},
		},
	}
	_, _ = client.Clientset.BatchV1().Jobs("fail-ns").Create(ctx, job, metav1.CreateOptions{})

	result, err := handleWfJobs(ctx, client, WfJobsParams{Namespace: "fail-ns"})
	if err != nil {
		t.Fatalf("handleWfJobs: %v", err)
	}
	if len(result.Jobs) == 0 {
		t.Fatal("expected at least one job")
	}
	if result.Jobs[0].Status != "Failed" {
		t.Errorf("expected status=Failed, got %q", result.Jobs[0].Status)
	}
}

func TestWfJobsRunningStatus(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "running-job", Namespace: "run-ns"},
		Status:     batchv1.JobStatus{Active: 1},
	}
	_, _ = client.Clientset.BatchV1().Jobs("run-ns").Create(ctx, job, metav1.CreateOptions{})

	result, err := handleWfJobs(ctx, client, WfJobsParams{Namespace: "run-ns"})
	if err != nil {
		t.Fatalf("handleWfJobs: %v", err)
	}
	if len(result.Jobs) == 0 {
		t.Fatal("expected at least one job")
	}
	if result.Jobs[0].Status != "Running" {
		t.Errorf("expected status=Running, got %q", result.Jobs[0].Status)
	}
}

func TestWfJobsPendingStatus(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "pending-job", Namespace: "pend-ns"},
		Status:     batchv1.JobStatus{},
	}
	_, _ = client.Clientset.BatchV1().Jobs("pend-ns").Create(ctx, job, metav1.CreateOptions{})

	result, err := handleWfJobs(ctx, client, WfJobsParams{Namespace: "pend-ns"})
	if err != nil {
		t.Fatalf("handleWfJobs: %v", err)
	}
	if len(result.Jobs) == 0 {
		t.Fatal("expected at least one job")
	}
	if result.Jobs[0].Status != "Pending" {
		t.Errorf("expected status=Pending, got %q", result.Jobs[0].Status)
	}
}

func TestWfJobsCronJobSuspended(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	suspended := true
	cj := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "suspended-cj", Namespace: "susp-ns"},
		Spec: batchv1.CronJobSpec{
			Schedule: "*/5 * * * *",
			Suspend:  &suspended,
		},
	}
	_, _ = client.Clientset.BatchV1().CronJobs("susp-ns").Create(ctx, cj, metav1.CreateOptions{})

	result, err := handleWfJobs(ctx, client, WfJobsParams{Namespace: "susp-ns"})
	if err != nil {
		t.Fatalf("handleWfJobs: %v", err)
	}
	if len(result.CronJobs) == 0 {
		t.Fatal("expected at least one cronjob")
	}
	if !result.CronJobs[0].Suspended {
		t.Error("expected Suspended=true for suspended cronjob")
	}
}

func TestWfRestartSuccess(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	// Create a managed namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "restart-ns",
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
		},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: create ns: %v", err)
	}

	// Create a deployment
	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-app",
			Namespace: "restart-ns",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "web"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "web", Image: "nginx:1.25"},
					},
				},
			},
		},
	}
	_, err = client.Clientset.AppsV1().Deployments("restart-ns").Create(ctx, dep, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: create deployment: %v", err)
	}

	result, err := handleWfRestart(ctx, client, WfRestartParams{
		Namespace:  "restart-ns",
		Deployment: "web-app",
	})
	if err != nil {
		t.Fatalf("handleWfRestart: %v", err)
	}
	if !result.Restarted {
		t.Error("expected Restarted=true")
	}
	if result.Namespace != "restart-ns" {
		t.Errorf("namespace: got %q, want %q", result.Namespace, "restart-ns")
	}
	if result.Deployment != "web-app" {
		t.Errorf("deployment: got %q, want %q", result.Deployment, "web-app")
	}

	// Verify the annotation was set on the pod template
	updated, err := client.Clientset.AppsV1().Deployments("restart-ns").Get(ctx, "web-app", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get updated deployment: %v", err)
	}
	ann := updated.Spec.Template.Annotations
	if ann == nil {
		t.Fatal("expected annotations on pod template after restart")
	}
	if _, ok := ann["tentacular.io/restartedAt"]; !ok {
		t.Error("expected tentacular.io/restartedAt annotation on pod template")
	}
}

func TestWfRestartUnmanagedNamespace(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	// Create an unmanaged namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "unmanaged-ns"},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: create ns: %v", err)
	}

	_, err = handleWfRestart(ctx, client, WfRestartParams{
		Namespace:  "unmanaged-ns",
		Deployment: "web-app",
	})
	if err == nil {
		t.Fatal("expected error for unmanaged namespace, got nil")
	}
}

func TestWfRestartDeploymentNotFound(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	// Create a managed namespace but no deployment
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "empty-restart-ns",
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
		},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: create ns: %v", err)
	}

	_, err = handleWfRestart(ctx, client, WfRestartParams{
		Namespace:  "empty-restart-ns",
		Deployment: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent deployment, got nil")
	}
}

// --- handleWfEvents (additional coverage) ---

func TestWfEventsTimestampFormatting(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	now := metav1.Now()
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-event-1",
			Namespace: "my-ns",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod",
			Name: "worker-1",
		},
		Type:          "Normal",
		Reason:        "Scheduled",
		Message:       "Successfully assigned pod",
		Count:         1,
		LastTimestamp:  now,
	}
	_, err := client.Clientset.CoreV1().Events("my-ns").Create(ctx, event, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := handleWfEvents(ctx, client, WfEventsParams{Namespace: "my-ns"})
	if err != nil {
		t.Fatalf("handleWfEvents: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(result.Events))
	}
	ev := result.Events[0]
	if ev.Type != "Normal" {
		t.Errorf("Type: got %q, want %q", ev.Type, "Normal")
	}
	if ev.Reason != "Scheduled" {
		t.Errorf("Reason: got %q, want %q", ev.Reason, "Scheduled")
	}
	if ev.Object != "Pod/worker-1" {
		t.Errorf("Object: got %q, want %q", ev.Object, "Pod/worker-1")
	}
	if ev.Count != 1 {
		t.Errorf("Count: got %d, want 1", ev.Count)
	}
	if ev.LastSeen == "" {
		t.Error("expected LastSeen to be set")
	}
}

func TestWfEventsDefaultLimit(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	// Create 3 events
	for i := 0; i < 3; i++ {
		event := &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("event-%d", i),
				Namespace: "my-ns",
			},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "worker"},
			Type:           "Normal",
			Reason:         "Pulled",
			Message:        fmt.Sprintf("Event %d", i),
		}
		_, _ = client.Clientset.CoreV1().Events("my-ns").Create(ctx, event, metav1.CreateOptions{})
	}

	// Limit=0 should default to 100 (no truncation for 3 events)
	result, err := handleWfEvents(ctx, client, WfEventsParams{Namespace: "my-ns", Limit: 0})
	if err != nil {
		t.Fatalf("handleWfEvents: %v", err)
	}
	if len(result.Events) != 3 {
		t.Errorf("expected 3 events with default limit, got %d", len(result.Events))
	}
}

func TestWfEventsZeroTimestamp(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-ts-event",
			Namespace: "my-ns",
		},
		InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "my-dep"},
		Type:           "Warning",
		Reason:         "FailedScheduling",
		Message:        "No nodes available",
		// LastTimestamp is zero
	}
	_, _ = client.Clientset.CoreV1().Events("my-ns").Create(ctx, event, metav1.CreateOptions{})

	result, err := handleWfEvents(ctx, client, WfEventsParams{Namespace: "my-ns"})
	if err != nil {
		t.Fatalf("handleWfEvents: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(result.Events))
	}
	// Zero timestamp should produce empty string
	if result.Events[0].LastSeen != "" {
		t.Errorf("expected empty LastSeen for zero timestamp, got %q", result.Events[0].LastSeen)
	}
}
