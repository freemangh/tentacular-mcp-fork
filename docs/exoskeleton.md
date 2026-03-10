# Exoskeleton Reference

> **Note:** The architecture diagram is at `docs/diagrams/exoskeleton-architecture.svg`.

## 1. Overview

The exoskeleton is an optional, feature-flagged extension to Tentacular that provisions a scoped workspace for each tentacle. A workspace is a deterministic bundle of backing-service resources -- a Postgres schema, a NATS subject prefix, and an S3 object prefix -- derived from the tentacle's identity (namespace + workflow name). When a workflow declares `tentacular-*` dependencies in its contract, the MCP server automatically registers the tentacle with each backing service, provisions least-privilege credentials, and injects them as a Kubernetes Secret. The workspace persists across redeploys (credentials rotate, data is preserved) and is destroyed only on explicit undeploy with cleanup enabled. Authentication is dual-mode: existing bearer tokens continue to work, while optional Keycloak SSO (brokered to Google) adds deployer provenance -- every deployment is attributed to the person who initiated it. SPIRE provides cryptographic workload identity for each tentacle, enabling NATS mTLS isolation via SPIFFE when configured, and establishing the foundation for future Vault-based secret management.

---

## 2. Architecture

### 2.1 Namespace layout

| Namespace | Role | Contents |
|-----------|------|----------|
| `tentacular-system` | Control plane | MCP server, SPIRE server + agent |
| `tentacular-exoskeleton` | Data plane | Postgres, NATS, RustFS, Keycloak, cert-manager internal CA |
| `tentacular-support` | Dev tooling | esm-sh module proxy, shared utilities |
| `tent-*` | Workloads | Tentacle workflow deployments |

**Design rationale:**

- SPIRE belongs in `tentacular-system`. It runs a DaemonSet with host-level access, does TokenReview against the K8s API, and manages workload identity. Its access pattern matches the MCP server, not the passive data services.
- Data-plane services share `tentacular-exoskeleton` with per-service ServiceAccount lockdown. Each service gets its own SA with `automountServiceAccountToken: false` and no RBAC grants.
- NetworkPolicies restrict `tentacular-exoskeleton` inbound to `tentacular-system` + workflow namespaces. No inbound to `tentacular-system` except the MCP endpoint.

### 2.2 Component overview

| Component | Package | Responsibility |
|-----------|---------|----------------|
| Identity compiler | `pkg/exoskeleton/identity.go` | (namespace, workflow) to deterministic identifiers |
| Postgres registrar | `pkg/exoskeleton/registrar_postgres.go` | Role, schema, grants, cleanup |
| NATS registrar | `pkg/exoskeleton/registrar_nats.go` | Dual-mode auth (SPIFFE mTLS or shared token) + scoped subject prefix |
| RustFS registrar | `pkg/exoskeleton/registrar_rustfs.go` | IAM user, prefix-scoped policy |
| SPIRE registrar | `pkg/exoskeleton/registrar_spire.go` | ClusterSPIFFEID resource via K8s API |
| Controller | `pkg/exoskeleton/controller.go` | Orchestrates registration lifecycle |
| Credential injector | `pkg/exoskeleton/injector.go` | K8s Secret with `<dep>.<field>` keys |
| Contract enrichment | `pkg/exoskeleton/enrich.go` | Fills host/port/user in ConfigMap, patches --allow-net |
| Auth (OIDC) | `pkg/exoskeleton/auth.go` | Bearer token + Keycloak SSO validation |
| Config | `pkg/exoskeleton/config.go` | Env var loading, feature-flag evaluation |

### 2.3 Data flow

```
CLI (tntc deploy)
  → optional SSO login (tntc login → Keycloak device auth)
  → MCP server (wf_apply)
    → ExoskeletonController
      → IdentityCompiler: (namespace, workflow) → Identity struct
      → Registrars: Postgres, NATS, RustFS, SPIRE (each enabled independently)
      → CredentialInjector: writes K8s Secret in workflow namespace
      → ContractEnrichment: fills ConfigMap, patches Deployment
    → Deploy engine pod
```

---

## 3. Identity Model

