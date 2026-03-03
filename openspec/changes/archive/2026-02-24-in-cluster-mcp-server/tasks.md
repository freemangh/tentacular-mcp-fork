## 1. Tool Registration Framework

- [x] 1.1 Create `pkg/tools/register.go` with `RegisterAll(srv *mcp.Server, client *k8s.Client, logger *slog.Logger)` function
- [x] 1.2 Implement generic MCP tool registration wrapper that handles JSON param unmarshaling, `guard.CheckNamespace()` enforcement, result marshaling into `mcp.Content`, and error-to-MCP-error conversion
- [x] 1.3 Update `pkg/server/server.go` `registerTools()` to call `tools.RegisterAll()`
- [x] 1.4 Add unit tests for the registration wrapper: guard rejection, param validation errors, successful dispatch

## 2. Namespace Lifecycle Tools

- [x] 2.1 Create `pkg/tools/namespace.go` with param/result types for `ns_create`, `ns_delete`, `ns_get`, `ns_list`
- [x] 2.2 Implement `ns_create` handler orchestrating: `CreateNamespace` -> `CreateDefaultDenyPolicy` -> `CreateDNSAllowPolicy` -> `CreateResourceQuota` -> `CreateLimitRange` -> `CreateWorkflowServiceAccount` -> `CreateWorkflowRole` -> `CreateWorkflowRoleBinding`
- [x] 2.3 Implement `ns_delete` handler with managed-by label check before deletion
- [x] 2.4 Implement `ns_get` handler returning metadata, labels, status, quota summary, limit range summary
- [x] 2.5 Implement `ns_list` handler listing namespaces with the managed-by label
- [x] 2.6 Register all 4 namespace tools in `register.go` with MCP schema definitions
- [x] 2.7 Add unit tests for namespace handlers using fake clientset

## 3. Credential Management Tools

- [x] 3.1 Create `pkg/tools/credential.go` with param/result types for `cred_issue_token`, `cred_kubeconfig`, `cred_rotate`
- [x] 3.2 Implement `cred_issue_token` handler with TTL validation (10-1440 min) calling `k8s.IssueToken()`
- [x] 3.3 Implement `cred_kubeconfig` handler calling token issuance internally then `k8s.GenerateKubeconfig()` using cluster CA and API URL from `rest.Config`
- [x] 3.4 Implement `cred_rotate` handler calling `k8s.RecreateWorkflowServiceAccount()`
- [x] 3.5 Register all 3 credential tools in `register.go` with MCP schema definitions
- [x] 3.6 Add unit tests for credential handlers using fake clientset

## 4. Workflow Introspection Tools

- [x] 4.1 Create `pkg/tools/workflow.go` with param/result types for `wf_pods`, `wf_logs`, `wf_events`, `wf_jobs`
- [x] 4.2 Implement `wf_pods` handler listing pods with name, phase, ready condition, restart count, images, age
- [x] 4.3 Implement `wf_logs` handler reading pod logs with tail_lines (default 100) and optional container parameter
- [x] 4.4 Implement `wf_events` handler listing events sorted by last timestamp descending, limited to 100 by default
- [x] 4.5 Implement `wf_jobs` handler listing Jobs (name, status, start, completion, duration) and CronJobs (name, schedule, last_scheduled, active, suspended)
- [x] 4.6 Register all 4 workflow tools in `register.go` with MCP schema definitions
- [x] 4.7 Add unit tests for workflow handlers using fake clientset

## 5. Cluster Operations Tools

- [x] 5.1 Create `pkg/tools/clusterops.go` with param/result types for `cluster_preflight`, `cluster_profile`
- [x] 5.2 Implement `cluster_preflight` handler wrapping `k8s.RunPreflightChecks()`
- [x] 5.3 Implement `cluster_profile` handler wrapping `k8s.ProfileCluster()` with optional namespace parameter
- [x] 5.4 Register both cluster ops tools in `register.go` (preflight with namespace guard, profile with optional namespace guard)
- [x] 5.5 Add unit tests for cluster ops handlers using fake clientset

## 6. gVisor Sandbox Tools

- [x] 6.1 Create `pkg/tools/gvisor.go` with param/result types for `gvisor_check`, `gvisor_annotate_ns`, `gvisor_verify`
- [x] 6.2 Implement `gvisor_check` handler listing RuntimeClasses and checking for gvisor/runsc handler (cluster-scoped, no namespace guard)
- [x] 6.3 Implement `gvisor_annotate_ns` handler: verify namespace is managed, verify gVisor RuntimeClass exists, patch namespace with `tentacular.io/runtime-class: gvisor` annotation
- [x] 6.4 Implement `gvisor_verify` handler: create busybox pod with gVisor RuntimeClass, wait up to 60s for completion, read logs, check for gVisor/runsc markers, cleanup pod in defer
- [x] 6.5 Register all 3 gVisor tools in `register.go` with MCP schema definitions
- [x] 6.6 Add unit tests for gVisor handlers using fake clientset

## 7. Module Proxy Tools

