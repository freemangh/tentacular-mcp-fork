# Update README for Helm-first installation

## Why

The tentacular-mcp README currently documents installation as secondary to the
CLI's `tntc cluster install` command. Since Phase 1 removes `tntc cluster install`
from the CLI, the Helm chart becomes the primary (and only) installation method.

The README needs to be updated to reflect this: Helm installation is the
recommended and documented path. The old `tntc cluster install` references
should be removed or replaced with Helm instructions.

## What Changes

- **Update the Installation section** of `README.md` to lead with Helm:
  `helm install tentacular-mcp charts/tentacular-mcp --namespace tentacular-system --create-namespace`
- **Remove references to `tntc cluster install`** throughout the README.
- **Document Helm values** for common configuration (image, namespace, token,
  module proxy settings).
- **Add a "Post-Install" section** explaining how to configure the CLI to point
  at the installed MCP server (manual config.yaml setup or `tntc configure`).
- **Update the Quick Start** section to reflect the Helm-first flow:
  1. `helm install` the MCP server.
  2. `tntc configure` to set up the CLI.
  3. `tntc cluster check` to verify.

## Acceptance Criteria

- README.md installation instructions use Helm as the primary method.
- No references to `tntc cluster install` remain.
- Helm values for key configuration options are documented.
- The Quick Start flow is Helm-first.

## Non-goals

- Creating a published Helm chart repository (OCI, ChartMuseum, etc.).
- Modifying the Helm chart itself -- only the README documentation.