All identifiers are derived deterministically from `(namespace, workflow)` by `CompileIdentity()`.

### 3.1 Canonical principal

```
spiffe://tentacular/ns/<namespace>/tentacles/<workflow>
```

This URI is the source identity regardless of whether SPIRE is enabled.

### 3.2 Service-specific mappings

| Service | Identifier | Pattern | Example (`tent-dev`, `hn-digest`) |
|---------|------------|---------|-----------------------------------|
| SPIFFE | Principal | `spiffe://tentacular/ns/<ns>/tentacles/<wf>` | `spiffe://tentacular/ns/tent-dev/tentacles/hn-digest` |
| Postgres | Role | `tn_<ns>_<wf>` | `tn_tent_dev_hn_digest` |
| Postgres | Schema | `tn_<ns>_<wf>` | `tn_tent_dev_hn_digest` |
| NATS | User | `tentacle.<ns>.<wf>` | `tentacle.tent-dev.hn-digest` |
| NATS | Subject prefix | `tentacular.<ns>.<wf>.>` | `tentacular.tent-dev.hn-digest.>` |
| S3 | Object prefix | `ns/<ns>/tentacles/<wf>/` | `ns/tent-dev/tentacles/hn-digest/` |
| S3 | IAM user | `tn_<ns>_<wf>` | `tn_tent_dev_hn_digest` |
| S3 | Policy | `tn_<ns>_<wf>_policy` | `tn_tent_dev_hn_digest_policy` |

### 3.3 Sanitization rules

- Hyphens are replaced with underscores for Postgres and S3 IAM identifiers.
- All Postgres identifiers are lowercased and stripped of characters outside `[a-z0-9_]`.
- Postgres identifiers are truncated to 63 characters. If truncation is needed, a `_<8-hex-sha256>` suffix preserves uniqueness.
- NATS and S3 prefix identifiers preserve hyphens (they are valid in those systems).

### 3.4 Deployer identity

When SSO auth is active, deployer identity is recorded as Kubernetes annotations on the Deployment:

| Annotation | Value |
|------------|-------|
| `tentacular.io/deployed-by` | Deployer email (e.g., `user@example.com`) |
| `tentacular.io/deployed-at` | ISO 8601 timestamp |
| `tentacular.io/deployed-via` | Agent type (`cli`, `slack-bot`, `web-ui`) |

---

## 4. Services

### 4.1 Postgres

**Registration** creates a role and schema in the shared `tentacular` database:

1. `CREATE ROLE tn_<ns>_<wf> WITH LOGIN PASSWORD '<generated>'`
2. `CREATE SCHEMA tn_<ns>_<wf> AUTHORIZATION tn_<ns>_<wf>`
3. `GRANT USAGE ON SCHEMA ... TO role`
4. `ALTER DEFAULT PRIVILEGES IN SCHEMA ... GRANT ALL ON TABLES TO role`

**Re-registration** rotates the password. The schema and all data are preserved. Privileges are verified.

**Unregistration** (cleanup enabled):

1. `DROP SCHEMA tn_<ns>_<wf> CASCADE`
2. `DROP ROLE tn_<ns>_<wf>`

**Admin requirements:** The MCP server's admin role needs `CREATEROLE` privilege. `SUPERUSER` is not required.

**Auth model:** Password-based (Phase 1). Future: Vault-managed short-lived credentials.

### 4.2 NATS

**Registration** provisions a scoped subject prefix for the tentacle. The NATS user is `tentacle.<ns>.<wf>` with publish/subscribe on `tentacular.<ns>.<wf>.>`. Two authentication modes are supported:

| Mode | Config | Auth mechanism | Isolation |
|------|--------|----------------|-----------|
| **SPIFFE** (preferred) | `TENTACULAR_NATS_SPIFFE_ENABLED=true` | mTLS with X.509 SVIDs; NATS `verify_and_map` maps SPIFFE URIs to authorization rules | Enforced per-tentacle subject isolation via NATS authorization ConfigMap |
| **Token** (active default) | `TENTACULAR_NATS_TOKEN` set, SPIFFE not enabled | Shared bearer token | Convention-only subject isolation |

