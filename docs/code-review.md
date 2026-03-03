# Code Review: tentacular-mcp

**Reviewer:** Code Reviewer
**Date:** 2026-02-24
**Scope:** All foundation packages (`pkg/k8s/`, `pkg/auth/`, `pkg/guard/`, `pkg/server/`), all tool handlers (`pkg/tools/`), deploy manifests, and `cmd/tentacular-mcp/main.go`

---

## Verification Pass (Phase 1.5)

**Date:** 2026-02-24
**Build:** `go build ./...` -- PASS
**Vet:** `go vet ./...` -- PASS
**Tests:** 73 tests passing across 7 packages
**Kustomize:** `kubectl kustomize deploy/manifests/` -- PASS

### Coverage

| Package | Coverage |
|---------|----------|
| pkg/auth | 100.0% |
| pkg/guard | 100.0% |
| pkg/server | 92.9% |
| pkg/tools | 41.3% |
| pkg/k8s | 27.0% |

### Findings Fixed (Verification Pass)

| ID | Status | Description |
|----|--------|-------------|
| C1 | FIXED | Workflow Role now has `patch`, `watch`, `serviceaccounts`, `ingresses` |
| H3 | FIXED | ClusterRole manifest expanded with all D8 resources |

---

## Phase 2 Review: All Tool Implementations

**Date:** 2026-02-24
**Build:** `go build ./...` -- PASS
**Vet:** `go vet ./...` -- PASS
**Tests:** All passing across 5 packages (auth, guard, k8s, server, tools)

### Critical Finding Status

| ID | Phase 1 | Phase 2 | Notes |
|----|---------|---------|-------|
| C1 | FIXED | FIXED | Workflow Role complete per design D8 + added read-only replicasets/daemonsets/statefulsets |
| C2 | OPEN | FIXED | Pod has full PSA restricted security context; command changed to `uname -r` (works without elevated privileges) |
| C3 | OPEN | FIXED | `allowedKinds` map restricts to safe resource types (Deployment, Service, PVC, NetworkPolicy, ConfigMap, Secret, Job, CronJob, Ingress) with clear error message on rejection |
| C4 | OPEN | FIXED | Now uses `crypto/subtle.ConstantTimeCompare` |

All 4 critical findings are resolved.

### Guard Enforcement Verification (All 24 Tools)

Every namespace-accepting tool correctly calls `guard.CheckNamespace()` before any K8s API call. Cluster-scoped tools correctly omit the guard per design D10.

| Tool | Namespace Param | Guard Check |
|------|----------------|-------------|
| ns_create | params.Name | YES |
| ns_delete | params.Name | YES |
| ns_get | params.Name | YES |
| ns_list | (none) | N/A - cluster-scoped |
| cred_issue_token | params.Namespace | YES |
| cred_kubeconfig | params.Namespace | YES |
| cred_rotate | params.Namespace | YES |
| wf_pods | params.Namespace | YES |
| wf_logs | params.Namespace | YES |
| wf_events | params.Namespace | YES |
| wf_jobs | params.Namespace | YES |
| cluster_preflight | params.Namespace | YES |
| cluster_profile | params.Namespace | YES (conditional on non-empty, correct per D10) |
| gvisor_check | (none) | N/A - cluster-scoped |
| gvisor_annotate_ns | params.Namespace | YES |
| gvisor_verify | params.Namespace | YES |
| wf_apply | params.Namespace | YES |
| wf_remove | params.Namespace | YES |
| wf_status | params.Namespace | YES |
| health_nodes | (none) | N/A - cluster-scoped |
| health_ns_usage | params.Namespace | YES |
| health_cluster_summary | (none) | N/A - cluster-scoped |
| audit_rbac | params.Namespace | YES |
| audit_netpol | params.Namespace | YES |
| audit_psa | params.Namespace | YES |

### Findings Still Open After Phase 2

