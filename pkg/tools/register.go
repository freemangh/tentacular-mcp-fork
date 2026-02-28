package tools

import (
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
	"github.com/randybias/tentacular-mcp/pkg/proxy"
	"github.com/randybias/tentacular-mcp/pkg/scheduler"
)

// RegisterAll registers all MCP tools with the given server.
func RegisterAll(srv *mcp.Server, client *k8s.Client, reconciler *proxy.Reconciler, sched *scheduler.Scheduler, logger *slog.Logger) {
	_ = logger
	registerNamespaceTools(srv, client)
	registerCredentialTools(srv, client)
	registerWorkflowTools(srv, client)
	registerRunTools(srv, client)
	registerDiscoverTools(srv, client)
	registerClusterOpsTools(srv, client)
	registerGVisorTools(srv, client)
	registerDeployTools(srv, client, sched)
	registerHealthTools(srv, client)
	registerAuditTools(srv, client)
	registerProxyTools(srv, reconciler)
}