> **Note:** SPIFFE mode is fully implemented, tested, and deployed on the `eastus-dev` cluster. The NATS server TLS certificate is issued by a cert-manager internal CA (`tentacular-internal-ca`), with 1-year validity and automatic renewal 30 days before expiry. NATS trusts both the cert-manager CA (for its own server cert chain) and the SPIRE CA (for client SVIDs) via a combined trust bundle. See [NATS SPIFFE mTLS Activation Guide](nats-spiffe-deployment.md) for the full deployment procedure.

**SPIFFE mode:** When enabled, the NATS registrar creates an authorization entry in a ConfigMap (name configured via `TENTACULAR_NATS_AUTHZ_CONFIGMAP`, namespace via `TENTACULAR_NATS_AUTHZ_NAMESPACE`). Each entry maps a tentacle's SPIFFE URI to its permitted publish/subscribe subjects. NATS uses `verify_and_map` with the SPIRE trust bundle to authenticate client certificates and enforce subject-level permissions. This provides cryptographically enforced isolation -- a tentacle cannot publish to another tentacle's subjects.

**Token mode:** Fallback for clusters without SPIRE. All tentacles share the same NATS token. Subject isolation is convention-only. A tentacle can technically publish to another tentacle's subject prefix.

**Re-registration** preserves the same subject scope and any durable JetStream state. In SPIFFE mode, the authorization ConfigMap entry is updated.

**Unregistration** revokes the tentacle's credentials and auth artifacts. In SPIFFE mode, the authorization ConfigMap entry is removed.

### 4.3 RustFS

**Registration** creates a per-tentacle IAM user with a prefix-scoped policy:

1. Ensure `tentacular` bucket exists (S3 API via `minio-go/v7`)
2. Create IAM policy scoped to `ns/<ns>/tentacles/<wf>/` prefix
   - Actions: `s3:GetObject`, `s3:PutObject`, `s3:DeleteObject`, `s3:ListBucket` (with prefix condition)
3. Create IAM user `tn_<ns>_<wf>` and attach the policy

**API:** Uses RustFS's native admin API at `/rustfs/admin/v3/` (not MinIO's `/minio/admin/v3/`). The `madmin-go` library does not work because it hardcodes the `/minio/` path prefix. Admin operations use AWS SigV4 HTTP signing. Data-plane operations use the standard S3 API.

**Re-registration** preserves all objects under the prefix. Credentials are reissued.

**Unregistration** (cleanup enabled):

1. Recursively delete objects under the tentacle prefix
2. Remove IAM policy and user

**Known quirk:** RustFS alpha returns HTTP 500 (not 404) when querying a non-existent user or policy. The registrar handles this.

### 4.4 SPIRE

**Registration** creates a `ClusterSPIFFEID` custom resource that matches workflow pods by namespace and release label. The SPIRE controller automatically provisions X.509 SVIDs to matched pods.

```yaml
apiVersion: spire.spiffe.io/v1alpha1
kind: ClusterSPIFFEID
metadata:
  name: tentacle-<ns>-<wf>
spec:
  spiffeIDTemplate: "spiffe://tentacular/ns/{{ .PodMeta.Namespace }}/tentacles/{{ .PodMeta.Labels.tentacular.io/release }}"
  podSelector:
    matchLabels:
      tentacular.io/release: <workflow>
  namespaceSelector:
    matchLabels:
      kubernetes.io/metadata.name: <namespace>
```

**No credentials are returned** from the SPIRE registrar. SPIRE provides identity only, not service credentials.

**NATS integration:** When SPIFFE mode is enabled for NATS (`TENTACULAR_NATS_SPIFFE_ENABLED=true`), SPIRE SVIDs provide the client certificates used for mTLS authentication against the NATS server. The NATS `verify_and_map` directive maps the SPIFFE URI from the certificate SAN to per-tentacle authorization rules.

**Foundation for:**
- Vault authentication (SVIDs as auth credentials)
- NATS mTLS via SPIFFE (built -- see Section 4.2)
- Istio ambient mode (SPIFFE IDs as service mesh identity)

---

## 5. Authentication