| ID | Severity | Description |
|----|----------|-------------|
| H1 | High | health_ns_usage errors on missing quota (spec says return "unlimited") |
| H2 | High | NewClientFromConfig missing Dynamic client init |
| H4 | High | Logger param unused in register.go -- no structured logging in tool handlers |
| H5 | High | ns_list never populates quota_preset field |
| H6 | High | wf_apply Get error too broad -- treats all errors as "not found" (line 201-202) |
| H7 | High | audit_rbac flags tentacular's own managed role for secrets access |
| M1-M9 | Medium | See detailed findings below |
| M5+ | Medium | knownGVRs missing `ingresses` and `persistentvolumeclaims` in all 3 workflow functions -- GC, remove, and status will miss these resource types even though allowedKinds permits them |
| L1-L5 | Low | See detailed findings below |

### Phase 2 Summary

**Resolved:** All 4 critical findings (C1, C2, C3, C4).

**High-priority items remaining (5):**
- **H1** (`health.go:168-169`): Returns error on missing quota; spec says return "unlimited"
- **H2** (`client.go:60-65`): `NewClientFromConfig` missing Dynamic client -- nil-pointer risk for deploy tool tests
- **H4** (`register.go:12`): Logger accepted but unused -- no structured logging in any tool handler
- **H5** (`namespace.go:243-260`): `ns_list` never populates `quota_preset` -- field always empty
- **H6** (`deploy.go:201-202`): Get error handling treats any error as "not found" instead of checking `apierrors.IsNotFound` (affects wf_apply)

**Downgraded (1):**
- **H7** -> Medium: audit_rbac flagging tentacular's own role is annoying but not a functional bug. The audit results are still correct; they just include expected findings.

**New observation:**
- **M5 expanded**: `allowedKinds` now permits `Ingress` and `PersistentVolumeClaim`, but the `knownGVRs` lists in `handleWorkflowApply` (line 222-230), `handleWorkflowRemove` (line 262-270), and `handleWorkflowStatus` (line 299-307) still omit these types. This means Ingresses and PVCs deployed via wf_apply will not be garbage-collected on update, not removed by wf_remove, and not shown by wf_status.

**Total remaining:** 0 Critical, 5 High, 10 Medium, 5 Low

---

## Summary

The codebase is well-structured and follows the design doc closely. Guard enforcement is consistently applied at the registration layer. Error handling uses `%w` wrapping throughout. The tool handler separation from k8s business logic is clean.

**Phase 1 Counts:** 4 Critical, 7 High, 9 Medium, 5 Low
**Post-Phase 2 Remaining:** 0 Critical, 5 High, 10 Medium, 5 Low

---

## Critical Findings

### C1. `pkg/k8s/rbac.go:58-85` -- Workflow Role missing `patch` verb, `serviceaccounts`, `ingresses`, and `watch`

**Severity:** Critical
**Spec ref:** design.md section "K8s Resources Created by ns_create", item 7

The workflow Role is missing several permissions specified in the design:
- Missing `patch` verb on all resource rules (design specifies CRUD+patch)
- Missing `serviceaccounts` resource with `get,list,patch,update` verbs
- Missing `networking.k8s.io/ingresses` resource with CRUD+patch
- Missing `watch` verb on `pods`, `pods/log`, `events`

**Fix:** Update the `Rules` slice in `CreateWorkflowRole` to match the design spec exactly:
```go
// Add "patch" to all existing verb lists
// Add ingresses to networking.k8s.io rule
// Add serviceaccounts rule: get,list,patch,update
// Add watch to pods/pods-log/events rule
```

### C2. `pkg/tools/gvisor.go:164-183` -- gVisor verification pod does not comply with PSA restricted profile

**Severity:** Critical

The verification pod created by `handleGVisorVerify` will be rejected by PSA enforcement on restricted namespaces because it lacks the required security context fields:
- No `securityContext.runAsNonRoot: true`
- No `securityContext.seccompProfile`
- No `securityContext.capabilities.drop: ["ALL"]`
- No `securityContext.allowPrivilegeEscalation: false`

Since `ns_create` sets PSA enforce to `restricted`, any pod without these fields will be rejected by the admission controller.

**Fix:** Add a complete restricted-compliant security context to the pod spec and container spec. Note that `dmesg` requires elevated privileges, so either: (a) use a different verification command that works under restricted PSA (e.g., `uname -r` which shows "gvisor" in the kernel version), or (b) document that gvisor_verify only works in namespaces with baseline/privileged PSA.

