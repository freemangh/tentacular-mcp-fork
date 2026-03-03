## Context

Tentacular CLI users currently hold broad admin kubeconfig access on their workstations to manage workflow namespaces, credentials, and cluster operations. This is a security anti-pattern. The tentacular-mcp server replaces direct kube-api access with a single in-cluster MCP endpoint that proxies Kubernetes operations through a scoped ServiceAccount.

The scaffold is already built:
- `cmd/tentacular-mcp/main.go` -- entry point with graceful shutdown
- `pkg/server/server.go` -- MCP server with StreamableHTTP at `/mcp`, health at `/healthz`
- `pkg/auth/auth.go` -- Bearer token middleware (loads from mounted Secret, bypasses `/healthz`)
- `pkg/guard/guard.go` -- `CheckNamespace()` rejects operations on `tentacular-system`
- `pkg/k8s/client.go` -- in-cluster `Client` wrapping typed + dynamic clientsets
- `pkg/k8s/` -- namespace CRUD, RBAC scaffolding, tokens/kubeconfig, netpol, quota/limitrange, cluster profile, preflight checks
- `deploy/manifests/` -- Namespace, ServiceAccount, ClusterRole, ClusterRoleBinding, Secret, Deployment, Service, Kustomization

What remains: 24 MCP tool handlers in a new `pkg/tools/` package (one file per group), wired into `server.registerTools()`.

## Goals / Non-Goals

**Goals:**
- Expose 24 MCP tools across 8 groups via Streamable HTTP, authenticated by Bearer token
- All namespace-scoped tools enforce `guard.CheckNamespace()` before any K8s API call
- Namespace creation produces a fully hardened namespace: PSA restricted, default-deny netpol, DNS-allow netpol, ResourceQuota, LimitRange, workflow SA/Role/RoleBinding
- Short-lived tokens via TokenRequest API with configurable TTL (10-1440 min)
- Module apply/remove uses the dynamic client with release-label tracking for garbage collection
- gVisor sandbox verification through ephemeral pod lifecycle (create, wait, read logs, delete)
- All tools return structured JSON in MCP `Content` blocks

**Non-Goals:**
- Multi-tenancy or per-user identity propagation (single shared Bearer token for this iteration)
- Ingress or TLS termination (CLI uses port-forward or in-cluster DNS)
- Helm chart rendering or template engine (module_apply takes pre-rendered manifests)
- Persistent state or database (all state lives in Kubernetes objects)
- Streaming/SSE log tailing (wf_logs returns a snapshot of tail lines)
- Watch-based real-time streaming to clients (all tools are request-response); however, the ClusterRole grants `watch` on pods and events so server-side code can use short-lived watches internally (e.g., waiting for pod phase transitions in `gvisor_verify`)

## Decisions

### D1: Tool package structure -- one file per group under `pkg/tools/`

Each of the 8 tool groups gets its own file (`namespace.go`, `credential.go`, `workflow.go`, `clusterops.go`, `gvisor.go`, `module.go`, `health.go`, `audit.go`) plus a shared `register.go` that exports a single `RegisterAll(srv, client)` function. This keeps tool handlers close to their domain while giving `server.go` a single call site.

**Alternative considered**: One monolithic `tools.go` file. Rejected because 24 handlers in one file hurts readability and makes parallel development harder.

**Alternative considered**: Tools as methods on `Server`. Rejected because it couples tool logic to the server struct and makes unit testing harder -- standalone functions that take `*k8s.Client` are easier to test with fake clientsets.

### D2: Tool handler signature -- `func(ctx, *k8s.Client, params) (any, error)`

Each tool handler is a pure function: `func handleNsCreate(ctx context.Context, client *k8s.Client, params NsCreateParams) (NsCreateResult, error)`. The registration layer in `register.go` handles MCP schema declaration, JSON unmarshaling of params, `guard.CheckNamespace()` calls, and marshaling the result into `mcp.Content`. This separates MCP protocol concerns from K8s business logic.

### D3: Guard enforcement at the registration layer, not inside handlers