### 5.1 Dual auth model

The MCP server supports two auth modes simultaneously:

| Mode | When | How |
|------|------|-----|
| Bearer token | Always available | `Authorization: Bearer <token>` header, configured via `TENTACULAR_MCP_TOKEN` |
| OIDC (Keycloak SSO) | When `TENTACULAR_EXOSKELETON_AUTH_ENABLED=true` | Keycloak-issued access token, brokered to Google SSO |

Both modes coexist. Bearer tokens work for automation and CI/CD. OIDC adds human identity attribution for interactive deployments.

### 5.5 Google SSO domain restriction

The Keycloak Google identity provider is configured with a `hostedDomain` parameter that restricts authentication to a specific Google Workspace domain. Only Google accounts belonging to the configured domain can authenticate. Administrators set the allowed domain in the Keycloak Google IdP configuration (Identity Providers > Google > `Hosted Domain` field).

### 5.2 CLI authentication flow

1. `tntc login` initiates the OAuth 2.0 Device Authorization Grant
2. CLI sends `POST /auth/device` to the MCP server
3. MCP proxies to Keycloak's device authorization endpoint
4. CLI receives `device_code`, `user_code`, `verification_uri`
5. CLI opens browser to `verification_uri` (prints URI if browser launch fails)
6. User enters `user_code` and authenticates via Google SSO through Keycloak
7. CLI polls the MCP server's token endpoint at the specified interval
8. On success, CLI stores access + refresh tokens at `~/.tentacular/auth-token`
9. `tntc whoami` displays the authenticated identity (email, issuer, expiry)
10. `tntc logout` clears local tokens

**Token refresh:** The CLI checks token expiry before each request. If the access token is expired (or within 30 seconds of expiry), the CLI uses the refresh token automatically. If the refresh token is also expired, the CLI prompts `tntc login`.

### 5.3 Keycloak azp vs aud quirk

Keycloak access tokens use the `azp` (authorized party) claim instead of `aud` (audience) for the client ID. The MCP server's OIDC validator skips the `aud` check and validates `azp` against the configured client ID.

### 5.4 Keycloak client requirements

- Realm: `tentacular`
- Client: `tentacular-mcp` (confidential)
- OAuth 2.0 Device Authorization Grant: enabled
- Scopes: `openid`, `profile`, `email`
- Redirect URI: `https://mcp.<cluster>.<domain>/*`
- Google configured as upstream identity provider

---

## 6. Data Lifecycle

| Event | K8s resources | Backing-service data | Credentials |
|-------|---------------|---------------------|-------------|
| **Deploy** (first) | Created | Created (empty) | Generated |
| **Redeploy** | Replaced | Preserved | Rotated |
| **Undeploy** (cleanup OFF, default) | Deleted | Preserved | Revoked |
| **Undeploy** (cleanup ON) | Deleted | Destroyed | Revoked |
| **Redeploy after cleanup** | Created | Created (empty) | Generated |

### Cleanup behavior

`cleanup_on_undeploy` defaults to `false`. When disabled, `wf_remove` deletes Kubernetes resources (Deployment, ConfigMap, Secret) but leaves all backing-service state intact. A subsequent deploy of the same workflow reconnects to existing data.

When cleanup is enabled, unregistration permanently destroys:

- **Postgres:** `DROP SCHEMA CASCADE` -- all tables, views, functions, data. Role dropped.
- **RustFS:** All objects under the tentacle prefix recursively deleted. IAM policy and user removed.
- **NATS:** Credentials and auth artifacts revoked.
- **SPIRE:** ClusterSPIFFEID resource deleted.

### CLI undeploy confirmation

When a user runs `tntc undeploy <workflow>` and cleanup is enabled, the CLI warns about data loss and requires explicit confirmation. The `--force` flag skips the prompt for automation.

---

## 7. Configuration

### 7.1 Environment variables

#### Feature flags

