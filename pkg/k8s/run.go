package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
)

// RunWorkflowPod creates an ephemeral curl pod that POSTs the given input to
// http://<name>.<namespace>.svc:8080/run, waits for it to complete, captures
// stdout as the JSON output, and deletes the pod on return. The pod is always
// deleted even if the run fails. Returns the pod name, the raw JSON output
// bytes, and any error.
//
// The curl command uses --retry to tolerate kube-router NetworkPolicy ipset
// sync races (same pattern as the CLI RunWorkflow helper).
func RunWorkflowPod(ctx context.Context, client *Client, namespace, name string, input json.RawMessage) (string, json.RawMessage, error) {
	svcURL := fmt.Sprintf("http://%s.%s.svc:8080/run", name, namespace)
	podName := fmt.Sprintf("tntc-run-%s-%d", name, time.Now().UnixMilli())

	// Default to empty JSON object if no input provided.
	// Limit payload size to 1MB to avoid exceeding container arg limits.
	const maxPayloadBytes = 1 << 20 // 1MB
	payload := `{}`
	if len(input) > 0 {
		if len(input) > maxPayloadBytes {
			return "", nil, fmt.Errorf("payload too large (%d bytes); maximum is %d bytes", len(input), maxPayloadBytes)
		}
		payload = string(input)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				ManagedByLabel:           ManagedByValue,
				"tentacular/run-target":  name,
				"tentacular.dev/role":    "trigger",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:  "curl",
					Image: "curlimages/curl:latest",
					Command: []string{
						"curl", "-sf",
						"--retry", "5",
						"--retry-connrefused",
						"--retry-delay", "1",
						"-X", "POST",
						"-H", "Content-Type: application/json",
						"-d", payload,
						svcURL,
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("32Mi"),
							corev1.ResourceCPU:    resource.MustParse("100m"),
						},
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: boolPtr(false),
						RunAsNonRoot:             boolPtr(true),
						RunAsUser:                int64Ptr(65534),
						ReadOnlyRootFilesystem:   boolPtr(true),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
						},
					},
				},
			},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: boolPtr(true),
				RunAsUser:    int64Ptr(65534),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
		},
	}

	created, err := client.Clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return "", nil, fmt.Errorf("create runner pod: %w", err)
	}

	// Always clean up the trigger pod, even on error
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.Clientset.CoreV1().Pods(namespace).Delete(cleanupCtx, podName, metav1.DeleteOptions{})
	}()

	// If the pod already completed before we started watching, skip watch
	if created.Status.Phase != corev1.PodSucceeded && created.Status.Phase != corev1.PodFailed {
		watcher, err := client.Clientset.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{
			FieldSelector: fields.OneTermEqualSelector("metadata.name", podName).String(),
		})
		if err != nil {
			return podName, nil, fmt.Errorf("watch runner pod: %w", err)
		}
		defer watcher.Stop()

	watchLoop:
		for {
			select {
			case <-ctx.Done():
				return podName, nil, fmt.Errorf("context cancelled waiting for runner pod: %w", ctx.Err())
			case event, ok := <-watcher.ResultChan():
				if !ok {
					break watchLoop
				}
				if event.Type == watch.Error {
					return podName, nil, fmt.Errorf("watch error for runner pod %q", podName)
				}
				p, podOK := event.Object.(*corev1.Pod)
				if !podOK {
					continue
				}
				if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
					break watchLoop
				}
			}
		}
	}

	// Fetch final pod state to check phase
	finalPod, err := client.Clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return podName, nil, fmt.Errorf("get runner pod status: %w", err)
	}

	// Capture stdout logs
	logStream, err := client.Clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{}).Stream(ctx)
	if err != nil {
		return podName, nil, fmt.Errorf("read runner pod logs: %w", err)
	}
	defer logStream.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, logStream); err != nil {
		return podName, nil, fmt.Errorf("copy runner pod output: %w", err)
	}

	if finalPod.Status.Phase == corev1.PodFailed {
		return podName, nil, fmt.Errorf("workflow run failed: %s", buf.String())
	}

	output := json.RawMessage(buf.Bytes())
	if len(output) == 0 {
		output = json.RawMessage(`null`)
	}

	return podName, output, nil
}

func boolPtr(b bool) *bool {
	return &b
}

func int64Ptr(i int64) *int64 {
	return &i
}
