# Design: Replace trigger pods and CronJobs with direct HTTP and internal scheduler

## wf_run: Direct HTTP

`wf_run` triggers a deployed workflow by POSTing directly to the workflow's
ClusterIP service:

```
POST http://{svc}.{ns}.svc.cluster.local:8080/run
```

The MCP server connects directly to the workflow Service's port 8080 via
standard in-cluster HTTP. No K8s API service proxy is involved. The workflow
engine receives a standard HTTP POST to `/run` and returns JSON.

Input payload is passed as the POST body. Output is the parsed JSON response
body from the workflow engine.

The `services/proxy` RBAC permission is no longer needed in the MCP server
ClusterRole. Direct HTTP uses regular pod-to-service networking.

### Timeout Handling

`wf_run` accepts a `timeout_seconds` parameter (default 120, max 600). The HTTP
client is configured with this timeout. If the workflow exceeds the timeout, the
HTTP call returns a context deadline exceeded error, which is surfaced to the
MCP client as a tool error.

### Error Handling

HTTP 200 responses: body is parsed as JSON and returned as `output`.

HTTP 500 responses: body is captured and returned in the error message. This
surfaces workflow execution errors (e.g., node threw, DAG failed) to the MCP
client instead of silently swallowing them.

Non-200, non-500 responses: status code and body are included in the error.

## Cron Scheduler: robfig/cron/v3

The internal cron scheduler uses `robfig/cron/v3`. It is started by `cmd/tentacular-mcp/main.go`
and stopped on graceful shutdown.

On startup, the scheduler calls `SyncWorkflowCrons()`, which:
1. Lists all Deployments in all tentacular-managed namespaces.
2. Filters for Deployments with the `tentacular.dev/cron-schedule` annotation.
3. Parses the annotation value (single cron string, JSON array of strings, or
   JSON array of objects with `schedule` and `name` fields).
4. Registers a `cron.Entry` for each schedule, calling the internal `wf_run`
   logic on each fire.

`SyncWorkflowCrons()` is also called after `wf_apply` and `wf_remove` to keep
the scheduler in sync with the cluster state.

### Cron Annotation Format

Single schedule (no named trigger):
```
tentacular.dev/cron-schedule: "0 9 * * *"
```

Multiple schedules (unnamed):
```
tentacular.dev/cron-schedule: '["0 9 * * *","0 * * * *"]'
```

Named schedules (send `{"trigger": "<name>"}` as input to `/run`):
```
tentacular.dev/cron-schedule: '[{"schedule":"0 9 * * *","name":"daily"},{"schedule":"0 * * * *","name":"hourly"}]'
```

## Module Pre-Warm: Background Goroutine in wf_apply

After applying manifests, `wf_apply` launches a background goroutine that:

1. Finds the ConfigMap manifest in the applied set (kind: ConfigMap, name matches
   `{name}-code` pattern).
2. Reads the `workflow.yaml` key to extract node file references.
3. Reads each node file key (e.g., `nodes__fetch.ts`) for `jsr:` and `npm:` import
   specifiers.
4. For each specifier, calls the esm.sh module proxy via direct HTTP
   to warm the cache: `GET http://esm-sh-proxy.tentacular-support.svc.cluster.local:8080/{specifier}`
5. Logs warnings on failure (non-fatal; pod K8s restart policy handles recovery).

`wf_apply` returns to the MCP client immediately. Warming happens in the background.

### Pre-Warm Race Condition

**This is a known, documented, and accepted race condition.**

Timeline of events after `wf_apply` returns:

```
t=0ms    wf_apply returns, warming goroutine starts
t=0-N ms K8s schedules pod, pulls image (if needed), starts container
t=0-M ms warming goroutine fetches modules from esm.sh proxy
```

If the pod starts (t=N) before warming completes (t=M), the Deno engine will
attempt to fetch uncached modules at startup. The esm.sh proxy will need to fetch
from the upstream JSR/npm registry, which may take longer than the engine's
startup timeout. The pod will fail with a module resolution error.

**Recovery path:**
- K8s restart policy (`Always`) retries the pod after a backoff (typically 10s).
- By the second attempt, warming will have completed (most modules warm in 2-5s).
- The second pod start succeeds.

**User-visible symptom:**
- `wf_pods` shows 1 restart on the workflow pod after a fresh deploy.
- `wf_logs` on the failed container shows a Deno module resolution error.
- This is **expected and normal** for first-time deploys with new module dependencies.
- By the second pod start, the workflow is running correctly.

**This does not occur on:**
- Redeployments of the same workflow version (modules already cached).
- Workflows with no JSR/npm dependencies (only use built-in Deno APIs).
- Clusters where esm.sh has already cached the required modules from previous runs.

## Output Schema: `any` instead of `json.RawMessage`

The MCP SDK (`mark3labs/mcp-go`) JSON-encodes tool results before returning them
to the client. If the result contains a `json.RawMessage`, the SDK
double-encodes it: the raw bytes are treated as a `[]byte` and base64-encoded
in the JSON output, producing a string like `"eyJvdXRwdXQiOi4uLn0="` instead of
the structured JSON object.

Using `any` (or `interface{}`) as the output type for workflow execution results
avoids this. The MCP SDK marshals `any` values directly as JSON, preserving the
structure of the workflow output.

## Namespace Convention

The three tentacular namespaces serve distinct roles:

| Namespace | Purpose |
|-----------|---------|
| `tentacular-system` | MCP server and secure control plane. Protected by `guard.CheckNamespace()` -- no workflow tools can operate on this namespace. |
| `tentacular-support` | Secure support systems. Currently hosts the esm.sh module proxy. Protected from workflow namespace operations. |
| All others with `app.kubernetes.io/managed-by=tentacular` label | Workflow namespaces. Created and managed by `ns_create`. Subject to workflow tool operations. |

This separation ensures the control plane cannot be accidentally modified or
damaged by workflow operations.