### C3. `pkg/tools/deploy.go:138-200` -- wf_apply does not validate manifest content, enabling injection of cluster-scoped resources

**Severity:** Critical

`handleWorkflowApply` calls `obj.SetNamespace(params.Namespace)` but does not validate that the manifest Kind is namespace-scoped. A caller could pass cluster-scoped resources (e.g., ClusterRole, Namespace, Node) as manifests. While the dynamic client would attempt to create them namespace-scoped (which would fail for truly cluster-scoped resources), this is not explicitly validated and the error messages would be confusing.

More importantly, there is no validation of the `apiVersion`/`kind` against an allowlist. A malicious or buggy caller could attempt to create resources the ClusterRole grants access to at cluster scope.

**Fix:** Add an allowlist of permitted resource types for wf_apply (deployments, services, configmaps, secrets, jobs, cronjobs, networkpolicies, ingresses) matching the knownGVRs list. Reject manifests with kinds not in the allowlist.

### C4. `pkg/auth/auth.go:44` -- Token comparison is not constant-time

**Severity:** Critical

The token comparison `provided != token` uses standard string comparison, which is vulnerable to timing attacks. An attacker could potentially determine the token character-by-character by measuring response times.

**Fix:** Use `crypto/subtle.ConstantTimeCompare([]byte(provided), []byte(token))` instead.

---

## High Findings

### H1. `pkg/tools/health.go:168-169` -- health_ns_usage returns error when no quota exists instead of graceful handling

**Severity:** High
**Spec ref:** cluster-health spec, "Namespace without quota" scenario

The spec says: "The system returns usage figures with limits shown as 'unlimited'." But the code returns an error: `"no ResourceQuota found in namespace %q"`.

**Fix:** When no quota is found, return a result with "unlimited" for limit fields and 0% utilization, instead of erroring.

### H2. `pkg/k8s/client.go:60-65` -- `NewClientFromConfig` does not initialize Dynamic client

**Severity:** High

`NewClientFromConfig` creates a Client without a Dynamic client. Any test or code path that uses this constructor and then calls module tools will nil-pointer panic on `client.Dynamic`.

**Fix:** Either accept a `dynamic.Interface` parameter, or create one from the config. For test usage, accept both typed and dynamic interfaces.

### H3. `deploy/manifests/serviceaccount.yaml` -- ClusterRole missing resources needed by design D8

**Severity:** High
**Spec ref:** design.md section D8

Several resources identified in D8 are missing from the ClusterRole:
- Missing `networking.k8s.io/ingresses` with `create,update,delete,get,list` (needed by wf_apply and cluster_profile)
- Missing `""` / `serviceaccounts` / `patch,update` verbs (only has create,get,list,delete)
- Missing `""` / `pods,events` / `watch` verb
- Missing `apps/replicasets,daemonsets,statefulsets` with `get,list` (needed by cluster_profile per D8)
- Missing `""` / `persistentvolumes,persistentvolumeclaims` with `get,list`
- Missing `""` / `endpoints` with `get,list`
- Missing `discovery.k8s.io/endpointslices` with `get,list`
- Missing `storage.k8s.io/volumeattachments` with `get,list`

**Fix:** Add the missing rules to the ClusterRole. The broader profiling resources are read-only and safe to add.

### H4. `pkg/tools/register.go` -- logger parameter accepted but never used or passed to tool handlers

**Severity:** High

`RegisterAll` accepts a `*slog.Logger` parameter but never passes it to any registration function. None of the tool handlers have access to structured logging. This means errors during tool execution (especially partial failures in ns_create, GC errors in wf_apply) are silent.

**Fix:** Either pass the logger to each `register*Tools` function and use it for non-fatal error logging, or remove the parameter to avoid confusion.

### H5. `pkg/tools/namespace.go:250-258` -- ns_list does not return quota_preset

**Severity:** High
**Spec ref:** design.md API contract for ns_list

The API contract specifies `quota_preset` in the ns_list output, but the implementation never populates it. The field exists on `NsListItem` but is always empty because there is no way to derive the preset from the stored ResourceQuota.

**Fix:** Either store the quota_preset as a label/annotation on the namespace during ns_create (so ns_list can read it back), or derive it by comparing quota values to known presets.

