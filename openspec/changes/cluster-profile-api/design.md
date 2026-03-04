# Design: Verify cluster_profile tool API compatibility

## Current Tool Response

The `cluster_profile` MCP tool returns a JSON object with these top-level fields
(verify against actual implementation):

- `kubernetes_version` -- cluster K8s version string
- `distribution` -- detected distribution (k0s, kind, EKS, etc.)
- `nodes` -- array of node info (name, roles, capacity, runtime)
- `runtime_classes` -- available RuntimeClasses
- `cni` -- detected CNI plugin
- `storage_classes` -- available StorageClasses
- `extensions` -- installed CRD-based extensions (Istio, cert-manager, etc.)

## CLI Expected Fields

The CLI's `k8s.ClusterProfile` struct in `pkg/k8s/profile.go` includes:

- K8s version and distribution
- Node topology (count, roles, capacity, allocatable)
- RuntimeClasses
- CNI
- StorageClasses and CSI drivers
- CRD-based extensions
- Namespace-scoped: ResourceQuotas, LimitRanges, PSA labels

## Gap Analysis

Fields that may be missing from the MCP tool:
- CSI drivers (separate from StorageClasses)
- Namespace-scoped ResourceQuota details
- Namespace-scoped LimitRange details
- Pod Security Admission labels per namespace

The `namespace` parameter on the tool should trigger inclusion of these
namespace-scoped details.

## Implementation

1. Compare the actual `cluster_profile` handler output in the MCP server code
   against the CLI's `ClusterProfile` struct.
2. Add any missing fields to the tool's response.
3. Ensure the `namespace` parameter populates quota, limit range, and PSA data.
4. Add test cases for the namespace-scoped path.

## Backwards Compatibility

All changes are additive -- new fields in the response. No existing fields are
removed or renamed. Existing MCP clients that ignore unknown fields are
unaffected.
