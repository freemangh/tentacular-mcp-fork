package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const runRetryInterval = 3 * time.Second

// RunWorkflow triggers a deployed workflow by POSTing to its /run endpoint
// via direct HTTP to the workflow's ClusterIP service. The MCP server runs
// in tentacular-system; NetworkPolicy on the workflow namespace allows ingress
// from tentacular-system on port 8080 via namespaceSelector.
//
// Dial errors (connection refused, etc.) are retried until the context deadline
// is exceeded — this covers the startup window between wf_apply returning and
// the pod becoming ready to serve requests.
func RunWorkflow(ctx context.Context, client *Client, namespace, name string, input json.RawMessage) (json.RawMessage, error) {
	payload := []byte(`{}`)
	if len(input) > 0 {
		payload = input
	}

	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:8080/run", name, namespace)

	var lastErr error
	for {
		body, isConn, err := tryRunOnce(ctx, client, url, payload, namespace, name)
		if err == nil {
			return body, nil
		}
		if !isConn {
			return nil, err
		}
		lastErr = err

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("trigger workflow %s/%s: %w (last error: %v)", namespace, name, ctx.Err(), lastErr)
		case <-time.After(runRetryInterval):
		}
	}
}

// tryRunOnce makes a single HTTP POST to the workflow /run endpoint.
// Returns (body, isConnError, error).
func tryRunOnce(ctx context.Context, client *Client, url string, payload []byte, namespace, name string) (json.RawMessage, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, false, fmt.Errorf("create request for %s/%s: %w", namespace, name, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.HTTP.Do(req)
	if err != nil {
		return nil, isDialError(err), fmt.Errorf("trigger workflow %s/%s: %w", namespace, name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("read workflow response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("workflow %s/%s returned HTTP %d: %s", namespace, name, resp.StatusCode, string(body))
	}

	if len(body) == 0 {
		return json.RawMessage(`null`), false, nil
	}

	return json.RawMessage(body), false, nil
}

// isDialError reports whether err is a network dial failure (connection refused,
// connection reset, etc.) that may resolve once the pod is ready.
func isDialError(err error) bool {
	var netErr *net.OpError
	return errors.As(err, &netErr) && netErr.Op == "dial"
}
