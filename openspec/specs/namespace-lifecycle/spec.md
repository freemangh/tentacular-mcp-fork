# namespace-lifecycle Specification

## Purpose
TBD - created by archiving change in-cluster-mcp-server. Update Purpose after archive.
## Requirements
### Requirement: Create managed namespace
The system SHALL create a Kubernetes namespace with the `app.kubernetes.io/managed-by: tentacular` label and Pod Security Admission labels set to `restricted` profile at `latest` version. The system SHALL also create a default-deny NetworkPolicy, a DNS-allow NetworkPolicy, a ResourceQuota (from a named preset), a LimitRange with default container resource requests/limits, a workflow ServiceAccount, a workflow Role, and a workflow RoleBinding in the new namespace. The workflow Role SHALL grant `create,update,delete,patch,get,list,watch` on apps/deployments, core/services+configmaps+secrets, batch/cronjobs+jobs, networking.k8s.io/networkpolicies+ingresses; `get,list,watch` on core/pods+pods/log+events; and `get,list,patch,update` on core/serviceaccounts (for imagePullSecrets management). The system SHALL reject the operation if the target namespace is `tentacular-system`.

#### Scenario: Successful namespace creation with small quota
- **WHEN** the `ns_create` tool is called with `name: "dev-alice"` and `quota_preset: "small"`
- **THEN** the system creates namespace `dev-alice` with managed-by label, PSA restricted labels, default-deny and DNS-allow NetworkPolicies, a ResourceQuota with CPU=2, Mem=2Gi, Pods=10, a LimitRange, and the workflow ServiceAccount/Role/RoleBinding

#### Scenario: Reject creation of protected namespace
- **WHEN** the `ns_create` tool is called with `name: "tentacular-system"`
- **THEN** the system returns an error indicating operations on `tentacular-system` are not allowed

#### Scenario: Namespace already exists
- **WHEN** the `ns_create` tool is called with a name that already exists
- **THEN** the system returns an error indicating the namespace already exists

### Requirement: Delete managed namespace
The system SHALL delete a namespace only if it carries the `app.kubernetes.io/managed-by: tentacular` label. The system SHALL reject deletion of `tentacular-system`. Deleting a namespace removes all resources within it (Kubernetes garbage collection).

#### Scenario: Successful deletion of managed namespace
- **WHEN** the `ns_delete` tool is called with `name: "dev-alice"` and the namespace has the managed-by label
- **THEN** the system deletes the namespace

#### Scenario: Reject deletion of unmanaged namespace
- **WHEN** the `ns_delete` tool is called with a namespace that lacks the managed-by label
- **THEN** the system returns an error indicating the namespace is not managed by tentacular

#### Scenario: Reject deletion of protected namespace
- **WHEN** the `ns_delete` tool is called with `name: "tentacular-system"`
- **THEN** the system returns an error indicating operations on `tentacular-system` are not allowed

### Requirement: Get namespace details
The system SHALL retrieve a single namespace by name and return its metadata, labels, annotations, status, resource quota usage, and limit range configuration. The system SHALL reject the operation if the target is `tentacular-system`.

#### Scenario: Get existing namespace
- **WHEN** the `ns_get` tool is called with `name: "dev-alice"`
- **THEN** the system returns the namespace metadata, labels, status phase, quota summary, and limit range summary

#### Scenario: Namespace not found
- **WHEN** the `ns_get` tool is called with a name that does not exist
- **THEN** the system returns an error indicating the namespace was not found

### Requirement: List managed namespaces
The system SHALL list all namespaces with the `app.kubernetes.io/managed-by: tentacular` label, returning name, status, creation timestamp, and quota preset for each.

#### Scenario: List with managed namespaces present
- **WHEN** the `ns_list` tool is called and managed namespaces exist
- **THEN** the system returns a list of managed namespaces with their metadata

#### Scenario: List with no managed namespaces
- **WHEN** the `ns_list` tool is called and no managed namespaces exist
- **THEN** the system returns an empty list

### Requirement: Update managed namespace
The system SHALL update labels, annotations, and/or resource quota preset on a managed namespace. At least one update field must be provided. The system SHALL reject the operation if the namespace is not managed by tentacular, if the target namespace is in the protected set, or if the caller attempts to change the `app.kubernetes.io/managed-by` label. Label and annotation updates are additive (existing keys not listed are preserved).

#### Scenario: Update labels on managed namespace
- **WHEN** the `ns_update` tool is called with `name: "dev-alice"` and `labels: {"env": "staging"}`
- **THEN** the system adds the `env=staging` label while preserving existing labels including `managed-by`

#### Scenario: Update quota preset on managed namespace
- **WHEN** the `ns_update` tool is called with `name: "dev-alice"` and `quota_preset: "large"`
- **THEN** the system updates the `tentacular-quota` ResourceQuota to the `large` preset (CPU=8, Mem=16Gi, Pods=50)

#### Scenario: Reject update on unmanaged namespace
- **WHEN** the `ns_update` tool is called with a namespace that lacks the managed-by label
- **THEN** the system returns an error indicating the namespace is not managed by tentacular

#### Scenario: Reject when no update fields provided
- **WHEN** the `ns_update` tool is called with only `name` and no labels, annotations, or quota_preset
- **THEN** the system returns an error indicating at least one update field is required

#### Scenario: Reject managed-by label change
- **WHEN** the `ns_update` tool is called with `labels: {"app.kubernetes.io/managed-by": "other"}`
- **THEN** the system returns an error indicating the managed-by label cannot be changed

