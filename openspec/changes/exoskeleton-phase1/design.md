# Exoskeleton Phase 1 - Design (tentacular-mcp)

## Architecture

The exoskeleton adds a `pkg/exoskeleton/` package with these components:

```
pkg/exoskeleton/
  identity.go        - Identity compiler (namespace,workflow -> service identifiers)
  config.go          - Config loader from TENTACULAR_EXOSKELETON_* env vars
  registrar_postgres.go - Postgres role/schema provisioning via pgx
  registrar_nats.go  - NATS connectivity check + shared token creds
  registrar_rustfs.go - RustFS IAM user/policy via madmin-go
  injector.go        - K8s Secret manifest builder
  controller.go      - Orchestrator called by wf_apply/wf_remove
```

## Identity Model

```go
type Identity struct {
    Namespace  string
    Workflow   string
    Principal  string // spiffe://tentacular/ns/<ns>/tentacles/<wf>
    PgRole     string // tn_<ns>_<wf> (hyphens -> underscores)
    PgSchema   string // tn_<ns>_<wf>
    NATSUser   string // <ns>.<wf>
    NATSPrefix string // tentacular.<ns>.<wf>.>
    S3Prefix   string // ns/<ns>/tentacles/<wf>/
    S3User     string // tn_<ns>_<wf>
    S3Policy   string // tn_<ns>_<wf>_policy
}
```

Sanitization: replace `-` with `_` for Postgres/S3 identifiers, limit to 63 chars.

## Config

Loaded from environment at startup:
- `TENTACULAR_EXOSKELETON_ENABLED` - master toggle
- `TENTACULAR_EXOSKELETON_CLEANUP_ON_UNDEPLOY` - destructive cleanup on wf_remove
- `TENTACULAR_POSTGRES_ADMIN_{HOST,PORT,DATABASE,USER,PASSWORD}` - admin connection
- `TENTACULAR_NATS_{URL,TOKEN}` - NATS connection
- `TENTACULAR_RUSTFS_{ENDPOINT,ACCESS_KEY,SECRET_KEY,BUCKET,REGION}` - RustFS admin

## Controller Flow (ProcessManifests)

1. If disabled, return manifests unchanged
2. Find ConfigMap with workflow.yaml key
3. Parse contract.dependencies, filter for tentacular-* prefixed names
4. If none, return unchanged
5. Validate each backing service is enabled; fail fast if not
6. Run registrars: Postgres -> NATS -> RustFS
7. Enrich contract deps with host/port/user/auth fields
8. Rewrite ConfigMap with enriched workflow.yaml
9. Build exoskeleton Secret manifest, append to manifests
10. Return augmented manifests

## Secret Format

One key per service, JSON-encoded value:
```yaml
stringData:
  tentacular-postgres: '{"host":"...","port":"5432",...}'
  tentacular-nats: '{"url":"nats://...","token":"...",...}'
  tentacular-rustfs: '{"endpoint":"http://...","access_key":"...",...}'
```

## wf_apply Integration

`handleWorkflowApply` calls `controller.ProcessManifests()` before the existing apply loop. This enriches manifests in-place.

## wf_remove Integration

After deleting resources, detect exoskeleton Secret label and run unregistrars if `CleanupOnUndeploy` is true.

## SPIRE Registrar

The SPIRE registrar creates `ClusterSPIFFEID` custom resources that match workflow pods by namespace and release label. The SPIRE controller provisions X.509 SVIDs to matched pods automatically.

```go
// Register creates a ClusterSPIFFEID for the tentacle
func (r *SPIRERegistrar) Register(ctx context.Context, id Identity) error
// Unregister deletes the ClusterSPIFFEID
func (r *SPIRERegistrar) Unregister(ctx context.Context, id Identity) error
```

No credentials are returned. SPIRE provides identity only.

## NATS SPIFFE Mode

The NATS registrar supports dual-mode authentication:

### SPIFFE mode (`TENTACULAR_NATS_SPIFFE_ENABLED=true`)

- Uses mTLS with SPIRE-issued X.509 SVIDs for NATS authentication
- NATS server configured with `verify_and_map` to map SPIFFE URIs to authorization rules
- Authorization rules stored in a ConfigMap (`TENTACULAR_NATS_AUTHZ_CONFIGMAP` in `TENTACULAR_NATS_AUTHZ_NAMESPACE`)
- Each tentacle gets a ConfigMap entry mapping its SPIFFE URI to permitted publish/subscribe subjects
- Enforced per-tentacle isolation -- cryptographic, not convention-based

### Token mode (fallback)

- Shared bearer token, convention-only subject isolation
- Used when SPIRE is not available or SPIFFE mode is not enabled

### Config additions

```
TENTACULAR_NATS_SPIFFE_ENABLED      - Enable SPIFFE mTLS auth (default: false)
TENTACULAR_NATS_AUTHZ_CONFIGMAP     - ConfigMap name for authz rules (default: nats-tentacular-authz)
TENTACULAR_NATS_AUTHZ_NAMESPACE     - ConfigMap namespace (default: tentacular-exoskeleton)
```

### exo_status additions

`exo_status` now reports `spire_available` and `nats_spiffe_enabled` fields.

## New Dependencies
- github.com/jackc/pgx/v5
- github.com/nats-io/nats.go
- github.com/minio/madmin-go/v3
- github.com/minio/minio-go/v7
