## 1. Expand Static Namespace Blocklist

- [x] 1.1 Update `pkg/guard/guard.go` to add `kube-system`, `kube-public`, `kube-node-lease`, and `default` to the `systemNamespaces` map alongside `tentacular-system`
- [x] 1.2 Update `pkg/guard/guard_test.go` to assert all five system namespaces are blocked and update the allowed-namespace test cases

## 2. Add Shared Managed-Namespace Helper

- [x] 2.1 Add `CheckManagedNamespace(ctx, client, name)` to `pkg/k8s/namespace.go` that calls `GetNamespace`, checks `IsManagedNamespace`, and returns an actionable error with the `kubectl label` adoption command
- [x] 2.2 Verify unit test coverage for the new helper (namespace not found, namespace unmanaged, namespace managed)

## 3. Apply Managed-Namespace Check to Credential Tools

- [x] 3.1 Add `k8s.CheckManagedNamespace` call at the top of `handleCredIssueToken` in `pkg/tools/credential.go` (before TTL validation)
- [x] 3.2 Add `k8s.CheckManagedNamespace` call at the top of `handleCredKubeconfig` in `pkg/tools/credential.go`
- [x] 3.3 Add `k8s.CheckManagedNamespace` call at the top of `handleCredRotate` in `pkg/tools/credential.go`
- [x] 3.4 Update `pkg/tools/credential_test.go` to seed fake client with managed namespace objects so existing tests continue to pass

## 4. Apply Managed-Namespace Check to gVisor Tools

- [x] 4.1 Refactor `handleGVisorAnnotateNs` in `pkg/tools/gvisor.go` to use `k8s.CheckManagedNamespace` (replacing the inline `IsManagedNamespace` check)
- [x] 4.2 Add `k8s.CheckManagedNamespace` call at the top of `handleGVisorVerify` in `pkg/tools/gvisor.go`

## 5. Apply Managed-Namespace Check to Module Tools

- [x] 5.1 Refactor `handleModuleApply` in `pkg/tools/module.go` to use `k8s.CheckManagedNamespace` (replacing the inline check)
- [x] 5.2 Add `k8s.CheckManagedNamespace` call at the top of `handleModuleRemove` in `pkg/tools/module.go`
- [x] 5.3 Add `k8s.CheckManagedNamespace` call at the top of `handleModuleStatus` in `pkg/tools/module.go`
- [x] 5.4 Update `pkg/tools/module_test.go` to seed fake client with managed namespace objects

## 6. Verify Unit Test Suite

- [x] 6.1 Run `go test ./...` and confirm all 136 tests pass with no regressions
- [x] 6.2 Confirm new guard tests cover all five blocked system namespaces
- [x] 6.3 Confirm credential and module tests cover the unmanaged-namespace rejection path