### H6. `pkg/tools/deploy.go:182-199` -- wf_apply error handling on Get is too broad

**Severity:** High

When checking if a resource exists (line 183), any error (including network errors, permission errors, etc.) is treated as "resource doesn't exist" and triggers a Create. This could lead to duplicate resources or confusing errors.

**Fix:** Check specifically for `apierrors.IsNotFound(err)` before falling through to Create. For other errors, return the error.

### H7. `pkg/tools/audit.go:155-163` -- audit_rbac flags tentacular's own managed role rules as findings

**Severity:** High

The `secrets` resource appears in the workflow Role rules (line 66 of rbac.go). The audit_rbac handler will flag this as a "medium" finding for "access to sensitive resource 'secrets'" every time, creating noise. The tentacular-managed workflow role is expected to have secrets access.

**Fix:** Either skip findings for roles labeled with `app.kubernetes.io/managed-by: tentacular`, or document this as expected behavior in the finding.

---

## Medium Findings

### M1. `pkg/tools/credential.go:112-114` -- kubeconfig generation will produce empty CA when CAData is not in rest.Config

**Severity:** Medium

In-cluster configs may use `CAFile` instead of `CAData`. The code only reads `client.Config.CAData`. If the in-cluster config uses a CA file path (as is common), the kubeconfig will have an empty CA, making it unusable.

**Fix:** Read `CAData` first; if empty, read the file at `CAFile` using `os.ReadFile`.

### M2. `pkg/tools/gvisor.go:162` -- Predictable pod name using UnixNano modulo

**Severity:** Medium

`podName := fmt.Sprintf("gvisor-verify-%d", time.Now().UnixNano()%100000)` produces a somewhat predictable name with only 100K possibilities. While not a direct security issue, concurrent gvisor_verify calls could collide.

**Fix:** Use `crypto/rand` or a UUID for the pod name suffix.

### M3. `pkg/tools/gvisor.go:199-209` -- Busy-wait polling loop without context cancellation check

**Severity:** Medium

The polling loop for pod completion checks `time.Now().Before(deadline)` but does not check if the context has been cancelled. If the MCP request is cancelled, the handler will continue polling for up to 60 seconds.

**Fix:** Add a `select` with `ctx.Done()` or check `ctx.Err()` at the top of each loop iteration.

### M4. `pkg/tools/workflow.go:192-236` -- wf_logs reads entire log stream into memory

**Severity:** Medium

`io.ReadAll(stream)` reads all log data into memory. For pods with very large logs, this could cause OOM. Even though `TailLines` limits the output, a single very long log line could still be large.

**Fix:** Add a `LimitReader` wrapper around the stream (e.g., 10MB max) to prevent unbounded memory usage.

### M5. `pkg/tools/deploy.go:204-212` -- knownGVRs for garbage collection is hardcoded and incomplete

**Severity:** Medium

The `knownGVRs` list used for garbage collection is hardcoded and does not include `ingresses`. If a workflow deploys an Ingress resource, it will never be garbage-collected on update.

**Fix:** Add `{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}` to the knownGVRs lists in all three functions (handleWorkflowApply, handleWorkflowRemove, handleWorkflowStatus).

### M6. `pkg/tools/deploy.go:304` -- wf_status uses `strings.ToTitle` incorrectly for Kind display

**Severity:** Medium

`strings.ToTitle(gvr.Resource[:1]) + gvr.Resource[1:]` converts the first character to title case, but `strings.ToTitle` converts ALL characters to title case (it is not the same as `strings.Title`). For a single character this works, but it's semantically wrong and could break for multi-byte first characters.

More importantly, the Kind should be derived from the resource's actual Kind field (e.g., "Deployment" not "Deployments"), or the GVR resource name should be singularized.

**Fix:** Use the actual Kind from the unstructured object's `GetKind()` instead of deriving it from the GVR resource name.

### M7. `pkg/tools/workflow.go:243-249` -- wf_events uses Limit on ListOptions but API may not honor it for sorting

**Severity:** Medium

The `Limit` field on `ListOptions` is a server-side pagination hint. The server returns the N oldest events (by resource version), not the N most recent. Then the code sorts by LastSeen descending. This means on namespaces with many events, you might miss the most recent ones because the server returned the oldest N.

