# tentacular-mcp

An in-cluster MCP (Model Context Protocol) server for Kubernetes namespace lifecycle, credential management, workflow introspection, and cluster operations. Replaces direct kube-api access from developer workstations with a single authenticated HTTP endpoint backed by scoped RBAC.

## Why

Developer workstations holding cluster-wide admin kubeconfig is a security anti-pattern. tentacular-mcp proxies Kubernetes operations through a controlled ServiceAccount so CLI clients (and any MCP-capable client) interact with the cluster over one authenticated endpoint instead of raw kube-api access.

## Architecture

```
+------------------+        +-------------------------------------+
|  tentacular CLI  |        |   tentacular-system namespace       |
|  (or any MCP     | Bearer |                                     |
|   client)        +------->+  tentacular-mcp Deployment          |
|                  |  :8080 |  +-------------------------------+  |
+------------------+  /mcp  |  | auth.Middleware (Bearer token) |  |
                            |  |   |                            |  |
                            |  | server.Handler (MCP SDK)       |  |
                            |  |   |                            |  |
                            |  | pkg/tools/register.go          |  |
                            |  |   |  guard.CheckNamespace()    |  |
                            |  |   |  unmarshal params          |  |
                            |  |   |  call handler              |  |
                            |  |   |  marshal result            |  |
                            |  |   v                            |  |
                            |  | pkg/tools/*.go (25 tools)      |  |
                            |  |   |                            |  |
                            |  |   v                            |  |
                            |  | pkg/k8s/* (K8s client layer)   |  |
                            |  +---+---------------------------+  |
                            |      |                              |
                            +------+------------------------------+
                                   |
                                   v
                            +------+------------------------------+
                            |  Kubernetes API Server              |
                            +-------------------------------------+
```

### Request Flow

1. HTTP request hits `:8080/mcp` with `Authorization: Bearer <token>`
2. `auth.Middleware` validates the token (rejects with 401 if invalid; bypasses for `/healthz`)
3. MCP SDK `StreamableHTTPHandler` parses the message and routes to the registered tool
4. `register.go` wrapper: unmarshal params, run `guard.CheckNamespace()`, call handler, marshal result
5. Handler calls `pkg/k8s` functions using in-cluster `rest.Config`
6. Result returned as MCP `Content` with `type: "text"` containing JSON

## Prerequisites

- Go 1.25+
- A Kubernetes cluster (kind works for local development)
- `kubectl` configured with cluster access
- Docker (for building the container image)
- `openssl` (for generating the auth token)

## Quick Start

### Deploy via tntc CLI (Recommended)

The easiest way to deploy tentacular-mcp is through the
`tntc` CLI bootstrap command:

```bash
tntc cluster install
```

This auto-generates a token, deploys the server, and
saves the MCP config to `~/.tentacular/config.yaml`.

### Build from Source

```bash
# Build the binary
make build

# Build the Docker image
make docker-build
```

### Manual Deploy (Kustomize)

For manual deployment without the tntc CLI:

```bash
kubectl apply -k deploy/manifests/
```

This creates:
- `tentacular-system` namespace
- ServiceAccount, ClusterRole, and ClusterRoleBinding
- Auth Secret
- Deployment (single replica, distroless container, non-root)
- ClusterIP Service on port 8080

### Connect

```bash
# Port-forward to reach the server from outside the cluster
kubectl port-forward -n tentacular-system svc/tentacular-mcp 8080:8080

# Verify the health endpoint
curl http://localhost:8080/healthz

# Send an MCP initialize request (using any MCP client)
# The server listens on /mcp via Streamable HTTP transport
```

## MCP Tools

28 tools organized across 10 functional groups. All namespace-scoped tools enforce a self-protection guard that rejects operations targeting `tentacular-system`.

### Namespace Lifecycle

| Tool | Description |
|------|-------------|
| `ns_create` | Create a managed namespace with PSA labels, default-deny NetworkPolicy, DNS-allow policy, ResourceQuota, LimitRange, and workflow SA/Role/RoleBinding. Accepts `small`, `medium`, or `large` quota presets. |
| `ns_delete` | Delete a managed namespace and all child resources. |
| `ns_get` | Get namespace details including labels, annotations, quota summary, and limit range. |
| `ns_list` | List all tentacular-managed namespaces. |

### Credential Management

| Tool | Description |
|------|-------------|
| `cred_issue_token` | Issue a short-lived ServiceAccount token via the TokenRequest API. TTL configurable from 10 to 1440 minutes. |
| `cred_kubeconfig` | Generate a scoped kubeconfig YAML containing a time-limited token, cluster CA, and API server URL. |
| `cred_rotate` | Rotate credentials by recreating the workflow ServiceAccount, invalidating all prior tokens. |

