# Design: Update README for Helm-first installation

## README Structure

### Current structure (approximate):
1. Overview
2. Features
3. Installation (references `tntc cluster install`)
4. Configuration
5. MCP Tools reference
6. Development

### Updated structure:
1. Overview
2. Features
3. **Installation** (Helm-first)
   - Prerequisites (kubectl, helm, cluster access)
   - Quick install
   - Configuration values
   - Verifying the installation
4. **CLI Configuration** (post-install)
   - Manual config.yaml setup
   - `tntc configure` flow
5. MCP Tools reference
6. Development

## Key Content Changes

### Installation section

Replace `tntc cluster install` instructions with:

```bash
# Add the Helm repo (if published)
# helm repo add tentacular https://...

# Or install from local chart
helm install tentacular-mcp charts/tentacular-mcp \
  --namespace tentacular-system \
  --create-namespace \
  --set image.tag=latest
```

### Helm Values Documentation

Document the key values.yaml overrides:
- `image.repository` / `image.tag`
- `namespace` (if configurable)
- `moduleProxy.enabled` / `moduleProxy.namespace`
- `auth.token` (or token generation method)

### Post-Install CLI Configuration

After Helm install, users need to configure their CLI:

```bash
# Option 1: tntc configure (interactive)
tntc configure

# Option 2: manual config
cat > ~/.tentacular/config.yaml <<EOF
mcp:
  endpoint: http://tentacular-mcp.tentacular-system.svc.cluster.local:8080
  token_path: ~/.tentacular/mcp-token
EOF
```

## Removed Content

- All references to `tntc cluster install`
- The "Bootstrap" section that described the CLI-based install flow
- Any mention of the CLI generating MCP server manifests
