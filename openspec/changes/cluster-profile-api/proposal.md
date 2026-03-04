# Verify and update cluster_profile tool API compatibility

## Why

The CLI's `tntc cluster profile` command is being routed through the MCP server
(Phase 3 in the tentacular repo). The MCP server's `cluster_profile` tool must
return all fields that the CLI currently expects from its direct K8s profiler.

The current `cluster_profile` tool returns: K8s version, distribution, nodes,
runtime classes, CNI, storage, and extensions. The CLI's `k8s.ClusterProfile`
struct (in the tentacular repo at `pkg/k8s/profile.go`) may include additional
fields like namespace-scoped resource quotas, LimitRanges, and Pod Security
Admission posture.

This change ensures the MCP tool's response is a superset of what the CLI needs.

## What Changes

- **Audit `cluster_profile` tool output** against the CLI's `k8s.ClusterProfile`
  struct to identify any missing fields.
- **Add missing fields** to the `cluster_profile` tool response if any are found.
- **Ensure the `namespace` parameter** works correctly -- when provided, the tool
  should include namespace-scoped details (quotas, limit ranges, PSA labels).
- **Update tool documentation** in the tool registration if the response schema
  changes.
- **Add or update tests** to verify the response includes all expected fields.

## Acceptance Criteria

- The `cluster_profile` tool response includes every field that the CLI's
  `ClusterProfile` struct expects.
- The `namespace` parameter correctly returns quota and limit range data.
- Existing MCP clients (agents, skill) are not broken by any response format
  changes (additive-only changes).

## Non-goals

- Changing the tool's name or input parameters.
- Adding new profiling capabilities beyond what the CLI already expects.