### Workflow Introspection

| Tool | Description |
|------|-------------|
| `wf_pods` | List pods in a namespace with phase, readiness, restart count, images, and age. |
| `wf_logs` | Tail pod logs (snapshot, not streaming). Supports container selection and line count. |
| `wf_events` | List namespace events with type, reason, message, object reference, and count. |
| `wf_jobs` | List Jobs and CronJobs in a namespace with status, schedule, and duration. |

### Cluster Operations

| Tool | Description |
|------|-------------|
| `cluster_preflight` | Run preflight validation checks (API connectivity, namespace access, RBAC, gVisor availability). |
| `cluster_profile` | Generate a full cluster profile: K8s version, nodes, CNI, storage classes, runtime classes, and extensions. |

### gVisor Sandbox

| Tool | Description |
|------|-------------|
| `gvisor_check` | Check if a gVisor RuntimeClass is available in the cluster. |
| `gvisor_apply` | Apply gVisor annotation to a namespace. |
| `gvisor_verify` | Run a verification pod to confirm gVisor sandbox isolation is functional. |

### Deploy Lifecycle

| Tool | Description |
|------|-------------|
| `wf_apply` | Apply arbitrary Kubernetes manifests as a named deployment using the dynamic client. Tracks resources by name label for garbage collection. |
| `wf_remove` | Remove all resources associated with a deployment name. |
| `wf_status` | Check the status of all resources in a named deployment. |

### Workflow Execution

| Tool | Description |
|------|-------------|
| `wf_run` | Trigger a deployed workflow by POSTing to the workflow's `/run` endpoint via the Kubernetes API service proxy. Returns the JSON output with execution duration. Timeout configurable (default 120s, max 600s). No ephemeral pods are created. |

### Module Proxy

| Tool | Description |
|------|-------------|
| `proxy_status` | Check installation and readiness status of the in-cluster module proxy (esm.sh). Returns installed state, readiness, namespace, image, and storage type. |

### Cluster Health

| Tool | Description |
|------|-------------|
| `health_nodes` | Query node readiness, capacity, allocatable resources, and conditions. |
| `health_ns_usage` | Report namespace resource utilization vs. quota (CPU, memory, pod count). |
| `health_cluster_summary` | Overall cluster resource summary: total nodes, pods, CPU and memory capacity/requested. |

### Security Audit

| Tool | Description |
|------|-------------|
| `audit_rbac` | Scan namespace RBAC for over-permissioned roles (wildcard verbs, escalation paths). |
| `audit_netpol` | Verify NetworkPolicy coverage: default-deny presence, policy analysis. |
| `audit_psa` | Validate Pod Security Admission labels against the restricted profile. |

## Authentication

All requests to `/mcp` require a `Authorization: Bearer <token>` header. The `/healthz` endpoint is unauthenticated.

The server loads its expected token from the `TENTACULAR_MCP_TOKEN` environment variable. In the standard deployment, this is populated from the `tentacular-mcp-token` Kubernetes Secret via `secretKeyRef`.

### Generating a Token

```bash
# Generate a 32-byte hex token
openssl rand -hex 32
```

When deployed via `tntc cluster install`, the token is
auto-generated and saved to `~/.tentacular/mcp-token`.

### Retrieving a Deployed Token

```bash
kubectl get secret tentacular-mcp-token -n tentacular-system \
  -o jsonpath='{.data.token}' | base64 -d
```

## Deployment

### Kustomize Deploy

```bash
kubectl apply -k deploy/manifests/
```

The kustomization deploys these resources in order:
1. `tentacular-system` Namespace
2. ServiceAccount + ClusterRole + ClusterRoleBinding
3. Auth Secret
4. Deployment (single replica, distroless non-root image)
5. ClusterIP Service (port 8080)

### Verifying the Deployment

```bash
# Check the pod is running
kubectl get pods -n tentacular-system

# Check logs
kubectl logs -n tentacular-system -l app.kubernetes.io/name=tentacular-mcp

# Port-forward and test
kubectl port-forward -n tentacular-system svc/tentacular-mcp 8080:8080 &
curl http://localhost:8080/healthz
```

### Rollback

```bash
kubectl rollout undo deployment/tentacular-mcp -n tentacular-system
```

Or scale to zero:

```bash
kubectl scale deployment/tentacular-mcp -n tentacular-system --replicas=0
```

No persistent state to clean up -- all state lives in Kubernetes objects.

## Development

### Building

