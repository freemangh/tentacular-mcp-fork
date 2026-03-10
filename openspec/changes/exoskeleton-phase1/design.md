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

## Google SSO Domain Restriction

Keycloak's Google identity provider uses the `hostedDomain` parameter to restrict authentication to a single Google Workspace domain. This is configured in the Keycloak admin console under Identity Providers > Google > Hosted Domain.

## SPIRE ClusterRole Requirement

The MCP service account's ClusterRole must include permissions for `spire.spiffe.io` API group resources (specifically `clusterspiffeids`). The Helm chart includes this in the default ClusterRole. On existing clusters, the ClusterRole may need manual patching if it was deployed before this permission was added.

## Certificate Management

### Design decisions

1. **cert-manager internal CA for server certificates.** A `tentacular-internal-ca` CA Issuer in `tentacular-exoskeleton`, bootstrapped from a self-signed ClusterIssuer. The CA has 10-year validity with auto-renewal 1 year before expiry. The NATS server certificate (`nats-server-tls`) is issued by this CA with 1-year validity and 30-day auto-renewal.

2. **SPIRE for client SVIDs.** SPIRE issues X.509 SVIDs to workload pods through the Workload API (Unix socket on each node via DaemonSet). These SVIDs serve as client certificates for NATS mTLS.

3. **Why not use SPIRE as the cert-manager CA?** The SPIRE CA key is not exportable. SPIRE manages its own CA internally and does not expose the private key material. There is no supported way to use SPIRE as a cert-manager CA Issuer. Therefore, cert-manager has its own independent CA for server certs.

4. **Combined trust bundle.** NATS must verify both server-cert-chain trust (cert-manager CA) and client-cert trust (SPIRE CA). A combined PEM bundle containing both CA certificates is stored in the `nats-spire-ca` Secret and mounted into the NATS pod at `/etc/nats/spire-ca/ca.pem`.

5. **Rotation model:**
   - Server cert: fully automated by cert-manager.
   - Client SVIDs: fully automated by SPIRE.
   - SPIRE CA bundle sync to NATS: manual refresh of `nats-spire-ca` Secret when SPIRE rotates its CA. Known limitation -- future fix is a sidecar or CronJob.

### Resources created

| Resource | Kind | Namespace |
|----------|------|-----------|
| `tentacular-selfsigned-bootstrap` | ClusterIssuer | cluster-scoped |
| `tentacular-internal-ca` | Certificate | `tentacular-exoskeleton` |
| `tentacular-internal-ca` | Issuer | `tentacular-exoskeleton` |
| `nats-server-tls` | Certificate | `tentacular-exoskeleton` |
| `nats-spire-ca` | Secret (manual) | `tentacular-exoskeleton` |

## New Dependencies
- github.com/jackc/pgx/v5
- github.com/nats-io/nats.go
- github.com/minio/madmin-go/v3
- github.com/minio/minio-go/v7