The `register.go` wrapper checks `guard.CheckNamespace(params.Namespace)` for every tool that accepts a namespace parameter before calling the handler. This guarantees protection cannot be accidentally omitted from a handler. Tools without a namespace parameter (e.g., `health_nodes`, `health_cluster_summary`, `gvisor_check`) skip the guard check.

### D4: Namespace creation as an orchestrated sequence, not a transaction

`ns_create` calls the existing `pkg/k8s` functions in order: `CreateNamespace` -> `CreateDefaultDenyPolicy` -> `CreateDNSAllowPolicy` -> `CreateResourceQuota` -> `CreateLimitRange` -> `CreateWorkflowServiceAccount` -> `CreateWorkflowRole` -> `CreateWorkflowRoleBinding`. If any step fails after namespace creation, the handler returns an error describing which step failed. The namespace will exist in a partially-configured state. This is acceptable because:
- `ns_delete` can clean up the partial namespace (K8s garbage collection handles child resources)
- A retry of `ns_create` will fail with "namespace already exists", which is a clear signal to delete-and-retry
- Adding rollback logic would significantly increase complexity for an unlikely failure mode

**Alternative considered**: Wrap in a rollback sequence that deletes the namespace on any sub-resource failure. Rejected because partial failures are rare (RBAC is validated at startup via preflight) and automatic rollback can mask the real error.

### D5: Module apply/remove via dynamic client with release labels

`module_apply` uses the dynamic client to apply arbitrary unstructured resources. Each resource gets labeled `tentacular.io/release: <name>`. On update, any existing resources with the release label that are NOT in the new manifest set are deleted (garbage collection). `module_remove` deletes everything with the release label.

The dynamic client is used because module manifests can contain any K8s resource type. The GVR (Group/Version/Resource) is derived from each manifest's `apiVersion` and `kind` using the discovery API's RESTMapper.

**Alternative considered**: Typed client with a fixed set of supported resource types. Rejected because it would limit what modules can deploy and require code changes for each new resource type.

### D6: gVisor verification via ephemeral pod

`gvisor_verify` creates a pod with `runtimeClassName: <gvisor-class>`, runs `dmesg | head -1`, reads the logs, checks for "gVisor" or "runsc" in the output, and deletes the pod. The pod uses `busybox:latest` with a 60-second timeout. Cleanup happens in a `defer` block (best-effort) regardless of success or failure.

### D7: Token issuance flow

```
CLI -> Bearer token -> tentacular-mcp -> TokenRequest API -> JWT for tentacular-workflow SA
```

1. CLI authenticates to tentacular-mcp with the shared Bearer token (from mounted Secret)
2. `cred_issue_token` validates TTL (10-1440 min), calls `k8s.IssueToken()` which uses the TokenRequest API
3. `cred_kubeconfig` calls `cred_issue_token` internally, then wraps the token with cluster CA and API server URL from `rest.Config`
4. `cred_rotate` calls `k8s.RecreateWorkflowServiceAccount()` which invalidates all prior tokens

The API server URL and CA certificate are read from the in-cluster `rest.Config` -- no configuration needed.

### D8: ClusterRole RBAC scope

The server's ClusterRole (in `deploy/manifests/serviceaccount.yaml`) is scoped to exactly the verbs and resources needed. The design is explicitly NOT cluster-admin -- every rule is justified by a specific tool. Key additions needed beyond the current manifest:

