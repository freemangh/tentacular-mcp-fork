## Context

The MCP server provides `wf_run` and `wf_apply` tools for deploying and triggering workflows via the MCP protocol. After enabling PSA enforcement on managed namespaces, `wf_run` runner pods fail validation. Separately, `wf_apply` may be truncating ConfigMap data for larger workflows.

## Goals / Non-Goals

**Goals:**
- Make `wf_run` runner pod PSA-compliant so it schedules in restricted namespaces
- Identify and fix ConfigMap data truncation in `wf_apply`

**Non-Goals:**
- Changing the runner pod architecture (it remains ephemeral curl-based)
- Adding new MCP tools
- Modifying the MCP protocol

## Decisions

### Runner pod security context
Use the standard non-root security context pattern: `runAsNonRoot: true`, `runAsUser: 65534` (nobody), `allowPrivilegeEscalation: false`, `capabilities: {drop: ["ALL"]}`, `readOnlyRootFilesystem: true`. The `curlimages/curl` image supports running as non-root.

### ConfigMap data investigation
**Outcome: no server-side truncation.** The data path through `handleWorkflowApply` is:
1. MCP JSON deserializes into `WorkflowApplyParams.Manifests []map[string]interface{}`
2. Each manifest is wrapped in `unstructured.Unstructured{Object: manifest}` — no copy, no re-marshaling
3. Dynamic client `Create`/`Update` passes it directly to the K8s API

There is no JSON round-trip, buffer, or size limit applied in this function. Values up to at least 7KB pass through intact (verified by `TestWorkflowApplyConfigMapLargeDataIntegrity`).

If ConfigMap data appears truncated in practice, the cause is **client-side**: the LLM generating the manifests is truncating or omitting content before it reaches the MCP server. No server-side fix is needed or possible for that case.

## Risks / Trade-offs

- **readOnlyRootFilesystem on curl**: curl may need to write temp files. If so, add an emptyDir volume at `/tmp`. Low risk since curl for simple POST requests doesn't typically write to disk.
- **ConfigMap size limit**: If truncation is caused by hitting the 1MB ConfigMap limit, the fix may require splitting large code across multiple ConfigMaps or switching to a different delivery mechanism.
