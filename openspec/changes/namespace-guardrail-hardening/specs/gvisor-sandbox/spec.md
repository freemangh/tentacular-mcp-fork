## MODIFIED Requirements

### Requirement: Apply gVisor to namespace
The system SHALL annotate a managed namespace to indicate gVisor sandboxing is desired by adding the annotation `tentacular.io/runtime-class: gvisor`. This annotation signals to workflow deployments that pods SHOULD use the gVisor RuntimeClass. The system SHALL verify the namespace is managed by tentacular and SHALL reject the operation if the target namespace is in the protected set (`tentacular-system`, `kube-system`, `kube-public`, `kube-node-lease`, `default`) or is not managed by tentacular.

#### Scenario: Apply gVisor annotation to managed namespace
- **WHEN** the `gvisor_annotate_ns` tool is called with `namespace: "dev-alice"` and the namespace is managed
- **THEN** the system adds the `tentacular.io/runtime-class: gvisor` annotation to the namespace

#### Scenario: Reject for unmanaged namespace
- **WHEN** the `gvisor_annotate_ns` tool is called for a namespace without the managed-by label
- **THEN** the system returns an error indicating the namespace is not managed by tentacular and includes the kubectl label command to adopt it

#### Scenario: gVisor not available in cluster
- **WHEN** the `gvisor_annotate_ns` tool is called and no gVisor RuntimeClass exists
- **THEN** the system returns an error indicating gVisor is not available in the cluster

### Requirement: Verify gVisor sandbox
The system SHALL launch a short-lived verification pod in the target namespace using the gVisor RuntimeClass, run `dmesg | head -1` inside it, confirm the output contains "gVisor" or "runsc", and clean up the pod. The system SHALL reject the operation if the target namespace is in the protected set or is not managed by tentacular.

#### Scenario: gVisor verification succeeds
- **WHEN** the `gvisor_verify` tool is called with `namespace: "dev-alice"` and gVisor is functional
- **THEN** the system returns `verified: true` with the dmesg output confirming gVisor

#### Scenario: gVisor verification fails
- **WHEN** the `gvisor_verify` tool is called and the verification pod output does not contain gVisor markers
- **THEN** the system returns `verified: false` with the actual dmesg output for debugging

#### Scenario: Verification pod times out
- **WHEN** the `gvisor_verify` tool is called and the verification pod does not reach Running state within 60 seconds
- **THEN** the system returns an error indicating the verification timed out and cleans up the pod

#### Scenario: Reject for unmanaged namespace
- **WHEN** the `gvisor_verify` tool is called with a namespace that lacks the managed-by label
- **THEN** the system returns an error indicating the namespace is not managed by tentacular