- `rbac.authorization.k8s.io` / `clusterroles` / `get,list` -- for `audit_rbac` to inspect ClusterRoles referenced by ClusterRoleBindings
- `rbac.authorization.k8s.io` / `clusterrolebindings` / `get,list` -- for `audit_rbac` to find ClusterRoleBindings targeting namespace SAs
- `""` / `namespaces` / `patch` -- for `gvisor_annotate_ns` to add annotations
- `""` / `pods` / `create,delete` -- for `gvisor_verify` ephemeral pod
- `""` / `serviceaccounts` / `patch,update` -- for writing `imagePullSecrets` on workflow SAs
- `""` / `pods,events` / `watch` -- for streaming pod status and event tailing in `wf_pods` and `wf_events`
- `networking.k8s.io` / `ingresses` / `get,list` -- for `cluster_profile` to inventory ingress resources at cluster scope
- Dynamic client resources for `module_apply` -- requires broad `create,update,delete,patch,get,list,watch` on the resource types modules can contain (deployments, services, configmaps, secrets, jobs, cronjobs, networkpolicies, ingresses). The existing ClusterRole already covers `get,list` for most of these; `create,update,delete,patch` verbs need to be added for: `apps/deployments`, `""/services,configmaps,secrets`, `batch/cronjobs,jobs`, `networking.k8s.io/networkpolicies,ingresses`
- `patch` verb on all namespace-scoped workflow Role mutable resources -- the workflow Role grants `create,update,delete,get,list` on deployments, services, configmaps, secrets, cronjobs, jobs, networkpolicies, and ingresses but is missing `patch`, which is needed for incremental updates (e.g., strategic merge patches via `kubectl apply`). Adding `patch` to all mutable rules in the workflow Role.
- `watch` verb added to all resource rules (both read-only and mutable) -- enables event streaming for `wf_events`, `wf_pods`, `workflow_status`, and future Tasks support

**Broader profiling resources** -- `cluster_profile` scans cluster topology and workload distribution. The ClusterRole needs read-only access (`get,list,watch`) to:
- `apps` / `replicasets,daemonsets,statefulsets` -- workload topology
- `""` / `persistentvolumes,persistentvolumeclaims` -- storage posture
- `""` / `endpoints` -- service mesh and connectivity mapping
- `discovery.k8s.io` / `endpointslices` -- modern endpoint discovery
- `storage.k8s.io` / `volumeattachments` -- volume binding status

These are all read-only and do not grant any mutating access beyond what is already scoped to managed namespaces.

### D9: Error handling pattern

All tool handlers return `(ResultType, error)`. The registration wrapper converts errors to MCP error responses with `isError: true`. Errors use `fmt.Errorf` with `%w` wrapping for context chain. K8s API errors preserve the original `apierrors` type so callers can check `IsNotFound`, `IsAlreadyExists`, etc.

Structured error responses include:
- Guard violations: "operations on namespace \"tentacular-system\" are not allowed"
- Validation errors: "TTL must be between 10 and 1440 minutes"
- Not found: "namespace \"foo\" not found"
- Already exists: "namespace \"foo\" already exists"
- Partial failures: "namespace created but failed to create resource quota: ..."

### D10: Health and cluster summary tools are cluster-scoped (no namespace guard)

`health_nodes`, `health_cluster_summary`, and `gvisor_check` operate at cluster scope. They do not take a namespace parameter and skip the guard check. This is by design -- these tools report cluster-wide state needed for capacity planning and diagnostics.

## Architecture

### Component Diagram

```
+------------------+        +----------------------------+
|  tentacular CLI  |        |   tentacular-system ns     |
|  (or any MCP     | Bearer |                            |
|   client)        +------->+  tentacular-mcp Deployment |
|                  |  :8080 |  +----------------------+  |
+------------------+  /mcp  |  | auth.Middleware       |  |
                            |  |   |                   |  |
                            |  | server.Handler        |  |
                            |  |   |                   |  |
                            |  | pkg/tools/register.go |  |
                            |  |   |  guard.Check      |  |
                            |  |   |  unmarshal params  |  |
                            |  |   |  call handler      |  |
                            |  |   |  marshal result    |  |
                            |  |   v                   |  |
                            |  | pkg/tools/*.go        |  |
                            |  |   |                   |  |
                            |  |   v                   |  |
                            |  | pkg/k8s/*             |  |
                            |  |   |                   |  |
                            |  +---+-------------------+  |
                            |      |                      |
                            +------+----------------------+
                                   |
                                   v
                            +------+----------------------+
                            |  Kubernetes API Server      |
                            +-----------------------------+
```

### Request Flow

