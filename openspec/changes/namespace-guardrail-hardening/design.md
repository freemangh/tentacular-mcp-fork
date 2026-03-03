## Context

The guard system in tentacular-mcp has two layers:

1. **Static blocklist** (`pkg/guard/guard.go`): Pure string check, no K8s dependency. Runs synchronously before any tool handler. Previously only blocked `tentacular-system`.
2. **Managed-namespace check**: K8s API call to verify `app.kubernetes.io/managed-by: tentacular` label. Previously implemented inline in `handleModuleApply` and `handleGVisorAnnotateNs`, absent from all credential tools and `module_remove`/`module_status`/`gvisor_verify`.

All 25 tools call `guard.CheckNamespace()` for namespace-scoped operations. The managed-namespace check was inconsistent — some tools had it, most did not.

## Goals / Non-Goals

**Goals:**
- Block writes to all Kubernetes system namespaces, not just `tentacular-system`
- Enforce managed-namespace check consistently across all write operations
- Provide a clear, actionable error message that tells operators how to adopt a namespace
- Keep the static guard as a fast, dependency-free first check
- Consolidate the managed-namespace check into one reusable helper

**Non-Goals:**
- Adding managed-namespace checks to read-only tools (`wf_pods`, `audit_*`, `health_ns_usage`, etc.)
- Introducing a new `ns_adopt` MCP tool (noted as future work)
- Changing the K8s RBAC permissions of the server's ClusterRole

## Decisions

### Decision 1: Keep guard as pure static check; managed check in k8s package

**Choice**: `guard.CheckNamespace` stays dependency-free (no K8s client). `k8s.CheckManagedNamespace(ctx, client, name)` is the dynamic check.

**Rationale**: The guard package has no imports beyond `fmt`. Injecting a K8s client would create a circular dependency (`pkg/tools` → `pkg/guard` → `pkg/k8s` is fine, but it conflates two distinct concerns). The static check is fast and catches the most obvious misuse without a network round-trip. The managed check is a second gate.

**Alternative considered**: Merge both into a single `guard.CheckNamespace(ctx, client, ns)` call. Rejected because it forces all callers to provide a K8s client even when they only need static protection.

### Decision 2: Apply managed check to write tools only, not reads

**Choice**: Read-only tools (`wf_*`, `audit_*`, `health_ns_usage`, `ns_get`, `module_status` excepted below) do not require managed-by label.

**Rationale**: Read operations carry no mutation risk. Requiring managed-by on reads would prevent operators from inspecting a namespace before deciding whether to adopt it — a counterproductive UX. `module_status` is treated as a write-class operation because it is part of the apply/status/remove lifecycle and should be consistent with `module_apply` and `module_remove`.

**Alternative considered**: Require managed-by on all namespace-scoped operations. Rejected as overly restrictive for diagnostic use cases.

### Decision 3: Error message includes the adoption command

**Choice**: The error from `CheckManagedNamespace` reads:
> `namespace "X" is not managed by tentacular; add label app.kubernetes.io/managed-by=tentacular to adopt it`

**Rationale**: Operators hitting this error on a pre-existing namespace need to know exactly what to do. A self-documenting error eliminates the need to consult docs.

### Decision 4: No new `ns_adopt` MCP tool in this change

**Choice**: Adoption is a manual `kubectl label` operation for now.

**Rationale**: `ns_adopt` would need to reconcile potentially conflicting network policies, quotas, and RBAC — a non-trivial implementation requiring its own change. The label-based adoption is sufficient for immediate operational needs and unblocks the cluster where workflows were deployed before the MCP server.

## Risks / Trade-offs

- **Risk**: Operators with pre-existing tentacular-managed namespaces (created before this server was deployed) will see new errors on credential and module operations. → **Mitigation**: Clear error message with the exact `kubectl label` command; document in README.
- **Risk**: `default` namespace added to blocklist may surprise operators who put workloads in `default`. → **Mitigation**: `default` is already a security anti-pattern for workloads; the error message is explicit.
- **Trade-off**: `module_status` is treated as a write-class operation for consistency, even though it makes no mutations. This means `module_status` on an unmanaged namespace returns an error rather than just listing resources. Acceptable given the principle of least surprise within the module lifecycle.

## Migration Plan

Implementation is already complete. The change is backwards-compatible for all namespaces that carry the managed-by label. Namespaces created by `ns_create` are unaffected. Pre-existing namespaces require one `kubectl label` command per namespace to adopt.

No rollback complexity — reverting the guard change requires only removing the new entries from the `systemNamespaces` map and removing the `CheckManagedNamespace` calls.
