package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ExoStatusParams are the parameters for exo_status (none required).
type ExoStatusParams struct{}

// ExoStatusResult is the result of exo_status.
type ExoStatusResult struct {
	Enabled           bool   `json:"enabled"`
	CleanupOnUndeploy bool   `json:"cleanup_on_undeploy"`
	PostgresAvailable bool   `json:"postgres_available"`
	NATSAvailable     bool   `json:"nats_available"`
	RustFSAvailable   bool   `json:"rustfs_available"`
	SPIREAvailable    bool   `json:"spire_available"`
	AuthEnabled       bool   `json:"auth_enabled"`
	AuthIssuer        string `json:"auth_issuer,omitempty"`
}

// ExoRegistrationParams are the parameters for exo_registration.
type ExoRegistrationParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace of the workflow"`
	Name      string `json:"name" jsonschema:"Workflow deployment name"`
}

// ExoRegistrationResult is the result of exo_registration.
type ExoRegistrationResult struct {
	Found     bool              `json:"found"`
	Namespace string            `json:"namespace"`
	Name      string            `json:"name"`
	Data      map[string]string `json:"data,omitempty"`
}

func registerExoskeletonTools(srv *mcp.Server, client *k8s.Client, ctrl *exoskeleton.Controller) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "exo_status",
		Description: "Return exoskeleton feature status including which backing services are available.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params ExoStatusParams) (*mcp.CallToolResult, ExoStatusResult, error) {
		if ctrl == nil {
			return nil, ExoStatusResult{Enabled: false}, nil
		}
		return nil, ExoStatusResult{
			Enabled:           ctrl.Enabled(),
			CleanupOnUndeploy: ctrl.CleanupOnUndeploy(),
			PostgresAvailable: ctrl.PostgresAvailable(),
			NATSAvailable:     ctrl.NATSAvailable(),
			RustFSAvailable:   ctrl.RustFSAvailable(),
			SPIREAvailable:    ctrl.SPIREAvailable(),
			AuthEnabled:       ctrl.AuthEnabled(),
			AuthIssuer:        ctrl.AuthIssuer(),
		}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "exo_registration",
		Description: "Return exoskeleton registration details (Secret contents) for a workflow deployment.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params ExoRegistrationParams) (*mcp.CallToolResult, ExoRegistrationResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, ExoRegistrationResult{}, err
		}

		secretName := "tentacular-exoskeleton-" + params.Name
		secret, err := client.Clientset.CoreV1().Secrets(params.Namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			return nil, ExoRegistrationResult{
				Found:     false,
				Namespace: params.Namespace,
				Name:      params.Name,
			}, fmt.Errorf("exoskeleton secret %s/%s not found: %w", params.Namespace, secretName, err)
		}

		data := make(map[string]string)
		for k, v := range secret.Data {
			// Redact password and secret key values.
			if isSecretKey(k) {
				data[k] = "***REDACTED***"
			} else {
				data[k] = string(v)
			}
		}

		return nil, ExoRegistrationResult{
			Found:     true,
			Namespace: params.Namespace,
			Name:      params.Name,
			Data:      data,
		}, nil
	})
}

// isSecretKey returns true for keys that contain sensitive values.
func isSecretKey(key string) bool {
	sensitiveSuffixes := []string{".password", ".secret_key", ".access_key", ".token"}
	for _, suffix := range sensitiveSuffixes {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}