1. HTTP request arrives at `:8080/mcp` with `Authorization: Bearer <token>`
2. `auth.Middleware` validates the token; rejects with 401 if invalid
3. MCP SDK `StreamableHTTPHandler` parses the MCP message, routes to the registered tool
4. `register.go` wrapper: unmarshal params -> `guard.CheckNamespace()` -> call handler -> marshal result
5. Handler calls `pkg/k8s` functions which use the in-cluster `rest.Config`
6. Result returned as MCP `Content` with `type: "text"` containing JSON

## API Contracts

### Group 1: Namespace Lifecycle

| Tool | Input | Output |
|------|-------|--------|
| `ns_create` | `{name: string, quota_preset: "small"\|"medium"\|"large"}` | `{name, status, quota_preset, resources_created: [string]}` |
| `ns_delete` | `{name: string}` | `{name, deleted: true}` |
| `ns_get` | `{name: string}` | `{name, labels, annotations, status, managed, quota: QuotaSummary, limitRange: LimitRangeSummary}` |
| `ns_list` | `{}` | `{namespaces: [{name, status, created_at, quota_preset}]}` |

### Group 2: Credential Management

| Tool | Input | Output |
|------|-------|--------|
| `cred_issue_token` | `{namespace: string, ttl_minutes: int}` | `{token: string, expires_at: string}` |
| `cred_kubeconfig` | `{namespace: string, ttl_minutes: int}` | `{kubeconfig: string}` |
| `cred_rotate` | `{namespace: string}` | `{namespace, rotated: true, message: string}` |

### Group 3: Workflow Introspection

| Tool | Input | Output |
|------|-------|--------|
| `wf_pods` | `{namespace: string}` | `{pods: [{name, phase, ready, restarts, images: [string], age: string}]}` |
| `wf_logs` | `{namespace: string, pod: string, container?: string, tail_lines?: int}` | `{pod, container, lines: [string]}` |
| `wf_events` | `{namespace: string, limit?: int}` | `{events: [{type, reason, message, object, count, last_seen: string}]}` |
| `wf_jobs` | `{namespace: string}` | `{jobs: [{name, status, start, completion, duration}], cronjobs: [{name, schedule, last_scheduled, active, suspended}]}` |

### Group 4: Cluster Operations

| Tool | Input | Output |
|------|-------|--------|
| `cluster_preflight` | `{namespace: string}` | `{checks: [{name, passed, warning?, remediation?}]}` |
| `cluster_profile` | `{namespace?: string}` | `ClusterProfile` (see `pkg/k8s/profile.go`) -- scans nodes, runtimeClasses, storageClasses, CSI drivers, CRDs, ingresses, replicasets, daemonsets, statefulsets, PVs, PVCs, endpoints, endpointslices, volumeattachments |

### Group 5: gVisor Sandbox

| Tool | Input | Output |
|------|-------|--------|
| `gvisor_check` | `{}` | `{available: bool, runtime_class?: string, handler?: string, guidance?: string}` |
| `gvisor_annotate_ns` | `{namespace: string}` | `{namespace, annotation: string, applied: true}` |
| `gvisor_verify` | `{namespace: string}` | `{verified: bool, output: string, runtime_class: string}` |

### Group 6: Module Proxy

| Tool | Input | Output |
|------|-------|--------|
| `module_apply` | `{namespace: string, release: string, manifests: [object]}` | `{release, namespace, created: int, updated: int, deleted: int}` |
| `module_remove` | `{namespace: string, release: string}` | `{release, namespace, deleted: int}` |
| `module_status` | `{namespace: string, release: string}` | `{release, namespace, resources: [{kind, name, ready, reason?}]}` |

### Group 7: Cluster Health

| Tool | Input | Output |
|------|-------|--------|
| `health_nodes` | `{}` | `{nodes: [{name, ready, cpu_capacity, mem_capacity, cpu_allocatable, mem_allocatable, kubelet_version, conditions: [{type, status}]}]}` |
| `health_ns_usage` | `{namespace: string}` | `{namespace, cpu_used, cpu_limit, mem_used, mem_limit, pod_count, pod_limit, cpu_pct, mem_pct, pod_pct}` |
| `health_cluster_summary` | `{}` | `{total_nodes, ready_nodes, total_pods, cpu_capacity, cpu_allocatable, cpu_requested, mem_capacity, mem_allocatable, mem_requested}` |

