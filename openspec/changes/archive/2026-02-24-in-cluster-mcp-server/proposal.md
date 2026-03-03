## Why

Tentacular CLI users currently need broad admin kubeconfig access to manage workflow namespaces, credentials, and cluster operations. This is a security anti-pattern: every developer workstation holds cluster-wide privileges. An in-cluster MCP server eliminates this by proxying Kubernetes operations through a controlled service account with scoped RBAC, letting the CLI (and any MCP-capable client) interact with the cluster over a single authenticated HTTP endpoint instead of raw kube-api access.

## What Changes

- Expose 24 MCP tools across 8 functional groups via Streamable HTTP transport at `/mcp` on `:8080`
- Authenticate all requests with a Bearer token loaded from a mounted Kubernetes Secret
- Enforce self-protection: reject any operation targeting the `tentacular-system` namespace
- Deploy as a single-replica Deployment in `tentacular-system` with a dedicated ServiceAccount, ClusterRole, and kustomize-based manifests
- Scaffold code already exists for the server skeleton (`cmd/tentacular-mcp/main.go`), K8s client wiring (`pkg/k8s/`), auth middleware (`pkg/auth/`), guard (`pkg/guard/`), and MCP server setup (`pkg/server/`); this change fills in all 24 tool implementations and wires them into `registerTools()`

## Capabilities

### New Capabilities
- `namespace-lifecycle`: Create, delete, get, and list tentacular-managed namespaces with PSA labels, resource quotas, limit ranges, network policies, and RBAC scaffolding (tools: `ns_create`, `ns_delete`, `ns_get`, `ns_list`)
- `credential-management`: Issue short-lived ServiceAccount tokens via TokenRequest API, generate scoped kubeconfig YAML, and rotate credentials by recreating the workflow ServiceAccount (tools: `cred_issue_token`, `cred_kubeconfig`, `cred_rotate`)
- `workflow-introspection`: List and inspect pods, tail pod logs, list events, and list jobs/cronjobs within a namespace for debugging and status visibility (tools: `wf_pods`, `wf_logs`, `wf_events`, `wf_jobs`)
- `cluster-ops`: Run preflight validation checks and generate a full cluster profile snapshot covering K8s version, nodes, CNI, storage, runtime classes, and extensions (tools: `cluster_preflight`, `cluster_profile`)
- `gvisor-sandbox`: Check gVisor RuntimeClass availability, apply gVisor annotation to a namespace, and run a verification pod to confirm sandbox isolation is functional (tools: `gvisor_check`, `gvisor_annotate_ns`, `gvisor_verify`)
- `module-proxy`: Apply and remove a Helm/module release in a managed namespace using the dynamic client, with template rendering and resource tracking (tools: `module_apply`, `module_remove`, `module_status`)
- `cluster-health`: Query node readiness, namespace resource utilization vs. quota, and overall cluster resource summary (tools: `health_nodes`, `health_ns_usage`, `health_cluster_summary`)
- `security-audit`: Scan namespace RBAC for over-permissioned roles, verify network policy coverage, and validate Pod Security Admission labels (tools: `audit_rbac`, `audit_netpol`, `audit_psa`)

### Modified Capabilities
<!-- No existing specs to modify -->

## Impact

- **Code**: All 24 tool handler functions added under `pkg/tools/` (new package, one file per group); `pkg/server/server.go` updated to call each group's registration function; existing `pkg/k8s/` functions consumed by tool handlers
- **API**: New MCP Streamable HTTP endpoint at `/mcp` on port 8080; existing `/healthz` endpoint unchanged
- **Dependencies**: go-sdk v1.2.0 and client-go v0.35.0 already declared in `go.mod`; no new external dependencies expected
- **Kubernetes RBAC**: The server's ServiceAccount needs a ClusterRole with permissions spanning namespaces, nodes, storage classes, runtime classes, and authorization reviews; deploy manifests in `deploy/manifests/` will need a ClusterRole and ClusterRoleBinding added. RBAC scope was refined based on real-world credential manifest analysis (OpenClaw), adding: `patch` verb on namespace-scoped workflow resources, `serviceaccounts` patch/update for imagePullSecrets, `ingresses` (networking.k8s.io) for deployment workflows, `watch` verb for event streaming, and broader read-only ClusterRole coverage for profiling (replicasets, daemonsets, statefulsets, persistentvolumes, persistentvolumeclaims, endpoints, endpointslices, volumeattachments).
- **Security**: Bearer token auth on all non-healthz endpoints; `tentacular-system` namespace guard enforced on every tool; PSA restricted profile on all created namespaces; default-deny NetworkPolicy applied to all new namespaces
- **Deployment**: Single Deployment in `tentacular-system`, ClusterIP Service, kustomize overlay; no ingress required (CLI port-forwards or uses in-cluster DNS)
