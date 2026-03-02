# security-audit Specification

## Purpose
TBD - created by archiving change in-cluster-mcp-server. Update Purpose after archive.
## Requirements
### Requirement: Audit RBAC for over-permissions
The system SHALL scan all Roles and RoleBindings in a given namespace and flag any rules that grant wildcard verbs (`*`), wildcard resources (`*`), access to sensitive resources (secrets, serviceaccounts/token, pods/exec, pods/attach), or escalation verbs (`bind`, `escalate`, `impersonate`). The system SHALL return a list of findings, each with the role name, the problematic rule, a severity level (high/medium/low), and a remediation suggestion. The system SHALL reject the operation if the target namespace is `tentacular-system`.

#### Scenario: Namespace with over-permissioned role
- **WHEN** the `audit_rbac` tool is called with `namespace: "dev-alice"` and a Role grants `*` verbs on secrets
- **THEN** the system returns a finding with severity `high`, the role name, the flagged rule, and a remediation suggestion

#### Scenario: Namespace with clean RBAC
- **WHEN** the `audit_rbac` tool is called and all Roles follow least-privilege principles
- **THEN** the system returns an empty findings list

#### Scenario: Also inspect ClusterRoleBindings targeting namespace
- **WHEN** the `audit_rbac` tool is called and a ClusterRoleBinding grants a ClusterRole to a ServiceAccount in the target namespace
- **THEN** the system includes any over-permissioned rules from the bound ClusterRole in the findings

#### Scenario: Detect escalation via bind verb
- **WHEN** the `audit_rbac` tool is called and a Role grants the `bind` verb on roles or clusterroles
- **THEN** the system returns a finding with severity `high` indicating privilege escalation risk and a remediation to remove the verb

#### Scenario: Detect escalation via escalate verb
- **WHEN** the `audit_rbac` tool is called and a Role grants the `escalate` verb
- **THEN** the system returns a finding with severity `high` indicating the role can modify its own permissions

#### Scenario: Detect impersonation capability
- **WHEN** the `audit_rbac` tool is called and a Role grants the `impersonate` verb on users, groups, or serviceaccounts
- **THEN** the system returns a finding with severity `high` indicating identity impersonation risk

### Requirement: Audit network policy coverage
The system SHALL verify that a namespace has at least a default-deny NetworkPolicy (denying all ingress and egress) and report on all NetworkPolicies present. The system SHALL flag namespaces that allow unrestricted egress or have no NetworkPolicies at all. The system SHALL reject the operation if the target namespace is `tentacular-system`.

#### Scenario: Namespace with default-deny policy
- **WHEN** the `audit_netpol` tool is called with `namespace: "dev-alice"` and a default-deny policy exists
- **THEN** the system returns `default_deny: true` and lists all NetworkPolicies with their policy types and pod selectors

#### Scenario: Namespace without network policies
- **WHEN** the `audit_netpol` tool is called and no NetworkPolicies exist in the namespace
- **THEN** the system returns `default_deny: false` with a finding flagging the namespace as having unrestricted network access

#### Scenario: Namespace with partial coverage
- **WHEN** the `audit_netpol` tool is called and the namespace has ingress policies but no egress restriction
- **THEN** the system returns `default_deny: false` with a finding noting unrestricted egress

### Requirement: Audit Pod Security Admission labels
The system SHALL check the Pod Security Admission labels on a namespace and report the enforce, audit, and warn levels. The system SHALL flag namespaces that do not have PSA enforce set to `restricted` or that have no PSA labels at all. The system SHALL reject the operation if the target namespace is `tentacular-system`.

#### Scenario: Namespace with restricted PSA
- **WHEN** the `audit_psa` tool is called with `namespace: "dev-alice"` and PSA enforce is `restricted`
- **THEN** the system returns `compliant: true` with the enforce, audit, and warn levels

#### Scenario: Namespace with baseline or privileged PSA
- **WHEN** the `audit_psa` tool is called and PSA enforce is `baseline`
- **THEN** the system returns `compliant: false` with a finding recommending upgrade to `restricted`

#### Scenario: Namespace with no PSA labels
- **WHEN** the `audit_psa` tool is called and no PSA labels exist on the namespace
- **THEN** the system returns `compliant: false` with a finding noting the absence of PSA configuration