### Group 8: Security Audit

| Tool | Input | Output |
|------|-------|--------|
| `audit_rbac` | `{namespace: string}` | `{findings: [{role, rule, severity: "high"\|"medium"\|"low", reason}]}` |
| `audit_netpol` | `{namespace: string}` | `{default_deny: bool, policies: [{name, types, pod_selector}], findings: [{severity, message}]}` |
| `audit_psa` | `{namespace: string}` | `{compliant: bool, enforce, audit, warn, findings: [{severity, message}]}` |

## K8s Resources Created by `ns_create`

When `ns_create` is called with `name: "dev-alice"` and `quota_preset: "small"`:

1. **Namespace** `dev-alice` -- labels: `app.kubernetes.io/managed-by: tentacular`, `pod-security.kubernetes.io/enforce: restricted`, `pod-security.kubernetes.io/enforce-version: latest`
2. **NetworkPolicy** `default-deny` -- denies all ingress and egress
3. **NetworkPolicy** `allow-dns` -- allows UDP/TCP egress port 53 to kube-system/kube-dns
4. **ResourceQuota** `tentacular-quota` -- limits.cpu=2, limits.memory=2Gi, pods=10
5. **LimitRange** `tentacular-limits` -- default request: 100m CPU / 64Mi mem; default limit: 500m CPU / 256Mi mem
6. **ServiceAccount** `tentacular-workflow`
7. **Role** `tentacular-workflow` -- grants: apps/deployments (CRUD+patch), core/services+configmaps+secrets (CRUD+patch), batch/cronjobs+jobs (CRUD+patch), networking/networkpolicies+ingresses (CRUD+patch), core/pods+pods/log+events (get,list,watch), core/serviceaccounts (get,list,patch,update)
8. **RoleBinding** `tentacular-workflow` -- binds Role to ServiceAccount

## Code-to-Spec Mapping

| Existing Code | Spec Coverage | Notes |
|---|---|---|
| `pkg/k8s/namespace.go` | namespace-lifecycle (ns_create, ns_delete, ns_get, ns_list) | `CreateNamespace`, `DeleteNamespace`, `GetNamespace`, `ListManagedNamespaces`, `IsManagedNamespace` all exist |
| `pkg/k8s/rbac.go` | namespace-lifecycle (SA/Role/RoleBinding creation), credential-management (rotate) | `CreateWorkflowServiceAccount`, `CreateWorkflowRole`, `CreateWorkflowRoleBinding`, `RecreateWorkflowServiceAccount` all exist |
| `pkg/k8s/tokens.go` | credential-management (issue, kubeconfig) | `IssueToken`, `GenerateKubeconfig` exist |
| `pkg/k8s/netpol.go` | namespace-lifecycle (network policies) | `CreateDefaultDenyPolicy`, `CreateDNSAllowPolicy` exist |
| `pkg/k8s/quota.go` | namespace-lifecycle (quota, limitrange) | `CreateResourceQuota`, `CreateLimitRange` exist with small/medium/large presets |
| `pkg/k8s/profile.go` | cluster-ops (cluster_profile) | `ProfileCluster` exists with full implementation |
| `pkg/k8s/preflight.go` | cluster-ops (cluster_preflight) | `RunPreflightChecks` exists with API, namespace, RBAC, gVisor checks |
| `pkg/guard/guard.go` | All specs (self-protection) | `CheckNamespace` exists |
| `pkg/auth/auth.go` | All specs (authentication) | `LoadToken`, `Middleware` exist |
| `pkg/server/server.go` | MCP transport | Server struct, `Handler()`, `StreamableHTTPHandler` exist; `registerTools()` is empty stub |

