## 1. Runner Pod PSA Compliance

- [ ] 1.1 Locate runner pod spec in `wf_run` tool implementation
- [ ] 1.2 Add securityContext: runAsNonRoot, runAsUser 65534, drop ALL capabilities, readOnlyRootFilesystem
- [ ] 1.3 Add emptyDir `/tmp` volume if curl needs writable temp space
- [ ] 1.4 Add test verifying runner pod security context fields

## 2. ConfigMap Data Truncation

- [ ] 2.1 Reproduce the truncation issue with a large workflow
- [ ] 2.2 Identify root cause (serialization, API client, or size limit)
- [ ] 2.3 Implement fix based on root cause
- [ ] 2.4 Add test verifying ConfigMap data integrity for large payloads

## 3. Verification

- [ ] 3.1 Run all MCP server tests -- all pass
- [ ] 3.2 Verify runner pod deploys in PSA-restricted namespace