- [x] 7.1 Create `pkg/tools/module.go` with param/result types for `module_apply`, `module_remove`, `module_status`
- [x] 7.2 Implement `module_apply` handler: verify namespace is managed, use dynamic client to apply unstructured manifests, label all resources with `tentacular.io/release: <name>`, garbage-collect previously-labeled resources not in new manifest set
- [x] 7.3 Implement GVR resolution helper using discovery API RESTMapper to derive Group/Version/Resource from manifest apiVersion and kind
- [x] 7.4 Implement `module_remove` handler: delete all resources with matching release label in namespace
- [x] 7.5 Implement `module_status` handler: list resources by release label, report kind, name, and readiness status
- [x] 7.6 Register all 3 module tools in `register.go` with MCP schema definitions
- [x] 7.7 Add unit tests for module handlers using fake clientset and fake dynamic client

## 8. Cluster Health Tools

- [x] 8.1 Create `pkg/tools/health.go` with param/result types for `health_nodes`, `health_ns_usage`, `health_cluster_summary`
- [x] 8.2 Implement `health_nodes` handler: list nodes with ready condition, capacity, allocatable, kubelet version, and unhealthy conditions (cluster-scoped, no namespace guard)
- [x] 8.3 Implement `health_ns_usage` handler: compare namespace resource usage against ResourceQuota limits, return utilization percentages
- [x] 8.4 Implement `health_cluster_summary` handler: aggregate CPU, memory, pod counts across all nodes (cluster-scoped, no namespace guard)
- [x] 8.5 Register all 3 health tools in `register.go` with MCP schema definitions
- [x] 8.6 Add unit tests for health handlers using fake clientset

## 9. Security Audit Tools

- [x] 9.1 Create `pkg/tools/audit.go` with param/result types for `audit_rbac`, `audit_netpol`, `audit_psa`
- [x] 9.2 Implement `audit_rbac` handler: scan Roles and RoleBindings for wildcard verbs/resources, sensitive resource access; also inspect ClusterRoleBindings targeting namespace ServiceAccounts
- [x] 9.3 Implement `audit_netpol` handler: verify default-deny NetworkPolicy exists, flag missing egress restrictions, list all policies
- [x] 9.4 Implement `audit_psa` handler: check PSA enforce/audit/warn labels, flag non-restricted or missing PSA configuration
- [x] 9.5 Register all 3 audit tools in `register.go` with MCP schema definitions
- [x] 9.6 Add unit tests for audit handlers using fake clientset

## 10. ClusterRole RBAC Updates

- [x] 10.1 Update ClusterRole in `deploy/manifests/serviceaccount.yaml` to add `clusterroles` and `clusterrolebindings` get/list for audit_rbac
- [x] 10.2 Add `namespaces/patch` verb to ClusterRole for gvisor_annotate_ns
- [x] 10.3 Add `pods/create,delete` verbs to ClusterRole for gvisor_verify
- [x] 10.4 Elevate `apps/deployments`, `core/services,configmaps,secrets`, `batch/cronjobs,jobs`, `networking.k8s.io/networkpolicies` to include `create,update,delete,patch` verbs for module_apply
- [x] 10.5 Add `patch` and `update` verbs to `serviceaccounts` in ClusterRole for imagePullSecrets management on the default SA
- [x] 10.6 Add `ingresses` (networking.k8s.io) to ClusterRole with `get,list,watch` for profiling and `create,update,delete,patch` for namespace workflow
- [x] 10.7 Add `watch` verb to all existing read-only resource rules in ClusterRole (nodes, storageclasses, csidrivers, runtimeclasses, CRDs, pods, events)
- [x] 10.8 Add broader profiling resources to ClusterRole: `replicasets,daemonsets,statefulsets` (apps), `persistentvolumes,persistentvolumeclaims` (core), `endpoints` (core), `endpointslices` (discovery.k8s.io), `volumeattachments` (storage.k8s.io) with `get,list,watch`

## 10a. Namespace Workflow Role RBAC Updates (pkg/k8s/rbac.go)

- [x] 10a.1 Add `patch` verb to all mutable resource rules in `CreateWorkflowRole()` (deployments, services, configmaps, secrets, cronjobs, jobs, networkpolicies)
- [x] 10a.2 Add `watch` verb to all resource rules in `CreateWorkflowRole()` (both mutable and read-only)
- [x] 10a.3 Add `ingresses` (networking.k8s.io) to workflow Role with `create,update,delete,patch,get,list,watch`
- [x] 10a.4 Add `serviceaccounts` rule to workflow Role with `get,list,patch,update` for imagePullSecrets management
- [ ] 10a.5 Add unit tests verifying the updated workflow Role rules

## 11. Integration and Build

- [x] 11.1 Verify `go build ./...` compiles cleanly with all new code
- [x] 11.2 Run `go vet ./...` and fix any issues
- [x] 11.3 Run full test suite with `go test ./...` and verify all tests pass
- [ ] 11.4 Build container image with `docker build` using existing Dockerfile
- [x] 11.5 Verify kustomize build with `kubectl kustomize deploy/manifests/` produces valid YAML with updated ClusterRole
