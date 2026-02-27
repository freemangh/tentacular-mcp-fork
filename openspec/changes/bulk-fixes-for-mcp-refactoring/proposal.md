## Why

The MCP server's `wf_run` tool creates an ephemeral runner pod to trigger workflows via in-cluster curl. In PSA-restricted namespaces, this pod fails to schedule because it lacks the required security context fields. Additionally, `wf_apply` has a suspected ConfigMap data truncation issue where large workflow code may be silently truncated when applied via the MCP server.

## What Changes

- Add PSA-compliant security context to the `wf_run` runner pod (runAsNonRoot, runAsUser 65534, drop ALL capabilities, readOnlyRootFilesystem)
- Investigate and fix ConfigMap data truncation in `wf_apply` -- determine if the issue is in manifest serialization, the K8s API call, or data size limits

## Capabilities

### New Capabilities
<!-- None -->

### Modified Capabilities
- `wf-run`: Add PSA-compliant security context to ephemeral runner pod
- `wf-apply`: Fix ConfigMap data handling to prevent truncation

## Impact

- `cmd/server/tools_wf_run.go` (or equivalent): Add securityContext to runner pod spec
- `cmd/server/tools_wf_apply.go` (or equivalent): Investigate and fix ConfigMap data truncation
- Tests: Add test verifying runner pod security context and ConfigMap data integrity