**New code needed** (all in `pkg/tools/`):
- `register.go` -- `RegisterAll()` function, generic MCP tool registration wrapper
- `namespace.go` -- orchestrate existing `pkg/k8s` functions for ns_create, ns_delete, ns_get, ns_list
- `credential.go` -- thin wrappers calling `pkg/k8s` token/kubeconfig functions with TTL validation
- `workflow.go` -- new K8s API calls: list pods, get pod logs, list events, list jobs/cronjobs
- `clusterops.go` -- thin wrappers calling existing `pkg/k8s` preflight and profile functions
- `gvisor.go` -- new: check RuntimeClass, patch namespace annotation, create/wait/read/delete verification pod
- `module.go` -- new: dynamic client apply with release labeling, remove by label, status by label
- `health.go` -- new: node health aggregation, namespace resource usage vs quota, cluster resource summary
- `audit.go` -- new: RBAC rule scanning, netpol coverage analysis, PSA label compliance check

**ClusterRole additions needed** in `deploy/manifests/serviceaccount.yaml`:
- `rbac.authorization.k8s.io` / `clusterroles,clusterrolebindings` / `get,list`
- `""` / `namespaces` / `patch`
- `""` / `pods` / `create,delete`
- `""` / `serviceaccounts` / `patch,update` (for imagePullSecrets)
- `""` / `pods,events` / `watch` (for streaming)
- `networking.k8s.io` / `ingresses` / `get,list,watch,create,update,delete,patch` (profiling + module_apply)
- `apps` / `replicasets,daemonsets,statefulsets` / `get,list,watch` (profiling)
- `""` / `persistentvolumes,persistentvolumeclaims` / `get,list,watch` (profiling)
- `""` / `endpoints` / `get,list,watch` (profiling)
- `discovery.k8s.io` / `endpointslices` / `get,list,watch` (profiling)
- `storage.k8s.io` / `volumeattachments` / `get,list,watch` (profiling)
- Elevate `apps/deployments`, `""/services,configmaps,secrets`, `batch/cronjobs,jobs`, `networking.k8s.io/networkpolicies,ingresses` to include `create,update,delete,patch` verbs (for module_apply)
- Add `watch` verb to all existing read-only rules (nodes, storageclasses, csidrivers, runtimeclasses, CRDs, pods, events)

## Risks / Trade-offs

**Single Bearer token authentication** -- All CLI clients share one token. A leaked token grants full MCP access until rotated. Mitigation: token is mounted from a K8s Secret; rotate by updating the Secret and restarting the pod. Future iteration can add per-user OIDC.

**No rollback on partial ns_create failure** -- If namespace creation succeeds but a sub-resource fails, the namespace exists in partial state. Mitigation: `ns_delete` cleans up everything; the error message identifies the failed step.

**module_apply requires broad RBAC** -- The dynamic client needs create/update/delete on resource types that modules might contain. This is broader than other tool groups. Mitigation: operations are scoped to managed namespaces only; the guard prevents touching tentacular-system; the ClusterRole is still far narrower than cluster-admin.

**gVisor verification pod uses busybox:latest** -- The verification pod pulls a public image which may not be available in air-gapped clusters. Mitigation: the image can be overridden via a future configuration option; for now, document the requirement.

**60-second timeout for gVisor verification** -- If image pull is slow, the verification may time out. Mitigation: the timeout is generous for an already-pulled image; the error message explains the timeout and the pod is cleaned up.

**No rate limiting on MCP endpoint** -- A misbehaving client could flood the API server. Mitigation: single replica means one goroutine pool; K8s API server has its own rate limiting; add explicit rate limiting in a future iteration if needed.

## Migration Plan

1. Build the container image with all 24 tool handlers
2. Update the ClusterRole manifest with the additional RBAC rules identified in D8
3. Generate a fresh auth token: `openssl rand -hex 32` and update the Secret
4. `kubectl apply -k deploy/manifests/`
5. Verify: `kubectl port-forward -n tentacular-system svc/tentacular-mcp 8080:8080` then send an MCP initialize request

**Rollback**: `kubectl rollout undo deployment/tentacular-mcp -n tentacular-system` or scale to 0 replicas. No persistent state to clean up.

## Open Questions

1. Should `module_apply` support a dry-run mode (server-side dry-run) before actual apply?
2. Should `ns_create` accept custom labels/annotations beyond the defaults?
3. Should there be a configurable image for `gvisor_verify` instead of hardcoding `busybox:latest`?
4. Should `wf_logs` support `previous: true` for crashed container logs?
