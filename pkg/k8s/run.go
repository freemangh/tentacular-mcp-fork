package k8s

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
)

// RunWorkflow triggers a deployed workflow by POSTing to its /run endpoint
// via the Kubernetes API service proxy. This routes the request through the
// API server directly to the workflow service, eliminating the need for
// ephemeral trigger pods, container images, NetworkPolicy rules for triggers,
// and kube-router ipset sync races.
//
// The API server proxies: POST /api/v1/namespaces/{ns}/services/{svc}:8080/proxy/run
func RunWorkflow(ctx context.Context, client *Client, namespace, name string, input json.RawMessage) (json.RawMessage, error) {
	payload := []byte(`{}`)
	if len(input) > 0 {
		payload = input
	}

	proxyPath := fmt.Sprintf("/api/v1/namespaces/%s/services/%s:8080/proxy/run", namespace, name)

	result := client.Clientset.CoreV1().RESTClient().
		Post().
		AbsPath(proxyPath).
		SetHeader("Content-Type", "application/json").
		Body(payload).
		Do(ctx)

	// Get the raw response body regardless of HTTP status.
	// The workflow engine returns error details in the body even on 500.
	raw, rawErr := result.Raw()

	// Check for transport-level errors (connection refused, timeout, etc.)
	if err := result.Error(); err != nil {
		// If we have a response body, return it alongside the error context.
		// The body contains workflow execution details (errors, timing, etc.)
		if len(raw) > 0 && isProxyResponseError(err) {
			return json.RawMessage(raw), nil
		}
		return nil, fmt.Errorf("trigger workflow %s/%s: %w", namespace, name, err)
	}

	if rawErr != nil {
		return nil, fmt.Errorf("read workflow response: %w", rawErr)
	}

	if len(raw) == 0 {
		return json.RawMessage(`null`), nil
	}

	return json.RawMessage(raw), nil
}

// isProxyResponseError returns true if the error is from the proxied backend
// (e.g., the workflow returned 500) rather than a K8s API transport error.
func isProxyResponseError(err error) bool {
	if statusErr, ok := err.(*errors.StatusError); ok {
		code := statusErr.ErrStatus.Code
		return code >= 400 && code < 600
	}
	return false
}
