# Tasks

## Implementation

- [ ] Compare MCP `cluster_profile` tool output against CLI's `k8s.ClusterProfile` struct
- [ ] Identify missing fields (CSI drivers, ResourceQuotas, LimitRanges, PSA labels)
- [ ] Add missing fields to the `cluster_profile` tool response
- [ ] Ensure `namespace` parameter populates namespace-scoped details
- [ ] Update tool description/schema if response format changes

## Testing

- [ ] Add test cases for `cluster_profile` with namespace parameter
- [ ] Verify all CLI-expected fields are present in the response
- [ ] Verify backwards compatibility (no removed fields)
- [ ] `go build ./...` passes
- [ ] `go test -count=1 ./...` passes
