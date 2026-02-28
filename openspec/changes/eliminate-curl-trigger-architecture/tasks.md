# Tasks

## PR #4 -- Baseline cleanup

- [x] Audit existing wf_run implementation for curl pod creation code

## PR #5 -- API service proxy for wf_run

- [x] Replace ephemeral curl pod creation in `wf_run` with K8s API service proxy call
- [x] Add `services/proxy` to ClusterRole in `mcp_deploy.go`
- [x] Remove trigger pod cleanup logic from `wf_run`
- [x] Test wf_run via proxy in unit tests

## PR #6 -- Internal cron scheduler

- [x] Add `robfig/cron/v3` dependency to `go.mod`
- [x] Implement `SyncWorkflowCrons()` scanning Deployments for `tentacular.dev/cron-schedule`
- [x] Start cron scheduler in `cmd/tentacular-mcp/main.go`
- [x] Call `SyncWorkflowCrons()` after `wf_apply` and `wf_remove`
- [x] Add graceful shutdown for cron scheduler
- [x] Test scheduler registration and firing

## PR #7 -- Module pre-warm in wf_apply

- [x] Implement module dependency extraction from ConfigMap in `wf_apply`
- [x] Implement background goroutine for esm.sh cache warming via API service proxy
- [x] Document pre-warm race condition in code comments
- [x] Test pre-warm goroutine launch (not blocking on wf_apply return)

## PR #8 -- Output schema fix

- [x] Change `wf_run` output type from `json.RawMessage` to `any`
- [x] Verify MCP SDK serializes workflow output as structured JSON (not base64)
- [x] Update unit tests to assert correct output shape

## PR #9 -- Error handling for proxy responses

- [x] Capture HTTP 500 response bodies from workflow proxy calls
- [x] Return 500 body in tool error instead of dropping it
- [x] Test error propagation from workflow pod to MCP client
- [x] Confirm go test -count=1 ./... passes across all PRs
