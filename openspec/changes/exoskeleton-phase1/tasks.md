# Exoskeleton Phase 1 - Tasks (tentacular-mcp)

## Step 1: Identity Compiler
- [x] Create `pkg/exoskeleton/identity.go` with `CompileIdentity(namespace, workflow string) Identity`
- [x] Sanitize: replace `-` with `_` for Postgres/S3, limit 63 chars
- [x] Create `pkg/exoskeleton/identity_test.go` with table-driven tests for edge cases

## Step 2: Config Loader
- [x] Create `pkg/exoskeleton/config.go` with `LoadFromEnv() *Config`
- [x] Per-service enabled helpers: `PostgresEnabled()`, `NATSEnabled()`, `RustFSEnabled()`
- [x] Create `pkg/exoskeleton/config_test.go`

## Step 3: Postgres Registrar
- [x] Create `pkg/exoskeleton/registrar_postgres.go`
- [x] `Register(ctx, id)` -> create role, schema, grants, return creds
- [x] `Unregister(ctx, id)` -> revoke, drop schema cascade, drop role
- [x] Create `pkg/exoskeleton/registrar_postgres_test.go` (mocked)

## Step 4: NATS Registrar
- [x] Create `pkg/exoskeleton/registrar_nats.go`
- [x] `Register(ctx, id)` -> return shared token + subject prefix
- [x] `Unregister(ctx, id)` -> no-op
- [x] Create `pkg/exoskeleton/registrar_nats_test.go` (mocked)

## Step 5: RustFS Registrar
- [x] Create `pkg/exoskeleton/registrar_rustfs.go`
- [x] `Register(ctx, id)` -> create bucket, IAM user, policy, return creds
- [x] `Unregister(ctx, id)` -> remove objects, policy, user
- [x] Create `pkg/exoskeleton/registrar_rustfs_test.go` (mocked)

## Step 6: Credential Injector
- [x] Create `pkg/exoskeleton/injector.go` with `BuildSecretManifest()`
- [x] JSON-encoded per-service keys, correct labels
- [x] Create `pkg/exoskeleton/injector_test.go`

## Step 7: Controller + wf_apply Integration
- [x] Create `pkg/exoskeleton/controller.go` with `ProcessManifests()`
- [x] Modify `pkg/tools/deploy.go` to call ProcessManifests before apply loop
- [x] Modify `pkg/tools/deploy.go` wf_remove to call unregistrars
- [x] Create `pkg/exoskeleton/controller_test.go`

## Step 8: Guard Update
- [x] Add `"tentacular-exoskeleton": true` to systemNamespaces in `pkg/guard/guard.go`

## Step 9: MCP Tools
- [x] Create `pkg/tools/exoskeleton.go` with exo_status and exo_registration tools
- [x] Wire into `pkg/tools/register.go`

## Step 10: Helm Chart
- [x] Add `exoskeleton:` block to `charts/tentacular-mcp/values.yaml`
- [x] Update `charts/tentacular-mcp/templates/deployment.yaml` for conditional env vars
- [x] Create `charts/tentacular-mcp/templates/exoskeleton-secret.yaml`

## Step 11: Wire Config into Server
- [x] Update `cmd/tentacular-mcp/main.go` to load exoskeleton config
- [x] Pass config through `server.New()` -> `tools.RegisterAll()`
- [x] Update `pkg/server/server.go` and `pkg/tools/register.go` signatures

## Step 12: Go Dependencies
- [x] Add pgx, nats.go, madmin-go, minio-go to go.mod
- [x] Run go mod tidy

## Step 13: SPIRE Registrar
- [x] Create `pkg/exoskeleton/registrar_spire.go` with ClusterSPIFFEID management
- [x] `Register(ctx, id)` -> create ClusterSPIFFEID custom resource
- [x] `Unregister(ctx, id)` -> delete ClusterSPIFFEID custom resource
- [x] Wire into controller registration lifecycle

## Step 14: NATS SPIFFE Mode
- [x] Add dual-mode auth to NATS registrar (SPIFFE mTLS or shared token)
- [x] SPIFFE mode: manage authorization entries in ConfigMap (`TENTACULAR_NATS_AUTHZ_CONFIGMAP`)
- [x] Map SPIFFE URIs to per-tentacle publish/subscribe permissions
- [x] Token mode: preserve existing shared-token behavior as fallback
- [x] Add `TENTACULAR_NATS_SPIFFE_ENABLED`, `TENTACULAR_NATS_AUTHZ_CONFIGMAP`, `TENTACULAR_NATS_AUTHZ_NAMESPACE` env vars
- [x] Update config loader and Helm chart values
- [x] Update `exo_status` to report `spire_available` and `nats_spiffe_enabled`

## Step 15: Google SSO Domain Restriction
- [x] Configure Keycloak Google IdP `hostedDomain` to restrict authentication to allowed domain
- [x] Verify only allowed-domain Google accounts can authenticate

## Step 16: SPIRE ClusterRole Fix
- [x] Add `spire.spiffe.io/clusterspiffeids` permissions to MCP service account ClusterRole
- [x] Patch live cluster ClusterRole to match chart definition
- [x] Verify ClusterSPIFFEID creation succeeds with updated permissions

## Step 17: Full Workspace E2E Validation
- [x] Postgres: CREATE TABLE, INSERT, SELECT in scoped schema
- [x] RustFS: PUT/GET inside prefix succeeds, PUT outside prefix denied by IAM policy
- [x] NATS: Publish to scoped subject (token mode)
- [x] SPIRE: ClusterSPIFFEID created, workload identity registered
- [x] Deployer annotation verified
