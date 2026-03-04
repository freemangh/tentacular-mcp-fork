# Tentacular CLI Integration with tentacular-mcp

> **Note:** This is the original design document. The final implementation
> differs in key ways: MCP is no longer opt-in with per-command `--mcp-url`
> flags. The MCP server is installed via Helm, and MCP connection details are
> configured per-environment in `~/.tentacular/config.yaml`. The CLI has no
> direct K8s API access; all cluster commands route through MCP. See the
> tentacular-skill SKILL.md and cli.md for the current behavior.

This document specifies the changes required in the **tentacular** CLI repository
(`/Users/rbias/code/tentacular`) to integrate with the in-cluster MCP server
(`tentacular-mcp`). All MCP integration is opt-in via `--mcp-url` flags and the
`mcp_url` config field. Existing workflows continue to work unchanged.

---

## Table of Contents

1. [Overview](#overview)
2. [New Package: pkg/mcp](#new-package-pkgmcp)
3. [Config Changes](#config-changes)
4. [CLI Command Changes](#cli-command-changes)
5. [Kubeconfig Handling](#kubeconfig-handling)
6. [Skill Documentation Updates](#skill-documentation-updates)
7. [Backwards Compatibility](#backwards-compatibility)
8. [Migration Path](#migration-path)

---

## Overview

Today the tentacular CLI (`tntc`) communicates directly with the Kubernetes API
server using a kubeconfig that grants broad admin-level access. The MCP server
(`tentacular-mcp`) replaces this pattern by proxying K8s operations through a
single authenticated HTTP endpoint with scoped RBAC.

The CLI integration is additive. When `--mcp-url` is set (or `mcp_url` appears
in environment config), the CLI delegates cluster operations to the MCP server
instead of calling the K8s API directly. When not set, behavior is identical to
today.

### MCP Server Transport

- **Protocol**: MCP Streamable HTTP at `<mcp-url>/mcp`
- **Auth**: `Authorization: Bearer <token>` header
- **Health**: `GET <mcp-url>/healthz`

---

## New Package: pkg/mcp

A new `pkg/mcp/` package encapsulates all MCP server communication. This keeps
MCP transport concerns out of the CLI command code and the existing `pkg/k8s/`
package.

### File: `pkg/mcp/client.go`

```go
package mcp

// Client communicates with a tentacular-mcp server instance.
type Client struct {
    BaseURL string // e.g., "http://localhost:8080" or "http://tentacular-mcp.tentacular-system.svc:8080"
    Token   string // Bearer token for auth
}

// NewClient creates an MCP client. Token is read from the environment
// (TENTACULAR_MCP_TOKEN) or from a file path if prefixed with "file:".
func NewClient(baseURL, token string) *Client

// Preflight calls the cluster_preflight tool.
func (c *Client) Preflight(ctx context.Context, namespace string) ([]k8s.CheckResult, error)

// Profile calls the cluster_profile tool.
func (c *Client) Profile(ctx context.Context, namespace string) (*k8s.ClusterProfile, error)

// IssueKubeconfig calls the cred_kubeconfig tool.
func (c *Client) IssueKubeconfig(ctx context.Context, namespace string, ttlMinutes int) (string, error)

// Audit calls audit_rbac, audit_netpol, and audit_psa tools.
func (c *Client) AuditRBAC(ctx context.Context, namespace string) (interface{}, error)
func (c *Client) AuditNetpol(ctx context.Context, namespace string) (interface{}, error)
func (c *Client) AuditPSA(ctx context.Context, namespace string) (interface{}, error)

// WorkflowApply calls the wf_apply tool.
func (c *Client) WorkflowApply(ctx context.Context, namespace, name string, manifests []interface{}) (interface{}, error)

// WorkflowRemove calls the wf_remove tool.
func (c *Client) WorkflowRemove(ctx context.Context, namespace, name string) (interface{}, error)
```

Each method constructs an MCP `tools/call` request, sends it to `<BaseURL>/mcp`,
and unmarshals the JSON result from the MCP `Content` block. Errors from the MCP
server (responses with `isError: true`) are returned as Go errors.

### File: `pkg/mcp/transport.go`

Low-level MCP Streamable HTTP transport: session initialization, tool invocation,
JSON-RPC over HTTP. This can use the `github.com/mark3labs/mcp-go` client SDK
(already used in the MCP server's `go.mod`) or a minimal hand-rolled HTTP client.

---

## Config Changes

### File: `pkg/cli/environment.go`

**Type**: `EnvironmentConfig` struct (line 11)

Add a new field:

```go
type EnvironmentConfig struct {
    Kubeconfig      string                 `yaml:"kubeconfig,omitempty"`
    Context         string                 `yaml:"context,omitempty"`
    Namespace       string                 `yaml:"namespace,omitempty"`
    Image           string                 `yaml:"image,omitempty"`
    RuntimeClass    string                 `yaml:"runtime_class,omitempty"`
    ConfigOverrides map[string]interface{} `yaml:"config_overrides,omitempty"`
    SecretsSource   string                 `yaml:"secrets_source,omitempty"`
    Enforcement     string                 `yaml:"enforcement,omitempty"`
    MCPUrl          string                 `yaml:"mcp_url,omitempty"`    // NEW
    MCPToken        string                 `yaml:"mcp_token,omitempty"` // NEW (or file: path)
}
```

### File: `pkg/cli/config.go`

**Type**: `TentacularConfig` struct (line 20)

Add top-level defaults:

```go
type TentacularConfig struct {
    Registry     string                       `yaml:"registry,omitempty"`
    Namespace    string                       `yaml:"namespace,omitempty"`
    RuntimeClass string                       `yaml:"runtime_class,omitempty"`
    Environments map[string]EnvironmentConfig `yaml:"environments,omitempty"`
    ModuleProxy  ModuleProxyConfig            `yaml:"moduleProxy,omitempty"`
    MCPUrl       string                       `yaml:"mcp_url,omitempty"`    // NEW
    MCPToken     string                       `yaml:"mcp_token,omitempty"` // NEW
}
```

**Function**: `mergeConfig()` (line 53)

Add merge logic for the new fields:

```go
if override.MCPUrl != "" {
    base.MCPUrl = override.MCPUrl
}
if override.MCPToken != "" {
    base.MCPToken = override.MCPToken
}
```

### Config File Example

```yaml
# .tentacular/config.yaml
namespace: tentacular-workflows
environments:
  staging:
    context: staging-cluster
    namespace: staging-workflows
    mcp_url: http://tentacular-mcp.tentacular-system.svc:8080
    mcp_token: file:~/.tentacular/mcp-tokens/staging.token
  production:
    context: prod-cluster
    namespace: prod-workflows
    mcp_url: http://tentacular-mcp.tentacular-system.svc:8080
    mcp_token: file:~/.tentacular/mcp-tokens/prod.token
```

### Environment Variable

`TENTACULAR_MCP_URL` and `TENTACULAR_MCP_TOKEN` environment variables provide
fallback when no config file is present, following the same pattern as
`TENTACULAR_ENV`.

---

## CLI Command Changes

### 1. `tntc cluster check` -- Delegate preflight to MCP

**File**: `pkg/cli/cluster.go`
**Function**: `runClusterCheck()` (line 43)

**New flag**: `--mcp-url string` on the `check` command (line 24, after `--fix`)

**Change**: At the top of `runClusterCheck`, after resolving flags, check if
`--mcp-url` is set (or resolved from env config). If set:

```go
if mcpURL != "" {
    mcpClient := mcp.NewClient(mcpURL, mcpToken)
    results, err := mcpClient.Preflight(ctx, namespace)
    // ... render results using existing text/json output logic ...
    return nil
}
// ... existing direct K8s code unchanged below ...
```

**Specific changes**:
- Line 24: Add `check.Flags().String("mcp-url", "", "MCP server URL (delegates preflight to MCP)")`
- Line 43-95: Add MCP branch at top of `runClusterCheck()`
- The `--fix` flag is incompatible with `--mcp-url` (MCP server handles namespace creation via `ns_create` tool). Print an error if both are set.

### 2. `tntc cluster profile` -- Fetch profile from MCP

**File**: `pkg/cli/profile.go`
**Function**: `runProfileForEnv()` (line 90)

**New flag**: `--mcp-url string` on the `profile` command (line 31)

**Change**: In `runProfileForEnv()`, after building the client, check if MCP URL
is available (from flag, env config `MCPUrl`, or environment variable):

```go
if mcpURL != "" {
    mcpClient := mcp.NewClient(mcpURL, mcpToken)
    profile, err := mcpClient.Profile(ctx, namespace)
    // ... use existing render/save logic ...
}
```

**Specific changes**:
- `NewProfileCmd()` line 49: Add `cmd.Flags().String("mcp-url", "", "MCP server URL")`
- `runProfileForEnv()` line 117-120: After `buildClientForEnv()`, add MCP branch
- The `buildClientForEnv()` function (line 178) should also check `env.MCPUrl`

### 3. `tntc cluster install` -- Delegate module proxy to MCP

**File**: `pkg/cli/cluster.go`
**Function**: `runClusterInstall()` (line 120)

**New flag**: `--mcp-url string` on the `install` command (line 27)

**Change**: When `--mcp-url` is set, use `mcp.Client.WorkflowApply()` to deploy
the module proxy manifests through the MCP server instead of calling
`client.Apply()` directly.

```go
if mcpURL != "" {
    mcpClient := mcp.NewClient(mcpURL, mcpToken)
    manifests := k8s.GenerateModuleProxyManifests(image, proxyNamespace, storage, pvcSize)
    // Convert builder.Manifest to unstructured objects for wf_apply
    result, err := mcpClient.WorkflowApply(ctx, proxyNamespace, "esm-sh-proxy", convertedManifests)
    // ...
}
```

**Specific changes**:
- Line 27: Add `install.Flags().String("mcp-url", "", "MCP server URL")`
- Line 120-185: Add MCP branch at top of `runClusterInstall()`

### 4. `tntc deploy` -- Pre-deploy credential issuance

**File**: `pkg/cli/deploy.go`
**Function**: `runDeploy()` (line 56)

**New flag**: `--mcp-url string` on the deploy command (line 20)

**Change**: When `--mcp-url` is set (from flag or env config), add a pre-deploy
step that calls `cred_kubeconfig` to get a scoped, time-limited kubeconfig for
the target namespace. This kubeconfig is then used for the actual deployment
instead of the user's admin kubeconfig.

```go
if mcpURL != "" {
    mcpClient := mcp.NewClient(mcpURL, mcpToken)
    // Issue a scoped kubeconfig valid for 60 minutes
    kubeconfigYAML, err := mcpClient.IssueKubeconfig(ctx, namespace, 60)
    if err != nil {
        return fmt.Errorf("issuing MCP kubeconfig: %w", err)
    }
    // Write to temp file, set deployOpts.Kubeconfig
    tmpFile := filepath.Join(workflowDir, "scratch", ".mcp-kubeconfig")
    os.WriteFile(tmpFile, []byte(kubeconfigYAML), 0o600)
    defer os.Remove(tmpFile)
    deployOpts.Kubeconfig = tmpFile
}
```

**Specific changes**:
- `NewDeployCmd()` line 27: Add `cmd.Flags().String("mcp-url", "", "MCP server URL")`
- `runDeploy()` line 92-114: After resolving env config, extract `mcpURL` from flag or `env.MCPUrl`
- `runDeploy()` ~line 220: Before calling `deployWorkflow()`, add the credential issuance step
- `deployWorkflow()` remains unchanged -- it already accepts `Kubeconfig` in `InternalDeployOptions`

### 5. `tntc audit` -- Delegate to MCP security audit

**File**: `pkg/cli/audit.go`
**Function**: `RunE` closure (line 61)

**New flag**: `--mcp-url string` on the audit command

**Change**: When `--mcp-url` is set, call the MCP server's `audit_rbac`,
`audit_netpol`, and `audit_psa` tools instead of performing the local
NetworkPolicy/Secret/CronJob comparison.

```go
if mcpURL != "" {
    mcpClient := mcp.NewClient(mcpURL, mcpToken)
    rbacResult, _ := mcpClient.AuditRBAC(ctx, namespace)
    netpolResult, _ := mcpClient.AuditNetpol(ctx, namespace)
    psaResult, _ := mcpClient.AuditPSA(ctx, namespace)
    // Render combined security audit results
    // This is a DIFFERENT audit than the current contract-vs-deployed audit
}
```

Note: The MCP audit tools (`audit_rbac`, `audit_netpol`, `audit_psa`) provide
**security posture auditing**, which is complementary to but different from the
existing contract-vs-deployed audit. Consider adding a `--security` sub-flag or
making this a separate subcommand (`tntc audit security`).

**Specific changes**:
- `NewAuditCommand()` line 47: Add `cmd.Flags().String("mcp-url", "", "MCP server URL for security audit")`
- In the `RunE` closure: Add MCP branch before the existing contract audit logic

---

## Kubeconfig Handling

### Current State

The CLI handles kubeconfig through three paths in `pkg/k8s/client.go`:

| Function | File:Line | Behavior |
|----------|-----------|----------|
| `NewClient()` | `client.go:40` | In-cluster config, falls back to `KUBECONFIG` or `~/.kube/config` |
| `NewClientWithContext()` | `client.go:64` | Explicit kubeconfig context |
| `NewClientFromConfig()` | `client.go:130` | Explicit kubeconfig file + optional context |

### MCP-Issued Kubeconfigs

The MCP server's `cred_kubeconfig` tool returns a standard kubeconfig YAML with:
- A single cluster entry using the API server's CA and URL (from in-cluster config)
- A single user entry with a time-limited ServiceAccount token (via TokenRequest API)
- A single context entry binding the user to the cluster

This kubeconfig is **already compatible** with the existing `NewClientFromConfig()`
function. No changes are needed to `pkg/k8s/client.go`.

The CLI writes the MCP-issued kubeconfig to a temporary file and passes the path
via `InternalDeployOptions.Kubeconfig`, which is already wired into the deploy
flow at `deploy.go:375-376`.

### Token Lifetime

MCP-issued tokens have a TTL between 10 and 1440 minutes (configurable per call).
For deploy operations, a 60-minute TTL is recommended. The CLI should:
1. Issue the kubeconfig immediately before the deploy operation
2. Clean up the temp kubeconfig file after deployment completes
3. Not cache or reuse MCP-issued kubeconfigs across commands

---

## Skill Documentation Updates

### File: `tentacular-skill/references/`

The tentacular-skill documentation should be updated to reflect:

1. **New `--mcp-url` flag** on all cluster-interacting commands
2. **New `mcp_url` config field** in environment configuration
3. **Zero-admin-kubeconfig workflow**: when using `--mcp-url`, users no longer
   need admin kubeconfig access on their workstations
4. **Security audit capability**: the `tntc audit` command gains security posture
   auditing (RBAC, NetworkPolicy, PSA) when used with `--mcp-url`

### Skill Doc Changes

- Add `--mcp-url` to the flag reference for: `cluster check`, `cluster profile`,
  `cluster install`, `deploy`, `audit`
- Add `mcp_url` and `mcp_token` to the environment config reference
- Add a "Zero-Admin Deploy" workflow example showing:
  ```bash
  # No admin kubeconfig needed -- MCP issues scoped credentials
  tntc deploy --env staging --mcp-url http://tentacular-mcp.tentacular-system.svc:8080
  ```
- Document `TENTACULAR_MCP_URL` and `TENTACULAR_MCP_TOKEN` environment variables

---

## Backwards Compatibility

All changes are additive and opt-in:

| Change | Default | Backwards Compatible |
|--------|---------|---------------------|
| `--mcp-url` flags | Empty (not set) | Yes -- existing behavior when absent |
| `mcp_url` config field | Empty (not set) | Yes -- existing configs parse without it |
| `mcp_token` config field | Empty (not set) | Yes -- existing configs parse without it |
| `pkg/mcp/` package | New package, no imports from existing code paths | Yes -- no existing code affected |
| `EnvironmentConfig` struct | New optional fields with `omitempty` | Yes -- YAML unmarshaling ignores unknown fields |

### Guarantees

1. **No breaking changes**: Every existing command, flag, and config field
   continues to work exactly as before.
2. **No new dependencies in the default path**: The `pkg/mcp/` package is only
   imported when `--mcp-url` is provided. The `pkg/k8s/` package has no new
   dependencies.
3. **Config file forward compatibility**: Config files without `mcp_url` are
   valid. Config files with `mcp_url` work on older CLI versions (unknown fields
   are silently ignored by `gopkg.in/yaml.v3`).

---

## Migration Path

### For Existing Users (No MCP Server)

Nothing changes. All commands work exactly as before.

### For Users Adopting MCP Server

1. **Deploy tentacular-mcp** to the target cluster:
   ```bash
   kubectl apply -k deploy/manifests/
   ```

2. **Get the Bearer token** from the deployed Secret:
   ```bash
   kubectl get secret tentacular-mcp-token -n tentacular-system -o jsonpath='{.data.token}' | base64 -d > ~/.tentacular/mcp-tokens/staging.token
   ```

3. **Update environment config** to include `mcp_url`:
   ```yaml
   environments:
     staging:
       context: staging-cluster
       namespace: staging-workflows
       mcp_url: http://tentacular-mcp.tentacular-system.svc:8080
       mcp_token: file:~/.tentacular/mcp-tokens/staging.token
   ```

4. **Use port-forward for remote access** (when not in-cluster):
   ```bash
   kubectl port-forward -n tentacular-system svc/tentacular-mcp 8080:8080 &
   tntc cluster check --mcp-url http://localhost:8080
   ```

5. **Gradually migrate commands** to use `--mcp-url`:
   - Start with `cluster check` and `cluster profile` (read-only)
   - Move to `deploy` once comfortable with credential issuance
   - Add `audit` for security posture monitoring

### For CI/CD Pipelines

Use environment variables instead of config files:

```bash
export TENTACULAR_MCP_URL=http://tentacular-mcp.tentacular-system.svc:8080
export TENTACULAR_MCP_TOKEN=<token>
tntc deploy --env production
```

---

## Summary of Required Changes

### New Files

| File | Description |
|------|-------------|
| `pkg/mcp/client.go` | MCP client with typed methods for each relevant tool |
| `pkg/mcp/transport.go` | MCP Streamable HTTP transport layer |

### Modified Files

| File | Change |
|------|--------|
| `pkg/cli/environment.go:11` | Add `MCPUrl` and `MCPToken` fields to `EnvironmentConfig` |
| `pkg/cli/config.go:20` | Add `MCPUrl` and `MCPToken` fields to `TentacularConfig` |
| `pkg/cli/config.go:53` | Add merge logic for MCP fields in `mergeConfig()` |
| `pkg/cli/cluster.go:24` | Add `--mcp-url` flag to `check` command |
| `pkg/cli/cluster.go:27` | Add `--mcp-url` flag to `install` command |
| `pkg/cli/cluster.go:43` | Add MCP branch in `runClusterCheck()` |
| `pkg/cli/cluster.go:120` | Add MCP branch in `runClusterInstall()` |
| `pkg/cli/profile.go:49` | Add `--mcp-url` flag to `profile` command |
| `pkg/cli/profile.go:90` | Add MCP branch in `runProfileForEnv()` |
| `pkg/cli/deploy.go:27` | Add `--mcp-url` flag to `deploy` command |
| `pkg/cli/deploy.go:56` | Add pre-deploy credential issuance in `runDeploy()` |
| `pkg/cli/audit.go:47` | Add `--mcp-url` flag and MCP security audit branch |

### Unchanged Files

| File | Reason |
|------|--------|
| `pkg/k8s/client.go` | MCP-issued kubeconfigs are standard format; `NewClientFromConfig()` works as-is |
| `pkg/k8s/preflight.go` | Direct K8s preflight unchanged; MCP path uses `pkg/mcp/` |
| `pkg/k8s/profile.go` | Direct K8s profile unchanged; MCP path uses `pkg/mcp/` |
| `cmd/tntc/main.go` | No new top-level commands needed |

### MCP Tools Used by CLI

| CLI Command | MCP Tool(s) |
|-------------|-------------|
| `cluster check` | `cluster_preflight` |
| `cluster profile` | `cluster_profile` |
| `cluster install` | `wf_apply` |
| `deploy` | `cred_kubeconfig` |
| `audit` | `audit_rbac`, `audit_netpol`, `audit_psa` |