| Variable | Default | Description |
|----------|---------|-------------|
| `TENTACULAR_EXOSKELETON_ENABLED` | `false` | Enable the exoskeleton control plane |
| `TENTACULAR_EXOSKELETON_AUTH_ENABLED` | `false` | Enable OIDC authentication (Keycloak SSO) |
| `TENTACULAR_EXOSKELETON_SPIRE_ENABLED` | `false` | Enable SPIRE workload identity registration |
| `TENTACULAR_EXOSKELETON_CLEANUP_ON_UNDEPLOY` | `false` | Destroy backing-service data on undeploy |

Per-service enablement is implicit: a service is enabled when the exoskeleton is enabled AND the service's required credentials are configured.

| Service | Required env vars for enablement |
|---------|--------------------------------|
| Postgres | `TENTACULAR_POSTGRES_ADMIN_HOST`, `TENTACULAR_POSTGRES_ADMIN_USER`, `TENTACULAR_POSTGRES_ADMIN_PASSWORD` |
| NATS | `TENTACULAR_NATS_URL` |
| RustFS | `TENTACULAR_RUSTFS_ENDPOINT`, `TENTACULAR_RUSTFS_ACCESS_KEY`, `TENTACULAR_RUSTFS_SECRET_KEY` |
| SPIRE | `TENTACULAR_EXOSKELETON_SPIRE_ENABLED=true` |

#### Postgres

| Variable | Default | Description |
|----------|---------|-------------|
| `TENTACULAR_POSTGRES_ADMIN_HOST` | (none) | Postgres hostname |
| `TENTACULAR_POSTGRES_ADMIN_PORT` | `5432` | Postgres port |
| `TENTACULAR_POSTGRES_ADMIN_DATABASE` | `tentacular` | Database name |
| `TENTACULAR_POSTGRES_ADMIN_USER` | (none) | Admin role (needs CREATEROLE) |
| `TENTACULAR_POSTGRES_ADMIN_PASSWORD` | (none) | Admin password |
| `TENTACULAR_POSTGRES_SSLMODE` | `disable` | SSL mode |

#### NATS

| Variable | Default | Description |
|----------|---------|-------------|
| `TENTACULAR_NATS_URL` | (none) | NATS server URL |
| `TENTACULAR_NATS_TOKEN` | (none) | Shared auth token (token mode) |
| `TENTACULAR_NATS_SPIFFE_ENABLED` | `false` | Enable SPIFFE mTLS auth for NATS |
| `TENTACULAR_NATS_AUTHZ_CONFIGMAP` | `nats-tentacular-authz` | ConfigMap name for NATS authorization rules (SPIFFE mode) |
| `TENTACULAR_NATS_AUTHZ_NAMESPACE` | `tentacular-exoskeleton` | Namespace of the NATS authorization ConfigMap (SPIFFE mode) |

#### RustFS

| Variable | Default | Description |
|----------|---------|-------------|
| `TENTACULAR_RUSTFS_ENDPOINT` | (none) | RustFS S3 endpoint |
| `TENTACULAR_RUSTFS_ACCESS_KEY` | (none) | Admin access key |
| `TENTACULAR_RUSTFS_SECRET_KEY` | (none) | Admin secret key |
| `TENTACULAR_RUSTFS_BUCKET` | `tentacular` | Shared bucket name |
| `TENTACULAR_RUSTFS_REGION` | `us-east-1` | S3 region |

#### OIDC / Keycloak

| Variable | Default | Description |
|----------|---------|-------------|
| `TENTACULAR_KEYCLOAK_ISSUER` | (none) | OIDC issuer URL |
| `TENTACULAR_KEYCLOAK_CLIENT_ID` | (none) | OIDC client ID |
| `TENTACULAR_KEYCLOAK_CLIENT_SECRET` | (none) | OIDC client secret |

#### SPIRE

| Variable | Default | Description |
|----------|---------|-------------|
| `TENTACULAR_SPIRE_CLASS_NAME` | `tentacular-system-spire` | ClusterSPIFFEID class name |

### 7.2 Helm values

The `tentacular-mcp` Helm chart exposes two config sections:

