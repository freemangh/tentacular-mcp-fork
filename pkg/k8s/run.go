package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// RunWorkflow triggers a deployed workflow by POSTing to its /run endpoint
// via direct HTTP to the workflow's ClusterIP service. The MCP server runs
// in tentacular-system; NetworkPolicy on the workflow namespace allows ingress
// from tentacular-system on port 8080 via namespaceSelector.
func RunWorkflow(ctx context.Context, client *Client, namespace, name string, input json.RawMessage) (json.RawMessage, error) {
	payload := []byte(`{}`)
	if len(input) > 0 {
		payload = input
	}

	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:8080/run", name, namespace)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request for %s/%s: %w", namespace, name, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trigger workflow %s/%s: %w", namespace, name, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read workflow response: %w", err)
	}

	if len(body) == 0 {
		return json.RawMessage(`null`), nil
	}

	return json.RawMessage(body), nil
}
