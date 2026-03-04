package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
)

// MCPTestClient is a helper for sending HTTP requests to an in-process server
// under test.
type MCPTestClient struct {
	server *httptest.Server
	token  string
	client *http.Client
}

// NewMCPTestClient creates a test client pointed at the given httptest.Server.
// Pass the bearer token that the server expects.
func NewMCPTestClient(srv *httptest.Server, token string) *MCPTestClient {
	return &MCPTestClient{
		server: srv,
		token:  token,
		client: srv.Client(),
	}
}

// Get performs an authenticated GET to the given path.
func (c *MCPTestClient) Get(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, c.server.URL+path, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.client.Do(req)
}

// PostJSON performs an authenticated POST with a JSON body to the given path.
func (c *MCPTestClient) PostJSON(path string, body interface{}) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.server.URL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.client.Do(req)
}

// ReadBody reads and returns the response body as a string, closing the body.
func ReadBody(resp *http.Response) (string, error) {
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