```yaml
exoskeleton:
  # -- Enable the exoskeleton control plane
  enabled: false
  # -- Delete backing-service data on undeploy
  cleanupOnUndeploy: false
  postgres:
    existingSecret: ""     # Secret with keys: host, port, database, user, password
    host: ""
    port: "5432"
    database: "tentacular"
    user: ""
    password: ""
    sslMode: "disable"
  nats:
    existingSecret: ""     # Secret with keys: url, token
    url: ""
    token: ""
    spiffe:
      enabled: false       # Enable SPIFFE mTLS auth for NATS
      authzConfigMap: "nats-tentacular-authz"
      authzNamespace: "tentacular-exoskeleton"
  rustfs:
    existingSecret: ""     # Secret with keys: endpoint, access_key, secret_key, bucket, region
    endpoint: ""
    accessKey: ""
    secretKey: ""
    bucket: "tentacular"
    region: "us-east-1"

exoskeletonAuth:
  enabled: false
  issuerURL: ""
  clientID: ""
  clientSecret: ""
  existingSecret: ""       # Secret with keys: issuer-url, client-id, client-secret
```

Each service supports `existingSecret` for production use (reference a pre-created K8s Secret) or inline values for development.

### 7.3 Configuration profiles

#### Simple mode

```env
TENTACULAR_EXOSKELETON_ENABLED=false
```

Existing bearer-token auth. No service registration. No Keycloak/SPIRE dependency.

#### Exoskeleton without SSO

```env
TENTACULAR_EXOSKELETON_ENABLED=true
TENTACULAR_EXOSKELETON_AUTH_ENABLED=false
TENTACULAR_EXOSKELETON_CLEANUP_ON_UNDEPLOY=true
TENTACULAR_POSTGRES_ADMIN_HOST=postgres.tentacular-exoskeleton.svc.cluster.local
TENTACULAR_POSTGRES_ADMIN_USER=tentacular_admin
TENTACULAR_POSTGRES_ADMIN_PASSWORD=<secret>
TENTACULAR_NATS_URL=nats://nats.tentacular-exoskeleton.svc.cluster.local:4222
TENTACULAR_NATS_TOKEN=<secret>
TENTACULAR_RUSTFS_ENDPOINT=http://rustfs-svc.tentacular-exoskeleton.svc.cluster.local:9000
TENTACULAR_RUSTFS_ACCESS_KEY=<secret>
TENTACULAR_RUSTFS_SECRET_KEY=<secret>
```

Automatic registration into backing services. Bearer-token auth. No deployer identity.

#### Full exoskeleton with SSO

Add to the above:

```env
TENTACULAR_EXOSKELETON_AUTH_ENABLED=true
TENTACULAR_KEYCLOAK_ISSUER=https://auth.<cluster>.<domain>/realms/tentacular
TENTACULAR_KEYCLOAK_CLIENT_ID=tentacular-mcp
TENTACULAR_KEYCLOAK_CLIENT_SECRET=<secret>
TENTACULAR_EXOSKELETON_SPIRE_ENABLED=true
```

---

## 8. Deployment Guide

### 8.1 Prerequisites

| Service | Requirement |
|---------|-------------|
| Postgres | Dedicated instance, `tentacular` database, admin role with `CREATEROLE` |
| NATS | Dedicated instance, token auth, optional JetStream |
| RustFS | Dedicated instance, `tentacular` bucket pre-created |
| SPIRE | Server + agent in `tentacular-system`, trust domain `tentacular`. The MCP service account's ClusterRole must include permissions for `spire.spiffe.io` resources (e.g., `clusterspiffeids`). The Helm chart includes this, but verify on live clusters. |
| Keycloak | Realm `tentacular`, confidential client `tentacular-mcp` with device auth |
| Ingress + TLS | Required for Keycloak SSO (Google rejects non-HTTPS redirect URIs) |

### 8.2 Deployment order

1. Ingress controller + cert-manager (if SSO needed)
2. Postgres
3. NATS
4. RustFS
5. Keycloak (requires ingress + TLS for Google SSO)
6. SPIRE
7. `tentacular-mcp` with exoskeleton feature flags

Each service is independently optional. The MCP server gracefully handles disabled services: workflows that declare dependencies on a disabled service fail at deploy time with a clear error.

### 8.3 Bootstrap SQL for Postgres

