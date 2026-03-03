## Why

The namespace guard only blocked `tentacular-system` by name, allowing write operations to target Kubernetes system namespaces (`kube-system`, `default`, etc.) and arbitrary unmanaged namespaces. This creates a blast-radius risk where the MCP server could modify cluster infrastructure it has no business touching.

## What Changes

- Expand the static namespace blocklist from `tentacular-system` alone to the full set of Kubernetes system namespaces: `kube-system`, `kube-public`, `kube-node-lease`, `default`, and `tentacular-system`
- Add `k8s.CheckManagedNamespace()` — a K8s API check that verifies the `app.kubernetes.io/managed-by: tentacular` label before allowing any write operation
- Apply `CheckManagedNamespace` to all write tools that previously lacked it: `cred_issue_token`, `cred_kubeconfig`, `cred_rotate`, `gvisor_verify`, `module_remove`, `module_status`
- Refactor existing inline `IsManagedNamespace` checks in `gvisor_annotate_ns` and `module_apply` to use the new shared helper
- Document namespace adoption: adding `app.kubernetes.io/managed-by=tentacular` label to a pre-existing namespace makes it visible to all tentacular tools

## Capabilities

### New Capabilities

None — no new tools introduced.

### Modified Capabilities

- `namespace-lifecycle`: Guard now blocks 5 system namespaces (not just `tentacular-system`); adoption via `kubectl label` documented as the mechanism to bring pre-existing namespaces under management
- `credential-management`: All three credential tools now require managed namespace before issuing tokens or rotating SAs
- `gvisor-sandbox`: `gvisor_annotate_ns` and `gvisor_verify` now consistently enforce managed-namespace check via shared helper
- `module-proxy`: `module_apply`, `module_remove`, and `module_status` all enforce managed-namespace check
- `security-audit`: Guard references updated to reflect full system namespace blocklist
- `workflow-introspection`: Guard references updated to reflect full system namespace blocklist
- `cluster-ops`: Guard references updated to reflect full system namespace blocklist
- `cluster-health`: Guard references updated to reflect full system namespace blocklist

## Impact

- `pkg/guard/guard.go`: blocklist expanded
- `pkg/k8s/namespace.go`: `CheckManagedNamespace()` added
- `pkg/tools/credential.go`, `gvisor.go`, `module.go`: managed-namespace checks added/consolidated
- `pkg/guard/guard_test.go`, `pkg/tools/credential_test.go`, `pkg/tools/module_test.go`: tests updated
- All 8 main specs updated with correct guard language and purpose sections
- No API changes; no new tools; no breaking changes for callers targeting managed namespaces