**Fix:** Remove `Limit` from `ListOptions` and instead truncate the sorted result slice client-side after sorting.

### M8. `pkg/guard/guard.go` -- Guard only protects one namespace

**Severity:** Medium

The guard only checks for `tentacular-system`. Other critical namespaces like `kube-system`, `kube-public`, `kube-node-lease`, and `default` are not protected. While the design only specifies protecting `tentacular-system`, a defense-in-depth approach would protect all system namespaces.

**Fix:** Consider adding `kube-system`, `kube-public`, `kube-node-lease` to the protected set. At minimum, document this as a conscious decision.

### M9. `pkg/tools/namespace.go:116-165` -- ns_create does not validate namespace name format

**Severity:** Medium

The `name` parameter is passed directly to the K8s API without client-side validation. While K8s will reject invalid names, the error messages from the API are not user-friendly. Names with uppercase, special characters, or exceeding 63 characters will produce opaque errors.

**Fix:** Add a regex validation for RFC 1123 DNS label compliance before calling CreateNamespace.

---

## Low Findings

### L1. `pkg/k8s/profile.go:345-365` -- Variable shadowing: `lrs` used for both LimitRange list and LimitRangeSummary

**Severity:** Low

In `profileNamespaceDetails`, the variable `lrs` is first used for the `LimitRangeList` return value, then re-declared as `*LimitRangeSummary` inside the loop. This compiles but is confusing. The same pattern exists in `pkg/tools/namespace.go:216-238`.

**Fix:** Rename the inner variable to `summary` or `lrSummary`.

### L2. `cmd/tentacular-mcp/main.go:51-57` -- HTTP server missing MaxHeaderBytes

**Severity:** Low

The HTTP server does not set `MaxHeaderBytes`. The default is 1MB which is fine for most cases, but explicitly setting it is good practice for a security-focused server.

**Fix:** Add `MaxHeaderBytes: 1 << 20` (1MB) or a smaller value like 8KB.

### L3. `pkg/k8s/profile.go:282-310` -- profileExtensions swallows discovery errors partially

**Severity:** Low

When `ServerGroupsAndResources` returns an error but non-nil resources, the error is silently ignored. This could mask partial failures where some API groups are inaccessible.

**Fix:** Log the partial error (once logger is available) so operators can diagnose discovery issues.

### L4. `pkg/tools/health.go:123-128` -- health_nodes collects conditions where Status != False, including Unknown

**Severity:** Low

The condition `cond.Status != corev1.ConditionFalse` means conditions with status "Unknown" are also reported as unhealthy. This is actually correct behavior (Unknown conditions are noteworthy), but the variable name and comment say "unhealthy" which could be misleading.

**Fix:** Rename the comment to clarify that Unknown conditions are intentionally included.

### L5. `pkg/k8s/tokens.go:40-58` -- Kubeconfig template uses unescaped template values

**Severity:** Low

The kubeconfig template injects values without YAML escaping. If the server URL or token contains special YAML characters, the generated kubeconfig could be malformed. In practice, tokens are base64-like and server URLs are well-formed, so this is unlikely to cause issues.

**Fix:** No immediate fix needed, but consider using `sigs.k8s.io/yaml` marshaling instead of text templates for robust YAML generation.

---

## Positive Observations

1. **Consistent guard enforcement:** Every namespace-scoped tool calls `guard.CheckNamespace()` before any K8s API call. The pattern is applied uniformly at the registration layer.

2. **Clean error wrapping:** All errors use `fmt.Errorf` with `%w`, preserving the error chain for type checking (IsNotFound, IsAlreadyExists, etc.).

3. **Good separation of concerns:** Tool handlers are pure functions that take `*k8s.Client` and typed params. The MCP protocol layer (registration, marshaling) is separated from business logic.

4. **Comprehensive tool coverage:** All 24 tools specified in the design are implemented with matching param/result types.

5. **Proper context handling:** All K8s API calls respect context with appropriate timeouts.

6. **gVisor verification pod cleanup:** The deferred cleanup in `handleGVisorVerify` uses a separate context (background + 10s timeout) to ensure cleanup even if the parent context is cancelled.