```sql
CREATE DATABASE tentacular;
CREATE ROLE tentacular_admin WITH LOGIN PASSWORD '<strong-password>' CREATEROLE;
GRANT ALL PRIVILEGES ON DATABASE tentacular TO tentacular_admin;
```

### 8.4 Google SSO setup in Keycloak

1. **Google Cloud Console:** Create an OAuth 2.0 Client ID (Web application). Add redirect URI: `https://auth.<cluster>.<domain>/realms/tentacular/broker/google/endpoint`
2. **Keycloak admin:** In the `tentacular` realm, add Google as an identity provider. Set Client ID and Client Secret. Enable `Trust Email`. Ensure `email`, `name`, `sub` claims are mapped.
3. **Restrict SSO domain:** In the Google IdP configuration, set the `Hosted Domain` parameter to the allowed Google Workspace domain (e.g., `mirantis.com`). This ensures only accounts from that domain can authenticate.
4. **Verify:** Visit `https://auth.<cluster>.<domain>/realms/tentacular/account/` and sign in with a Google account from the allowed domain.

### 8.5 Helm install

```bash
helm upgrade --install tentacular-mcp charts/tentacular-mcp/ \
  -n tentacular-system \
  --set exoskeleton.enabled=true \
  --set exoskeleton.postgres.existingSecret=tentacular-mcp-postgres-admin \
  --set exoskeleton.nats.existingSecret=tentacular-mcp-nats-admin \
  --set exoskeleton.rustfs.existingSecret=tentacular-mcp-rustfs-admin \
  --set exoskeletonAuth.enabled=true \
  --set exoskeletonAuth.existingSecret=tentacular-mcp-keycloak-client
```

### 8.6 Dev build and deploy cycle

```bash
cd tentacular-mcp
make login                    # GHCR auth (if needed)
make dev-release              # builds ghcr.io/randybias/tentacular-mcp:dev-<sha>, pushes

helm upgrade tentacular-mcp charts/tentacular-mcp/ \
  -n tentacular-system \
  --set image.tag=dev-$(git rev-parse --short HEAD) \
  --set image.pullPolicy=Always \
  --reuse-values \
  --kubeconfig ~/dev-secrets/kubeconfigs/eastus-admin.kubeconfig

kubectl -n tentacular-system rollout status deploy/tentacular-mcp \
  --kubeconfig ~/dev-secrets/kubeconfigs/eastus-admin.kubeconfig
```

### 8.7 Service endpoints (eastus-dev cluster)

| Service | In-cluster endpoint |
|---------|---------------------|
| Postgres | `postgres-postgresql.tentacular-exoskeleton.svc.cluster.local:5432` |
| NATS | `nats://nats.tentacular-exoskeleton.svc.cluster.local:4222` |
| RustFS S3 | `http://rustfs-svc.tentacular-exoskeleton.svc.cluster.local:9000` |
| RustFS Console | `http://rustfs-svc.tentacular-exoskeleton.svc.cluster.local:9001` |
| Keycloak | `http://keycloak.tentacular-exoskeleton.svc.cluster.local:8080` |
| SPIRE Server | `spire-server.tentacular-system.svc.cluster.local:8081` |
| MCP (external) | `https://mcp.eastus-dev1.ospo-dev.miralabs.dev` |
| Keycloak (external) | `https://auth.eastus-dev1.ospo-dev.miralabs.dev` |

---

## 9. Workflow Integration

### 9.1 Declaring exoskeleton dependencies

Workflows opt in via the existing `contract.dependencies` mechanism:

```yaml
contract:
  dependencies:
    tentacular-postgres:
      protocol: postgresql
    tentacular-nats:
      protocol: nats
    tentacular-rustfs:
      protocol: s3
```

The MCP server treats any `tentacular-` prefixed dependency as an exoskeleton service request. If the service is not enabled, deployment fails with a clear error.

A workflow with no `tentacular-*` dependencies deploys identically to simple mode.

### 9.2 Credential injection

The MCP server writes a K8s Secret named `tentacular-exoskeleton-<workflow>` in the workflow namespace with flat `<dep>.<field>` keys:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: tentacular-exoskeleton-hn-digest
  namespace: tent-dev
  labels:
    tentacular.io/release: hn-digest
    tentacular.io/exoskeleton: "true"