```bash
make build         # Build binary to bin/tentacular-mcp
make docker-build  # Build Docker image
make lint          # Run golangci-lint and go vet
make clean         # Remove build artifacts
```

### Testing

Tests are organized in 4 tiers:

| Tier | Command | Requirements |
|------|---------|-------------|
| Unit | `make test-unit` | No cluster needed |
| Integration | `make test-integration` | kind cluster (auto-provisioned) |
| E2E | `make test-e2e` | Production k0s cluster; set `TENTACULAR_E2E_KUBECONFIG` |
| All | `make test-all` | Runs all tiers sequentially |

```bash
# Unit tests only (default)
make test

# Integration tests (sets up and tears down a kind cluster)
make test-integration

# E2E tests (requires a real cluster)
TENTACULAR_E2E_KUBECONFIG=/path/to/kubeconfig make test-e2e
```

### Project Structure

```
cmd/tentacular-mcp/main.go   Entry point with graceful shutdown
pkg/auth/                     Bearer token middleware
pkg/guard/                    Self-protection namespace guard
pkg/k8s/                      Kubernetes client and operations
pkg/proxy/                    Module proxy reconciler and manifests
pkg/server/                   MCP server setup and HTTP handler
pkg/tools/                    28 MCP tool handlers (one file per group)
deploy/manifests/             Kustomize-based deployment manifests
test/integration/             Integration tests (kind cluster)
test/e2e/                     E2E tests (production cluster)
```

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `LISTEN_ADDR` | `:8080` | Address and port the HTTP server binds to |
| `TENTACULAR_MCP_TOKEN` | (required) | Bearer auth token for client authentication |
| `TENTACULAR_MCP_NAMESPACE` | `tentacular-system` | Namespace the MCP server is installed in |

## Security Model

### Pod Security Admission (PSA)

All namespaces created by `ns_create` are labeled with the `restricted` PSA profile:
- `pod-security.kubernetes.io/enforce: restricted`
- `pod-security.kubernetes.io/enforce-version: latest`

### Network Policies

Every created namespace gets:
- A **default-deny** NetworkPolicy blocking all ingress and egress
- A **DNS-allow** NetworkPolicy permitting UDP/TCP egress on port 53 to kube-system/kube-dns

### RBAC Scoping

The server's ClusterRole is scoped to exactly the verbs and resources needed by the 25 tools. It is significantly narrower than `cluster-admin`. Key constraints:
- Read-only access to nodes, storage classes, runtime classes, CRDs
- Create/delete for pods (gVisor verification only)
- Namespaced CRUD for resources managed by tool handlers
- `selfsubjectaccessreviews` for preflight RBAC validation

### Self-Protection

`guard.CheckNamespace()` runs before every namespace-scoped tool. It rejects any operation targeting the `tentacular-system` namespace, preventing the server from modifying its own deployment.

### Container Security

The Deployment runs with:
- `runAsNonRoot: true` (UID 65534)
- `readOnlyRootFilesystem: true`
- `allowPrivilegeEscalation: false`
- All capabilities dropped
- `RuntimeDefault` seccomp profile
- Distroless base image (`gcr.io/distroless/static-debian12:nonroot`)

## CLI Integration

The tentacular CLI (`tntc`) routes all cluster operations
through this MCP server. After `tntc cluster install`
bootstraps the server, the MCP endpoint and token are
saved to `~/.tentacular/config.yaml`. All subsequent
CLI commands automatically use the MCP server -- no
per-command flags needed.

`tntc cluster install` is the **only** CLI command that
communicates directly with the Kubernetes API. All other
cluster-facing commands (deploy, run, list, status, logs,
undeploy, audit, cluster check) route through MCP.

### MCP Tools Used by CLI

| CLI Command | MCP Tool(s) |
|-------------|-------------|
| `cluster check` | `cluster_preflight` |
| `deploy` | `wf_apply`, `ns_create` |
| `run` | `wf_run` |
| `list` | `wf_list` |
| `status` | `wf_status` |
| `logs` | `wf_logs` |
| `undeploy` | `wf_remove` |
| `audit` | `audit_rbac`, `audit_netpol`, `audit_psa` |

For the original design document, see
[docs/cli-integration.md](docs/cli-integration.md).

## Contributing

1. Follow the existing code patterns -- tool handlers are standalone functions that take `*k8s.Client` and return structured results
2. Add new tools in `pkg/tools/` following the one-file-per-group convention
3. Register tools through `pkg/tools/register.go` -- the wrapper handles JSON unmarshaling, guard checks, and MCP protocol concerns
4. Write unit tests alongside your code; add integration tests for K8s interactions
5. Run `make lint` before submitting changes
6. Use conventional commits for all commit messages

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
