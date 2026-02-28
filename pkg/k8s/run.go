package k8s

import (
	"context"
	"encoding/json"
	"fmt"
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

	raw, err := client.Clientset.CoreV1().RESTClient().
		Post().
		AbsPath(proxyPath).
		SetHeader("Content-Type", "application/json").
		Body(payload).
		DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("trigger workflow %s/%s: %w", namespace, name, err)
	}

	if len(raw) == 0 {
		return json.RawMessage(`null`), nil
	}

	return json.RawMessage(raw), nil
}