type: Opaque
stringData:
  tentacular-postgres.host: postgres-postgresql.tentacular-exoskeleton.svc.cluster.local
  tentacular-postgres.port: "5432"
  tentacular-postgres.database: tentacular
  tentacular-postgres.user: tn_tent_dev_hn_digest
  tentacular-postgres.password: <generated>
  tentacular-postgres.schema: tn_tent_dev_hn_digest
  tentacular-postgres.protocol: postgresql
  tentacular-nats.url: nats://nats.tentacular-exoskeleton.svc.cluster.local:4222
  tentacular-nats.creds: <shared token>
  tentacular-nats.protocol: nats
  tentacular-rustfs.endpoint: http://rustfs-svc.tentacular-exoskeleton.svc.cluster.local:9000
  tentacular-rustfs.access_key: <scoped>
  tentacular-rustfs.secret_key: <scoped>
  tentacular-rustfs.bucket: tentacular
  tentacular-rustfs.prefix: ns/tent-dev/tentacles/hn-digest/
  tentacular-rustfs.protocol: s3
  tentacular-identity.principal: spiffe://tentacular/ns/tent-dev/tentacles/hn-digest
  tentacular-identity.namespace: tent-dev
  tentacular-identity.workflow: hn-digest
```

### 9.3 Contract enrichment

The MCP server enriches the workflow's contract at deploy time. When a workflow declares `tentacular-*` dependencies with only `protocol`, the MCP server fills in `host`, `port`, `user`, and other fields in the ConfigMap `workflow.yaml` before generating the Deployment manifest. This ensures:

- The Deployment's `--allow-net` Deno permission flags include exoskeleton service hostnames
- The engine's `ctx.dependency()` resolution has complete connection metadata at runtime

Enrichment happens server-side in the `wf_apply` handler, after registration but before manifest generation. The original `workflow.yaml` on disk is not modified.

### 9.4 Runtime access

Node code uses the standard dependency API:

```typescript
const pg = ctx.dependency("tentacular-postgres");
const nats = ctx.dependency("tentacular-nats");
const s3 = ctx.dependency("tentacular-rustfs");
```

### 9.5 MCP tools

| Tool | Description |
|------|-------------|
| `exo_status` | Returns exoskeleton configuration state (enabled services, auth status) |
| `exo_registration` | Returns registration details for a specific tentacle |

---

## 10. Roadmap

For the development roadmap, see [docs/roadmap.md](roadmap.md).

---

## 11. Known Limitations

| Limitation | Impact | Mitigation |
|------------|--------|------------|
| NATS SPIRE CA bundle sync is manual | NATS server TLS is automated via cert-manager (internal CA, auto-renewed). However, when SPIRE rotates its CA, the combined trust bundle in the `nats-spire-ca` Secret must be refreshed manually. | Future: sidecar or CronJob to watch `spire-bundle` and sync. See [NATS SPIFFE mTLS Activation Guide](nats-spiffe-deployment.md#certificate-rotation). |
| NATS token mode (fallback) | Convention-only subject isolation when SPIFFE mode is not enabled. | Enable SPIFFE mode for enforced isolation. |
| RustFS alpha admin API | Returns HTTP 500 instead of 404 for missing resources. | Registrar treats 500 as not-found for idempotent operations. |
| Keycloak azp vs aud | Access tokens use `azp` not `aud` for client ID. | Validator skips `aud` check, validates `azp` instead. |
| k0s kubelet path | SPIRE CSI driver hardcodes `/var/lib/kubelet/`; k0s uses `/var/lib/k0s/kubelet/`. | CSI driver disabled. SPIRE server + agent work without it. |
| No provenance persistence | Deployer identity is annotations-only, not queryable. | Sufficient for Phase 1 audit trail via `kubectl` / `wf_describe`. |
| ARM64 Bitnami images | Debian-tagged Bitnami images lack ARM64 builds. | Keycloak uses official `quay.io` image. Others use `:latest` or non-Bitnami charts. |
