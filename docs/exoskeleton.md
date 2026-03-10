# Exoskeleton Architecture and Design

## 1. Overview

### The problem

Tentacular already provides one-command deploy, automatic secret provisioning, and a Go CLI plus in-cluster MCP server pattern for cluster operations. But the moment a workflow needs backing services -- a database, a message bus, object storage -- the simplicity breaks down.

Before the exoskeleton, deploying a Tentacular workflow that needed these services meant the operator had to:

- Manually create a Postgres role and schema, choosing a name that does not collide with any other workflow's names
- Manually create NATS credentials and configure subject permissions, hoping that each workflow uses a different prefix
- Manually create S3 IAM users and policies with correct prefix scoping
- Write all of these credentials into `.secrets.yaml` files that must be kept in sync with the actual service state
- Track which credentials belong to which workflow, across multiple services, across multiple namespaces

There was no enforced isolation between tenants -- a misconfigured credential could access another workflow's data. There was no identity for workloads beyond whatever the operator chose to configure. And there was no attribution for deployments -- when something broke at 2 AM, the only clue was a bearer token that could belong to anyone or anything.

At the same time, enterprise deployments need more than just convenience. They need human attribution for deployments, SSO integration, consistent access boundaries, and a path to stronger workload identity via SPIFFE/SPIRE.

The exoskeleton addresses both ends of the spectrum -- convenience for developers and governance for enterprises -- by keeping the simple path intact while adding an optional control plane. It solves four specific problems:

1. **Manual credential management.** The MCP server provisions scoped credentials automatically at deploy time. Workflow authors declare what they need; the system handles the rest.
2. **No isolation between tenants.** Every tentacle gets its own Postgres schema, its own NATS subject prefix, and its own S3 object prefix. In SPIFFE mode, isolation is cryptographically enforced.
3. **No identity for workloads.** Each tentacle receives a deterministic SPIFFE identity derived from its namespace and workflow name. This identity is the root of all service-specific identifiers.
4. **No attribution for deployments.** When SSO is enabled, every deployment is annotated with the email address, timestamp, and agent type of the person who initiated it.

### What is a workspace?

A **workspace** is the scoped bundle of backing-service resources that the exoskeleton provisions for each tentacle. Concretely, it consists of:

- A **Postgres schema** (`tn_<ns>_<wf>`) with a dedicated role and least-privilege grants
- A **NATS subject prefix** (`tentacular.<ns>.<wf>.>`) with optional mTLS-enforced isolation
- An **S3 object prefix** (`ns/<ns>/tentacles/<wf>/`) with a prefix-scoped IAM policy
- A **SPIFFE identity** (`spiffe://tentacular/ns/<ns>/tentacles/<wf>`) provisioned by SPIRE

All four are derived deterministically from the tentacle's `(namespace, workflow)` tuple by a single pure function (`CompileIdentity`). There is no database of workspace records -- the identity model is stateless and reproducible.

### Design principles

The exoskeleton was designed under four constraints:

1. **Zero engine changes.** The Deno workflow engine reads secrets from `/app/secrets` and resolves dependencies via `ctx.dependency()`. The exoskeleton injects credentials through these existing mechanisms. The engine does not know or care whether its secrets came from a human-authored `.secrets.yaml` or from the exoskeleton's automatic provisioning.

2. **Opt-in via contract.** Workflows declare exoskeleton dependencies through the standard `contract.dependencies` block using the `tentacular-` prefix. A workflow with no `tentacular-*` dependencies deploys identically whether the exoskeleton is enabled or not.

3. **Feature-flagged.** Every capability is behind an environment variable. The top-level `TENTACULAR_EXOSKELETON_ENABLED` controls the entire subsystem. Individual services are enabled implicitly when their credentials are configured. SSO, SPIRE, and cleanup are each independently toggled.

4. **Backward compatible.** Existing bearer-token authentication continues to work. Existing workflows without exoskeleton dependencies are unaffected. The MCP server can run in simple mode with no exoskeleton services deployed at all.

### Relationship to the broader architecture

Tentacular routes all cluster-facing operations through the MCP server (`tentacular-mcp`). The CLI (`tntc`) sends workflow manifests to the MCP server via `wf_apply`; the MCP server generates Kubernetes resources and applies them. The exoskeleton extends this pipeline by intercepting `wf_apply`, inspecting the workflow's declared dependencies, and -- for any `tentacular-*` dependency -- registering the tentacle with the corresponding backing service, generating credentials, and injecting them into the manifest list before the MCP server applies it to the cluster.

This means the exoskeleton is not a separate system. It is an enrichment pipeline inside the MCP server that transforms manifests in flight.

### Goals and non-goals

**Primary goals:**

- Keep the default Tentacular user and agent experience simple. If you do not need backing services, nothing changes.
- Provide a deterministic mapping from tentacle identity to backing-service scope. No manual naming, no collisions, no coordination.
- Capture who or what deployed a tentacle. Every deployment should be attributable.
- Make exoskeleton registration automatic at deployment time. The workflow author declares dependencies; the system provisions everything.
- Make exoskeleton re-registration idempotent on redeploy. Data survives code updates.
- Make unregistration destructive only when explicitly requested. The default preserves data.
- Keep enterprise features behind feature flags. A small team should not pay the complexity cost of SSO, SPIRE, and approval flows unless they want to.

**Non-goals:**

- Multi-cluster federation in Phase 1. The exoskeleton manages a single cluster.
- Shared use of exoskeleton backing services by non-Tentacular workloads. Postgres, NATS, and RustFS are dedicated to Tentacular.
- A generic IAM platform inside Tentacular. The exoskeleton provides scoped access, not arbitrary policy authoring.
- Per-tentacle freeform policy authoring in Phase 1. Every tentacle gets the same privilege model (full access to its own scope, no access to anything else).
- Replacing the tentacle runtime identity with a human identity. The deployer identity is for audit; the workload identity is for runtime access.

### Assumptions

- All Tentacular workloads run in a single Kubernetes cluster.
- Exoskeleton services are dedicated to Tentacular (not shared with other applications).
- The tentacle identity is fully defined by the `(namespace, workflow)` tuple.
- All cluster-facing operations route through `tentacular-mcp` (the CLI does not talk to Kubernetes directly).
- Keycloak is the MCP server's identity integration point. Google SSO is upstream of Keycloak.
- SPIRE is optional in Phase 1.

The following diagram shows the complete exoskeleton architecture, including the namespace layout, component interactions, and data flow between the control plane and data plane:

![Exoskeleton Architecture](diagrams/exoskeleton-architecture.svg)

*The diagram shows the MCP server in `tentacular-system` orchestrating registrations against backing services in `tentacular-exoskeleton`, with workflow pods in `tent-*` namespaces accessing those services at runtime via scoped credentials.*

---

## 2. Architecture

### 2.1 Namespace layout

| Namespace | Role | Contents |
|-----------|------|----------|
| `tentacular-system` | Control plane | MCP server, SPIRE server + agent |
| `tentacular-exoskeleton` | Data plane | Postgres, NATS, RustFS, Keycloak, cert-manager internal CA |
| `tentacular-support` | Dev tooling | esm-sh module proxy, shared utilities |
| `tent-*` | Workloads | Tentacle workflow deployments |

#### Why SPIRE is in tentacular-system

SPIRE is a control-plane component, not a data service. It runs a DaemonSet (`spire-agent`) with host-level access, performs `TokenReview` calls against the Kubernetes API server to verify pod identity, and manages the trust domain's signing keys. Its access pattern -- privileged, cluster-scoped, host-mounted -- matches the MCP server, not the passive database or message broker running next to it. Placing SPIRE in `tentacular-exoskeleton` would mean the data-plane namespace contains a component with host-level privileges and cluster-wide RBAC, which contradicts the security boundary we want for that namespace.

#### Why data services share tentacular-exoskeleton

Postgres, NATS, RustFS, and Keycloak are all passive services that accept connections from the MCP server and from workflow pods. Giving each its own namespace would multiply the number of NetworkPolicies and ServiceAccounts without a proportional security benefit -- they are already isolated at the service level through per-service ServiceAccounts, disabled token auto-mounting, and pod-level network restrictions.

The isolation model within `tentacular-exoskeleton` is:

1. **One ServiceAccount per service**: `postgres-sa`, `nats-sa`, `rustfs-sa`, `keycloak-sa`. No service uses the default ServiceAccount.
2. **`automountServiceAccountToken: false`** on every pod. None of the data-plane services need Kubernetes API access.
3. **No RBAC grants** for pods/exec, pods/create, secrets/get, or any other escalation-sensitive permission.
4. **Restricted pod security**: no privileged mode, no hostPath, no hostPID/hostNetwork, read-only root filesystem where possible.
5. **NetworkPolicies**: inbound traffic to `tentacular-exoskeleton` is restricted to `tentacular-system` (the MCP server connecting to services) and `tent-*` namespaces (workflow pods connecting to their scoped services at runtime). No other namespace can reach data-plane services.

A compromised data-plane service cannot read another service's Secrets via the Kubernetes API (no token mounted, no RBAC), cannot exec into other pods, and cannot create workloads under another ServiceAccount.

#### Network flow

```
tentacular-system (MCP server)
  → tentacular-exoskeleton (Postgres, NATS, RustFS, Keycloak)  [admin + registration]

tent-* (workflow pods)
  → tentacular-exoskeleton (Postgres, NATS, RustFS)            [runtime, scoped credentials]

tentacular-system (SPIRE agent)
  → tentacular-system (SPIRE server)                           [attestation, SVID issuance]

External
  → tentacular-system (MCP server ingress)                     [API access]
  → tentacular-exoskeleton (Keycloak ingress)                  [SSO login]
```

No inbound traffic reaches `tentacular-system` except the MCP server endpoint. SPIRE agent-to-server communication stays within the namespace.

### 2.2 Control-plane and data-plane split

The exoskeleton introduces two distinct planes:

**Deployment control plane** (in `tentacular-system`):
- Driven by `tntc`, the skill, or other agent clients
- Implemented by `tentacular-mcp`
- Manages the full lifecycle: deploy, redeploy, undeploy
- Holds admin credentials for all backing services (stored as Kubernetes Secrets, never exposed to workflow pods)

**Data plane** (in `tentacular-exoskeleton`):
- The backing services themselves: Postgres, NATS, RustFS, Keycloak
- Accept connections from the MCP server (admin operations) and from workflow pods (runtime operations)
- Each service is passive -- it does not initiate outbound connections

The running tentacle gets a workload identity and scoped service credentials. It does **not** hold the MCP server's admin credentials, the human deployer's Google or Keycloak token, or the credentials for any other tentacle. The security boundary is enforced at three levels:

1. **Service-level**: Each tentacle's Postgres role can only access its own schema. Each RustFS IAM user can only access objects under its own prefix. Each NATS authorization entry restricts publish/subscribe to the tentacle's own subject prefix.
2. **Kubernetes-level**: NetworkPolicies restrict which namespaces can reach `tentacular-exoskeleton`. The exoskeleton Secret is in the workflow's namespace and is only readable by the workflow's ServiceAccount.
3. **Identity-level** (with SPIRE): The tentacle's SVID cryptographically binds its identity to its credentials. A compromised pod in a different namespace cannot impersonate another tentacle because it would need to forge an X.509 certificate signed by the SPIRE CA.

### 2.3 Component overview

| Component | Package | Responsibility |
|-----------|---------|----------------|
| Identity compiler | `pkg/exoskeleton/identity.go` | Pure function: `(namespace, workflow)` to deterministic `Identity` struct |
| Postgres registrar | `pkg/exoskeleton/registrar_postgres.go` | Role, schema, grants, password rotation, cleanup |
| NATS registrar | `pkg/exoskeleton/registrar_nats.go` | Dual-mode auth (SPIFFE mTLS or shared token), ConfigMap-based authorization |
| RustFS registrar | `pkg/exoskeleton/registrar_rustfs.go` | IAM user, prefix-scoped policy via RustFS admin API |
| SPIRE registrar | `pkg/exoskeleton/registrar_spire.go` | ClusterSPIFFEID CRD lifecycle via Kubernetes dynamic client |
| Controller | `pkg/exoskeleton/controller.go` | Orchestrates registration lifecycle across all enabled registrars |
| Credential injector | `pkg/exoskeleton/injector.go` | Builds Kubernetes Secret manifest with `<dep>.<field>` keys |
| Contract enrichment | `pkg/exoskeleton/enrich.go` | Fills host/port/user in ConfigMap, patches `--allow-net` flags |
| Auth (OIDC) | `pkg/exoskeleton/auth.go` | Bearer token + Keycloak SSO token validation |
| Config | `pkg/exoskeleton/config.go` | Environment variable loading, feature-flag evaluation |

### 2.4 The exoskeleton controller

The `Controller` type in `controller.go` is the central orchestrator. It is initialized once when the MCP server starts, and it holds references to whichever registrars are enabled based on the current configuration.

**Initialization** (`NewController`) follows this sequence:

1. If `TENTACULAR_EXOSKELETON_ENABLED` is false, return a no-op controller. All methods are safe to call; they return immediately.
2. For each service, check if its prerequisites are met (e.g., Postgres needs host + user + password). If met, initialize the registrar and establish its admin connection.
3. For SPIRE, additionally check that the `ClusterSPIFFEID` CRD is installed on the cluster. If the CRD is missing, SPIRE registration is silently skipped -- this makes the system tolerant of clusters where SPIRE is not yet installed.
4. Log the final state: which registrars are active, which are skipped.

**Manifest processing** (`ProcessManifests`) is called from the MCP server's `wf_apply` handler:

1. Scan the manifest list for a ConfigMap containing `workflow.yaml`.
2. Parse the workflow's `contract.dependencies` and extract any `tentacular-*` entries.
3. If none found, return the manifests unchanged -- the exoskeleton is transparent for workflows that do not opt in.
4. Compile the tentacle's identity from `(namespace, workflow)`.
5. For each detected dependency, call the corresponding registrar's `Register()` method. Registrars are called sequentially. If an earlier registrar succeeds but a later one fails, the successful registrations become orphaned. This is acceptable for Phase 1: the next deploy will re-register idempotently, and `Cleanup()` handles tear-down of all services.
6. Enrich the contract dependencies in the ConfigMap with resolved host/port/user values.
7. Build a Kubernetes Secret manifest from the accumulated credentials and append it to the manifest list.
8. Patch the Deployment's `--allow-net` flags to include exoskeleton service hostnames.
9. Register SPIRE identity (non-fatal if it fails -- SPIRE provides identity, not credentials).

**Cleanup** (`CleanupWithReport`) is called from `wf_remove` when `CleanupOnUndeploy` is true:

1. Check `cfg.Enabled && cfg.CleanupOnUndeploy`. If either is false, return an empty report immediately. This means cleanup is a no-op on clusters where the exoskeleton is disabled or where cleanup is not configured.
2. Compile the tentacle's identity from `(namespace, workflow)`.
3. Call each registrar's `Unregister()` method. Errors are collected into a slice but do not stop other registrars from running. This is intentional: if Postgres cleanup fails (e.g., the database is temporarily unreachable), we still want to attempt NATS and RustFS cleanup rather than leaving all three in a partially cleaned state.
4. Build a `CleanupReport` describing what happened: each service gets a status string (e.g., "schema dropped", "user removed", "authz entry removed", "identity removed").
5. If any errors occurred, return the report along with a combined error. The caller can decide whether to surface the error to the user or retry.

**Resource cleanup** (`Close`) is called when the MCP server shuts down:

1. Close the Postgres connection pool (releases all database connections).
2. Close the NATS registrar (no-op -- no persistent connection held).
3. Close the RustFS registrar (no-op -- HTTP client does not need explicit cleanup).
4. Close the SPIRE registrar (no-op -- dynamic Kubernetes client does not need explicit cleanup).

**Error handling philosophy**: The controller follows a "best effort, always proceed" pattern for registrations and cleanup. A failure in one service should not prevent the rest of the pipeline from executing. The only hard failures are:

- The identity compilation fails (empty namespace or workflow -- indicates a bug in the caller).
- A required service is declared as a dependency but its registrar is not initialized (e.g., `tentacular-postgres` declared but Postgres is not configured).

All other failures (network timeouts, transient database errors, SPIRE CRD issues) are logged and handled gracefully.

### 2.5 Data flow

The following shows the complete flow from CLI to running pod, as depicted in the [architecture diagram](diagrams/exoskeleton-architecture.svg):

```
CLI (tntc deploy)
  [1] User runs tntc deploy <workflow> in namespace <ns>
  [2] Optional: tntc login → Keycloak device auth → Google SSO → tokens stored locally
  [3] CLI sends manifests to MCP server via wf_apply (with bearer token or OIDC token)

MCP Server (wf_apply handler)
  [4] Authenticate request (bearer token or OIDC validation)
  [5] ExoskeletonController.ProcessManifests(ctx, namespace, name, manifests)
      [5a] IdentityCompiler: CompileIdentity(namespace, workflow) → Identity struct
      [5b] PostgresRegistrar.Register() → PostgresCreds (role, schema, password)
      [5c] NATSRegistrar.Register() → NATSCreds (URL, token or SPIFFE marker, subject prefix)
      [5d] RustFSRegistrar.Register() → RustFSCreds (endpoint, access key, secret key, prefix)
      [5e] SPIRERegistrar.Register() → creates ClusterSPIFFEID CRD (no creds returned)
      [5f] enrichContractDeps() → fills host/port/user in ConfigMap workflow.yaml
      [5g] BuildSecretManifest() → Kubernetes Secret with <dep>.<field> keys
      [5h] patchDeploymentAllowNet() → adds exo service hosts to --allow-net
  [6] If SSO active: AnnotateDeployer() → adds tentacular.io/deployed-by annotations
  [7] Apply enriched manifests to Kubernetes (ConfigMap, Secret, Deployment)

Kubernetes Cluster
  [8] Deployment creates engine pod in namespace <ns>
  [9] Pod mounts exoskeleton Secret at /app/secrets
  [10] SPIRE agent provisions X.509 SVID to pod (via ClusterSPIFFEID selector match)

Engine Pod (Deno runtime)
  [11] resolveSecrets() reads /app/secrets, populates dependency map
  [12] ctx.dependency("tentacular-postgres") → returns {host, port, user, password, schema}
  [13] Workflow nodes use scoped credentials to access backing services
```

---

## 3. Identity Model

### 3.1 Why deterministic identity matters

Every tentacle in the system needs identifiers across four different services (Postgres, NATS, RustFS, SPIRE). These identifiers must be:

- **Reproducible**: deploying the same workflow to the same namespace must always produce the same identifiers. This is what makes re-registration idempotent -- the registrar can detect that a role or user already exists and update it rather than creating a duplicate.
- **Collision-free**: two different `(namespace, workflow)` tuples must never produce the same Postgres role, NATS user, or S3 IAM user. This is guaranteed by the naming convention and, for truncated identifiers, by appending a SHA-256 hash suffix.
- **Auditable**: given a credential found in a Secret or a row in a database schema, an operator must be able to reverse-engineer which tentacle owns it. The `tn_` prefix and the `<ns>_<wf>` structure make this straightforward.

The alternative -- generating random UUIDs and storing mappings in a database -- would require a persistence layer, add a failure mode (what if the mapping database is unavailable?), and make debugging harder. The deterministic approach keeps the system stateless: if the mapping database were lost, you could reconstruct every identifier from the namespace and workflow name alone.

### 3.2 Canonical principal

The SPIFFE URI is the canonical identity for every tentacle:

```
spiffe://tentacular/ns/<namespace>/tentacles/<workflow>
```

This URI serves as the root from which all service-specific identifiers are derived, regardless of whether SPIRE is actually enabled on the cluster. When SPIRE is enabled, this URI is embedded in the X.509 SVID provisioned to the workflow pod. When SPIRE is not enabled, the URI still appears in the exoskeleton Secret as `tentacular-identity.principal` and is used as the logical identity for NATS authorization entries in SPIFFE mode.

### 3.3 Service-specific mappings

All identifiers are derived deterministically from `(namespace, workflow)` by `CompileIdentity()`:

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

### 3.4 The CompileIdentity function

`CompileIdentity(namespace, workflow)` is a pure function with no side effects, no I/O, and no dependencies beyond the Go standard library. It takes two strings and returns an `Identity` struct containing all service-specific identifiers. The function validates that both inputs are non-empty (returning `ErrEmptyNamespace` or `ErrEmptyWorkflow` otherwise) and applies service-appropriate sanitization.

The function is called exactly once per deploy: by the controller's `ProcessManifests`. It is also called during cleanup to reconstruct the same identifiers. Because the function is deterministic and pure, it is trivially unit-testable -- the test suite verifies identity compilation across a matrix of namespace/workflow combinations, edge cases (long names, special characters), and truncation scenarios.

```go
id, err := CompileIdentity("tent-dev", "hn-digest")
// id.Principal  = "spiffe://tentacular/ns/tent-dev/tentacles/hn-digest"
// id.PgRole     = "tn_tent_dev_hn_digest"
// id.PgSchema   = "tn_tent_dev_hn_digest"
// id.NATSUser   = "tentacle.tent-dev.hn-digest"
// id.NATSPrefix = "tentacular.tent-dev.hn-digest.>"
// id.S3Prefix   = "ns/tent-dev/tentacles/hn-digest/"
// id.S3User     = "tn_tent_dev_hn_digest"
// id.S3Policy   = "tn_tent_dev_hn_digest_policy"
```

### 3.5 Sanitization rules

Different services have different naming constraints. The identity compiler applies service-appropriate sanitization:

**Postgres and S3 IAM identifiers** (`tn_<ns>_<wf>` format):

- Hyphens are replaced with underscores. Kubernetes namespace names are DNS-1123 labels (lowercase alphanumeric + hyphens), and hyphens are not valid in Postgres unquoted identifiers. Without this replacement, every SQL statement would need to quote the identifier, and tools that do not quote properly would fail.
- All characters are lowercased. PostgreSQL folds unquoted identifiers to lowercase, so forcing lowercase at compilation time prevents case-sensitivity surprises.
- Any remaining character outside `[a-z0-9_]` is stripped. Kubernetes namespace names should only contain DNS-1123 characters, but this is a safety net against non-standard input from custom admission controllers or imported namespaces.
- **Truncation**: Postgres identifiers are limited to 63 characters (a hard PostgreSQL limit defined by `NAMEDATALEN`). If the raw identifier exceeds this, it is truncated and a `_<8-hex-sha256>` suffix is appended to preserve uniqueness. The suffix is 9 characters (`_` plus 8 hex digits from the first 4 bytes of SHA-256), leaving 54 characters for the human-readable portion. This means a namespace like `very-long-production-namespace` combined with a workflow like `extremely-long-workflow-name-for-digest-processing` will still produce a unique, valid Postgres identifier.

**NATS and S3 prefix identifiers** preserve hyphens, because both NATS subject tokens and S3 key prefixes allow them. No truncation is needed since these systems do not impose a 63-character limit.

**K8s resource names** (for ClusterSPIFFEID): must be DNS-1123 compliant (lowercase alphanumeric and hyphens, no underscores). The SPIRE registrar uses a separate `sanitizeK8sName()` that lowercases and replaces non-alphanumeric characters (except hyphens) with hyphens, then trims leading/trailing hyphens. Maximum length is 253 characters per Kubernetes convention.

### 3.6 Deployer identity

When SSO auth is active, the deployer's identity is recorded as Kubernetes annotations on the Deployment manifest. This provides an audit trail without requiring a separate provenance database:

| Annotation | Value |
|------------|-------|
| `tentacular.io/deployed-by` | Deployer email (e.g., `user@example.com`) |
| `tentacular.io/deployed-at` | ISO 8601 timestamp |
| `tentacular.io/deployed-via` | Agent type (`cli`, `slack-bot`, `web-ui`) |

The `AnnotateDeployer()` function in `enrich.go` applies these annotations to every Deployment in the manifest list. For bearer-token auth (where no deployer email is available), `deployed-by` is set to `"bearer-token"`.

### 3.7 Separation of identities

The exoskeleton maintains a strict separation between three types of identity:

- **Deployer identity** -- the human who initiated the deployment. Captured via OIDC, stored as annotations. Used for audit.
- **Agent identity** -- the tool or transport that issued the MCP call (`cli`, `slack-bot`, `web-ui`). Captured from request metadata.
- **Workload identity** -- the SPIFFE URI and scoped credentials that the running pod uses to access backing services. The workload never holds the deployer's Google or Keycloak token.

The deployer identity authorizes the deployment event. The workload identity authorizes runtime access. They must remain separate -- a workflow pod should never be able to impersonate the human who deployed it.

---

## 4. Services

### 4.1 Postgres

#### What it does

The Postgres registrar creates a per-tentacle role and schema within a shared `tentacular` database. This implements a schema-per-tentacle isolation model: every tentacle gets its own namespace within the database, and its credentials only grant access to that namespace.

#### Why schema-per-tentacle

The schema-per-tentacle model was chosen over database-per-tentacle and table-prefix-per-tentacle for several reasons:

- **vs. database-per-tentacle**: Creating a separate database for each tentacle is operationally heavier (requires `CREATEDB` privilege, complicates backup strategies, and creates connection pool fragmentation). Most tentacles need a few tables, not a full database.
- **vs. table-prefix-per-tentacle**: Using a shared schema with prefixed table names (e.g., `tn_tentdev_hndigest_articles`) provides no PostgreSQL-level isolation. Any role with access to the shared schema can read or write any table. Schema-level isolation, by contrast, is enforced by PostgreSQL's privilege system.
- **Schema-per-tentacle** gives strong isolation (each role can only access its own schema), simple cleanup (`DROP SCHEMA CASCADE`), and no naming conflicts (table names are scoped to the schema, not the database).

#### Registration

Registration creates a role and schema in a single transaction:

1. **Create role** (idempotent): Uses a `DO $$ ... END $$` block to check `pg_catalog.pg_roles` and either `CREATE ROLE` with `LOGIN PASSWORD` or `ALTER ROLE` with a new password. The `IF NOT EXISTS` pattern is implemented manually because PostgreSQL versions before 16 do not support `CREATE ROLE IF NOT EXISTS`.

2. **Grant role to admin**: `GRANT <role> TO CURRENT_USER`. This is the key to operating without `SUPERUSER`. PostgreSQL requires that the user issuing `CREATE SCHEMA ... AUTHORIZATION <role>` must be a member of `<role>`. The `GRANT TO CURRENT_USER` pattern satisfies this requirement with only `CREATEROLE` privilege on the admin user.

3. **Create schema**: `CREATE SCHEMA IF NOT EXISTS <schema> AUTHORIZATION <role>`. The schema name is identical to the role name. Authorization is set to the tentacle's role so it owns all objects within the schema.

4. **Grant privileges**: `GRANT USAGE ON SCHEMA` and `GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA` ensure the role can access existing tables.

5. **Alter default privileges**: `ALTER DEFAULT PRIVILEGES IN SCHEMA <schema> GRANT ALL PRIVILEGES ON TABLES TO <role>`. This ensures that tables created in the future (by migrations, by the workflow itself, or by the admin) are automatically accessible to the tentacle's role.

```sql
-- The actual SQL executed (simplified):
CREATE ROLE "tn_tent_dev_hn_digest" LOGIN PASSWORD '<generated>';
GRANT "tn_tent_dev_hn_digest" TO CURRENT_USER;
CREATE SCHEMA IF NOT EXISTS "tn_tent_dev_hn_digest"
  AUTHORIZATION "tn_tent_dev_hn_digest";
GRANT USAGE ON SCHEMA "tn_tent_dev_hn_digest"
  TO "tn_tent_dev_hn_digest";
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA "tn_tent_dev_hn_digest"
  TO "tn_tent_dev_hn_digest";
ALTER DEFAULT PRIVILEGES IN SCHEMA "tn_tent_dev_hn_digest"
  GRANT ALL PRIVILEGES ON TABLES TO "tn_tent_dev_hn_digest";
```

All identifiers are double-quoted to prevent SQL injection. The `pgIdent()` function escapes embedded double quotes by doubling them per SQL standard.

#### Re-registration

Re-registration rotates the password but preserves the schema and all data. The `DO $$ ... END $$` block detects the existing role and issues `ALTER ROLE` with a new password instead of `CREATE ROLE`. The `CREATE SCHEMA IF NOT EXISTS` is a no-op when the schema already exists. Privileges are re-granted to ensure consistency.

Password rotation on re-deploy is intentional: it limits the window of exposure if a credential is leaked. The old password becomes invalid as soon as the new Secret is applied to the workflow namespace.

#### Unregistration (cleanup enabled)

Unregistration is permanent and destructive:

1. `REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA` (best-effort; may fail if the schema is empty)
2. `DROP SCHEMA IF EXISTS <schema> CASCADE` -- this drops all tables, views, functions, indexes, and data within the schema
3. `DROP ROLE IF EXISTS <role>`

The `CASCADE` keyword is significant. It means the `DROP SCHEMA` will destroy everything inside the schema without requiring a separate enumeration of objects. The implications are serious: there is no undo, no soft delete, no recycle bin. Once cleanup runs, the data is gone.

#### Admin requirements

The MCP server's admin role needs `CREATEROLE` privilege. `SUPERUSER` is not required. The `GRANT TO CURRENT_USER` pattern specifically avoids the need for superuser access, which is a deliberate security decision -- the admin credentials should not have more power than necessary.

#### Security boundary

Each tentacle's role can only access its own schema. The isolation is enforced at the PostgreSQL level:

- The role is created with `LOGIN` only -- no `SUPERUSER`, no `CREATEDB`, no `CREATEROLE`.
- Schema authorization is set to the tentacle's role, so only that role (and the admin) can create objects in the schema.
- `GRANT USAGE ON SCHEMA` is specific to the tentacle's schema. No cross-schema grants are created.
- The role cannot see other schemas' contents because PostgreSQL's default `search_path` does not include them, and no grants are issued for other schemas.

**Hardening note**: PostgreSQL's default grants on the `public` schema allow all roles to create objects there. Operators should revoke `CREATE ON SCHEMA public FROM PUBLIC` as a hardening step to prevent tentacles from creating tables in the shared `public` schema. This is not done automatically by the registrar because it is a cluster-wide change that may affect other database users.

**Future**: Phase 2 plans to replace static passwords with Vault-managed short-lived credentials. In this model, the tentacle would authenticate to Vault using its SPIFFE SVID, and Vault would issue a time-limited Postgres credential. This eliminates the need for password rotation on redeploy and reduces the credential exposure window from "until next redeploy" to minutes.

### 4.2 NATS

#### What it does

The NATS registrar provisions a scoped subject prefix for each tentacle. The NATS user is `tentacle.<ns>.<wf>` with publish/subscribe on `tentacular.<ns>.<wf>.>`. Two authentication modes are supported, selectable by configuration.

#### Dual mode: SPIFFE vs token

| Mode | Config | Auth mechanism | Isolation |
|------|--------|----------------|-----------|
| **SPIFFE** (preferred) | `TENTACULAR_NATS_SPIFFE_ENABLED=true` | mTLS with X.509 SVIDs; NATS `verify_and_map` maps SPIFFE URIs to authorization rules | Cryptographically enforced per-tentacle subject isolation |
| **Token** (active default) | `TENTACULAR_NATS_TOKEN` set, SPIFFE not enabled | Shared bearer token | Convention-only subject isolation |

Both modes exist because not every cluster has SPIRE installed. Token mode is the fallback for clusters in early stages of infrastructure build-out. In token mode, all tentacles share the same NATS token. Subject isolation is convention-only -- a tentacle can technically publish to another tentacle's subject prefix. This is a known limitation accepted for Phase 1 simplicity.

SPIFFE mode provides cryptographic enforcement. A tentacle's SVID certificate contains its SPIFFE URI in the SAN field. The NATS server, configured with `verify_and_map`, extracts this URI and looks up the tentacle's permissions in a ConfigMap-based authorization configuration. A tentacle cannot publish to subjects it is not authorized for, because the authorization check happens at the NATS server level based on the certificate identity.

#### SPIFFE mode: the trust chain

```
SPIRE CA
  → issues X.509 SVID to workflow pod (via SPIRE agent + ClusterSPIFFEID)
    → pod presents SVID as client certificate to NATS
      → NATS verify_and_map extracts SPIFFE URI from certificate SAN
        → NATS looks up authorization entry in ConfigMap
          → authorization entry grants publish/subscribe on tentacular.<ns>.<wf>.>
```

#### SPIFFE mode: ConfigMap-based authorization

When SPIFFE mode is enabled, the NATS registrar manages an authorization ConfigMap (default name: `nats-tentacular-authz` in namespace `tentacular-exoskeleton`). Each entry maps a tentacle's SPIFFE URI to its permitted publish/subscribe subjects:

```
authorization {
  users = [
    {
      user = "spiffe://tentacular/ns/tent-dev/tentacles/hn-digest"
      permissions = {
        publish = {
          allow = ["tentacular.tent-dev.hn-digest.>"]
        }
        subscribe = {
          allow = ["tentacular.tent-dev.hn-digest.>"]
        }
      }
    }
  ]
}
```

The registrar uses optimistic concurrency (Kubernetes `resourceVersion`) when updating the ConfigMap. Entries are sorted by user for deterministic output, which minimizes spurious diffs when multiple registrations happen in sequence.

NATS must be configured to include this ConfigMap as an authorization configuration. The typical setup mounts the ConfigMap as a volume in the NATS pod and includes it in `nats.conf` via the `include` directive. When the ConfigMap changes, NATS needs a configuration reload. This can be triggered by a sidecar that watches the mounted file and sends a SIGHUP, or by the NATS Helm chart's built-in reload mechanism.

**Important operational note**: There is a brief window between when the exoskeleton writes the ConfigMap and when NATS reloads it. During this window, a newly registered tentacle's SVID will be rejected by NATS because the authorization entry does not exist yet. In practice, this is a non-issue because the workflow pod takes several seconds to start, and the ConfigMap reload happens within a few seconds of the change.

> **Note:** SPIFFE mode is fully implemented, tested, and deployed on the `eastus-dev` cluster. The NATS server TLS certificate is issued by a cert-manager internal CA (`tentacular-internal-ca`), with 1-year validity and automatic renewal 30 days before expiry. See [NATS SPIFFE mTLS Activation Guide](nats-spiffe-deployment.md) for the full deployment procedure.

#### Registration

- **Token mode**: Returns the shared NATS URL and token along with the tentacle's scoped subject prefix. No server-side state is created.
- **SPIFFE mode**: Upserts an authorization entry in the ConfigMap. Returns the NATS URL, subject prefix, and `auth_method: "spiffe"` (no token -- auth is via SVID certificate).

#### Re-registration

Re-registration preserves the same subject scope and any durable JetStream state. NATS JetStream streams and consumers are server-side state that persists independently of client connections. A tentacle that uses JetStream to create a durable consumer will find that consumer intact after a redeploy.

In SPIFFE mode, the authorization ConfigMap entry is updated. If the entry already exists with the same permissions, the update is effectively a no-op. If permissions have changed (e.g., due to a configuration update), the entry is replaced.

In token mode, re-registration is purely a client-side credential return. No server-side state changes.

#### Unregistration

- **Token mode**: No-op. There are no per-tentacle server-side artifacts to remove. The shared token continues to work for other tentacles.
- **SPIFFE mode**: Removes the tentacle's entry from the authorization ConfigMap. After NATS reloads its configuration, the tentacle's SVID will no longer be authorized for any subjects. Any active connections using that SVID will be disconnected on the next authorization check.

**Important**: NATS unregistration does not delete JetStream streams or consumers owned by the tentacle. This is intentional: stream cleanup requires knowledge of which streams belong to which tentacle, and the exoskeleton does not currently track stream ownership. Orphaned JetStream state must be cleaned up manually or via NATS admin tools.

### 4.3 RustFS

#### What it does

The RustFS registrar creates a per-tentacle IAM user with a prefix-scoped policy within a shared `tentacular` bucket. RustFS is a MinIO-compatible object storage server.

#### Why madmin-go failed

Early in development, we attempted to use MinIO's `madmin-go` library for IAM operations. This failed because `madmin-go` hardcodes the `/minio/admin/v3/` path prefix in all admin API calls. RustFS uses `/rustfs/admin/v3/` instead. The API shape is identical -- same endpoints, same request/response formats -- but the path prefix differs. Rather than forking `madmin-go` to change a hardcoded string, we wrote a thin HTTP client (`rustfsAdmin` in `registrar_rustfs.go`) that calls the RustFS admin API directly.

This client uses AWS SigV4 HTTP signing (via `minio-go/v7/pkg/signer.SignV4`) with service `"s3"`. Each request includes the `X-Amz-Content-Sha256` header required by SigV4. The data-plane operations (bucket creation, object listing, object deletion) use the standard S3 API via `minio-go/v7`, which works with RustFS without modification.

#### Registration

1. **Ensure bucket exists**: Check via S3 API (`BucketExists`). Create if missing (`MakeBucket`). Race conditions are handled by ignoring `BucketAlreadyOwnedByYou` and `BucketAlreadyExists` errors.

2. **Create IAM user**: `PUT /rustfs/admin/v3/add-user?accessKey=<userName>` with body `{"secretKey": "<generated>", "status": "enabled"}`. The access key is the deterministic `tn_<ns>_<wf>` name, not a random string. This is critical: `Unregister()` must be able to find and remove the user by the same deterministic name. Early versions used randomly generated access keys, which broke unregistration because there was no way to look up which random key belonged to which tentacle.

3. **Create IAM policy**: `PUT /rustfs/admin/v3/add-canned-policy?name=<policyName>` with a JSON policy document:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
      "Resource": "arn:aws:s3:::tentacular/ns/tent-dev/tentacles/hn-digest/*"
    },
    {
      "Effect": "Allow",
      "Action": ["s3:ListBucket"],
      "Resource": "arn:aws:s3:::tentacular",
      "Condition": {
        "StringLike": {
          "s3:prefix": "ns/tent-dev/tentacles/hn-digest/*"
        }
      }
    }
  ]
}
```

The policy has two statements:

- **Statement 1** (`GetObject`, `PutObject`, `DeleteObject`): Grants read/write/delete access to objects under the tentacle's prefix. The `Resource` is the ARN with a wildcard suffix (`*`), meaning the tentacle can create any key structure within its prefix (subdirectories, nested paths, etc.).
- **Statement 2** (`ListBucket` with `Condition`): Grants the ability to list objects, but only when the `s3:prefix` matches the tentacle's prefix. Without this condition, `ListBucket` would allow the tentacle to enumerate all objects in the shared bucket, including other tentacles' objects. The `StringLike` condition with a wildcard prefix restriction prevents this.

The policy explicitly does **not** grant `s3:DeleteBucket`, `s3:CreateBucket`, `s3:PutBucketPolicy`, or any other bucket-level administrative action. The tentacle can only operate on objects within its scoped prefix.

4. **Attach policy to user**: `PUT /rustfs/admin/v3/set-user-or-group-policy?policyName=<policy>&userOrGroup=<user>&isGroup=false`.

#### Re-registration

Preserves all objects under the prefix. The IAM user is updated with a new secret key (AddUser is idempotent -- it creates or updates). The policy is re-created (also idempotent).

#### Unregistration (cleanup enabled)

1. Recursively list and delete all objects under the tentacle's prefix via S3 API.
2. Remove the IAM user via admin API (`DELETE /rustfs/admin/v3/remove-user`). This must happen before policy removal because RustFS rejects removing a policy that is still attached to a user.
3. Remove the canned policy via admin API (`DELETE /rustfs/admin/v3/remove-canned-policy`).

#### Known quirk

RustFS alpha (1.0.0-alpha.85) returns HTTP 500 instead of HTTP 404 when querying a non-existent user or policy. The registrar handles this by treating 500 as not-found for idempotent operations.

### 4.4 SPIRE

#### What it does

The SPIRE registrar creates a `ClusterSPIFFEID` custom resource that tells the SPIRE controller to provision X.509 SVIDs to workflow pods matching specific label and namespace selectors.

#### ClusterSPIFFEID CRD approach

Rather than calling the SPIRE server API directly (which would require maintaining a gRPC connection and handling SPIRE's attestation protocol), the registrar uses the Kubernetes-native `ClusterSPIFFEID` CRD. This CRD is part of the SPIRE Controller Manager project. When a `ClusterSPIFFEID` resource exists, the SPIRE controller watches for pods matching the selectors and automatically creates SPIRE registration entries for them.

```yaml
apiVersion: spire.spiffe.io/v1alpha1
kind: ClusterSPIFFEID
metadata:
  name: tentacle-<ns>-<wf>
  labels:
    tentacular.io/release: <workflow>
    tentacular.io/exoskeleton: "true"
spec:
  className: tentacular-system-spire
  hint: <workflow>
  spiffeIDTemplate: >-
    spiffe://{{ .TrustDomain }}/ns/{{ .PodMeta.Namespace }}/tentacles/{{
    index .PodMeta.Labels "tentacular.io/release" }}
  namespaceSelector:
    matchLabels:
      kubernetes.io/metadata.name: <namespace>
  podSelector:
    matchLabels:
      tentacular.io/release: <workflow>
```

Key fields in the spec:

- **`className`**: Scopes this `ClusterSPIFFEID` to the Tentacular SPIRE installation (value: `tentacular-system-spire`). If multiple SPIRE installations exist on the cluster (e.g., different trust domains for different teams), the className ensures this entry is processed only by the correct SPIRE controller.
- **`hint`**: A human-readable hint for SPIRE server logs and debug output. Set to the workflow name.
- **`spiffeIDTemplate`**: Uses Go template syntax with variables provided by the SPIRE controller. `{{ .TrustDomain }}` resolves to the SPIRE trust domain (typically `tentacular`). `{{ .PodMeta.Namespace }}` and the label lookup ensure the SPIFFE URI is dynamically constructed per-pod, though in practice all pods for a given workflow get the same URI.
- **`namespaceSelector`**: Restricts matching to pods in the workflow's namespace. Uses the standard `kubernetes.io/metadata.name` label that Kubernetes automatically applies to every namespace.
- **`podSelector`**: Restricts matching to pods with the `tentacular.io/release` label set to the workflow name. This label is applied by the MCP server when generating the Deployment manifest.

#### Why registration is non-fatal

SPIRE provides identity, not service credentials. If SPIRE registration fails (CRD not installed, API server unreachable, RBAC misconfigured), the workflow can still deploy and access Postgres, NATS, and RustFS via password/token credentials. The SVID is an enhancement for mTLS -- its absence degrades the security posture but does not break functionality. For this reason, `ProcessManifests` logs a warning but does not return an error when SPIRE registration fails.

#### The SVID lifecycle

1. **Provisioning**: The SPIRE agent, running as a DaemonSet in `tentacular-system`, watches for pods that match a registered entry. When a workflow pod starts, the agent provisions an X.509 SVID to it.
2. **Rotation**: SVIDs are short-lived (typically 1 hour). The SPIRE agent rotates them automatically before expiry. The workflow does not need to handle renewal.
3. **Revocation**: When the `ClusterSPIFFEID` is deleted (during cleanup), the SPIRE controller removes the registration entry. The agent stops renewing the SVID, and it expires naturally.

#### Foundation for future capabilities

The SPIRE-issued SVID establishes a cryptographic identity that can be used for:

- **NATS mTLS** (built, see Section 4.2): SVIDs provide client certificates for NATS `verify_and_map` authentication. This is the first concrete consumer of SPIRE identity beyond the exoskeleton itself.
- **Vault authentication**: SVIDs can authenticate to HashiCorp Vault's SPIFFE auth method, enabling short-lived database credentials instead of static passwords. This would replace the current password-based Postgres auth with time-limited credentials issued by Vault.
- **Istio ambient mode**: SPIFFE IDs are the native identity format for Istio's ambient mesh, enabling seamless service mesh integration without sidecar injection. This would add network-level encryption and authorization between workflow pods and backing services.
- **Cross-service authorization**: Future phases may use SPIFFE IDs to authorize workflow-to-workflow communication, enabling a tentacle to call another tentacle's API with cryptographic identity verification.

---

## 5. Authentication

### 5.1 The dual auth design

The MCP server supports two authentication modes simultaneously:

| Mode | When | How |
|------|------|-----|
| Bearer token | Always available | `Authorization: Bearer <token>` header, configured via `TENTACULAR_MCP_TOKEN` |
| OIDC (Keycloak SSO) | When `TENTACULAR_EXOSKELETON_AUTH_ENABLED=true` | Keycloak-issued access token, brokered to Google SSO |

Both modes coexist on the same server. This dual design exists for backward compatibility and operational flexibility:

- **Bearer tokens** are essential for automation and CI/CD pipelines. A GitHub Actions workflow or a cron job cannot open a browser to authenticate via Google SSO. Bearer tokens provide a simple, machine-friendly authentication path.
- **OIDC tokens** add human identity attribution. When a person deploys a workflow, the system records who they are. This matters for audit trails, incident response, and compliance.

The MCP server's auth middleware tries OIDC validation first (if enabled). If OIDC validation fails or is not configured, it falls back to bearer token validation. A request that passes either check is authenticated.

### 5.2 Keycloak as the OIDC broker

Keycloak serves as the OIDC broker between Tentacular and the upstream identity provider (Google SSO). The architecture is:

```
tntc CLI
  → MCP server (OIDC client)
    → Keycloak (OIDC provider, realm: tentacular)
      → Google (upstream identity provider via OpenID Connect)
```

Keycloak is the identity integration point. It handles:

- OIDC discovery and JWKS endpoint exposure
- Token issuance (access tokens, refresh tokens, ID tokens)
- Identity provider brokering (Google SSO)
- Client management (the `tentacular-mcp` client)
- Realm configuration (the `tentacular` realm)

Google is not directly integrated into Tentacular. All identity federation goes through Keycloak, which allows swapping or adding identity providers (Azure AD, GitHub, SAML, etc.) without changing the MCP server code.

### 5.3 The azp vs aud quirk

Keycloak access tokens use the `azp` (authorized party) claim instead of the standard `aud` (audience) claim for the client ID. The OIDC specification allows this, but most OIDC libraries default to validating `aud`. The MCP server's `OIDCValidator` handles this by:

1. Creating the verifier with `SkipClientIDCheck: true` (disabling the default `aud` validation)
2. Manually validating `azp` against the configured client ID after token verification

This is not a bug in Keycloak -- it is a deliberate design choice in how Keycloak issues access tokens (as opposed to ID tokens, where `aud` is set correctly). The validator in `auth.go` documents this quirk explicitly.

### 5.4 Device Authorization Grant flow

The CLI uses the OAuth 2.0 Device Authorization Grant (RFC 8628) for authentication. This flow is designed for devices with limited input capabilities, but it works well for CLIs because it does not require the CLI to run a local HTTP server or handle redirect URIs.

1. `tntc login` initiates the flow: `POST /auth/device` to the MCP server
2. MCP proxies the request to Keycloak's device authorization endpoint
3. MCP returns `device_code`, `user_code`, `verification_uri` to the CLI
4. CLI opens a browser to `verification_uri` (or prints the URI if browser launch fails)
5. User enters the `user_code` in the browser
6. User authenticates via Google SSO through Keycloak
7. CLI polls the MCP server's token endpoint at the specified interval
8. On success, CLI stores access + refresh tokens at `~/.tentacular/auth-token`
9. `tntc whoami` displays the authenticated identity (email, issuer, expiry)
10. `tntc logout` clears local tokens

### 5.5 Token lifespans and refresh behavior

The CLI checks token expiry before each request. If the access token is expired or within 30 seconds of expiry, the CLI uses the refresh token to obtain a new access token transparently. If the refresh token is also expired, the CLI prompts the user to re-run `tntc login`.

Access token lifespans and refresh token policies are configured in Keycloak. Typical settings:

- Access token: 5 minutes
- Refresh token: 30 days
- SSO session idle: 30 days

### 5.6 Google SSO domain restriction

The Keycloak Google identity provider is configured with a `hostedDomain` parameter that restricts authentication to a specific Google Workspace domain. Only Google accounts belonging to the configured domain can authenticate. This prevents arbitrary Google accounts from obtaining access to the Tentacular deployment.

Administrators set the allowed domain in the Keycloak Google IdP configuration: Identity Providers > Google > `Hosted Domain` field.

### 5.7 Token validation flow

When an OIDC token arrives at the MCP server, the `OIDCValidator` performs the following checks:

1. **JWKS verification**: The token signature is verified against the JWKS (JSON Web Key Set) fetched from the Keycloak issuer's discovery endpoint. This confirms the token was issued by Keycloak and has not been tampered with.
2. **Expiry check**: The token's `exp` claim is checked against the current time. Expired tokens are rejected.
3. **Issuer check**: The token's `iss` claim must match the configured issuer URL.
4. **Authorized party check**: The token's `azp` claim is validated against the configured client ID (see Section 5.3 for why `aud` is skipped).
5. **Claims extraction**: Email, display name, subject, and identity provider are extracted from the token claims.
6. **Provider detection**: The `determineProvider()` function checks the `identity_provider` claim (set by Keycloak when brokering). If present, it identifies the upstream IdP (e.g., `"google"`). Otherwise, it falls back to checking the `azp` claim or defaults to `"keycloak"`.

The resulting `DeployerInfo` struct contains everything needed for provenance annotations.

### 5.8 Deployer provenance

When SSO auth is active and a deploy succeeds, the MCP server annotates the Deployment manifest with deployer identity (see Section 3.6). This provides a lightweight audit trail that is queryable via `kubectl get deployment -o yaml` or `wf_describe`.

### 5.9 Keycloak client requirements

- **Realm**: `tentacular`
- **Client**: `tentacular-mcp` (confidential)
- **OAuth 2.0 Device Authorization Grant**: enabled
- **Scopes**: `openid`, `profile`, `email`
- **Redirect URI**: `https://mcp.<cluster>.<domain>/*`
- **Google IdP**: configured as upstream identity provider with `Trust Email` enabled, `email`/`name`/`sub` claims mapped

---

## 6. Certificate Management

### 6.1 Why cert-manager

The exoskeleton data plane requires TLS certificates for multiple services: NATS server TLS, Keycloak HTTPS, and (in SPIFFE mode) a trust bundle that combines multiple CA certificates. Managing these certificates manually -- generating CSRs, signing them, distributing them, rotating them before expiry -- is operationally burdensome and error-prone.

cert-manager automates this entire lifecycle. It provisions certificates from configured issuers, stores them as Kubernetes Secrets, and renews them automatically before expiry (default: 30 days before).

### 6.2 The dual-CA architecture

The exoskeleton uses two independent certificate authorities:

1. **cert-manager internal CA** (`tentacular-internal-ca`): Issues server certificates for NATS, Keycloak, and other data-plane services. cert-manager manages the CA key, certificate issuance, and rotation.

2. **SPIRE CA**: Issues client X.509 SVIDs to workflow pods. SPIRE manages its own CA key, which is not exportable -- you cannot extract the SPIRE CA private key and import it into cert-manager.

This dual-CA design is not accidental. SPIRE's trust model requires that the CA key be managed exclusively by the SPIRE server. If the SPIRE CA key were managed by cert-manager, a compromise of the cert-manager installation could allow an attacker to mint arbitrary SVIDs. By keeping the CAs separate, the blast radius of a compromise is contained.

### 6.3 Combined trust bundle

NATS, when operating in SPIFFE mode, needs to trust both CAs:

- The **cert-manager CA** for its own server certificate chain (so NATS can present a valid server cert to clients)
- The **SPIRE CA** for client SVIDs (so NATS can verify the SVID certificates presented by workflow pods)

The combined trust bundle is stored in a Kubernetes Secret (`nats-spire-ca`) and mounted into the NATS pod. When cert-manager rotates the server CA, the trust bundle is updated automatically. However, when SPIRE rotates its CA, the combined trust bundle must be refreshed manually (see Known Limitations, Section 11).

### 6.4 cert-manager resources

The certificate hierarchy consists of four layers:

```
ClusterIssuer (tentacular-selfsigned)
  → CA Certificate (tentacular-internal-ca, self-signed, 10-year validity)
    → Issuer (tentacular-internal-ca-issuer, CA issuer using the CA cert)
      → Certificate (nats-server-tls, 1-year validity, auto-renewed 30 days before expiry)
      → Certificate (keycloak-tls, ...)
```

**ClusterIssuer** (`tentacular-selfsigned`): A cluster-wide self-signed issuer used solely to bootstrap the CA certificate. It is not used to issue leaf certificates directly.

**CA Certificate** (`tentacular-internal-ca`): A self-signed CA certificate with a 10-year validity period. This is the root of trust for all cert-manager-issued certificates in the exoskeleton. The private key is stored in a Kubernetes Secret managed by cert-manager.

**Issuer** (`tentacular-internal-ca-issuer`): A namespace-scoped issuer in `tentacular-exoskeleton` that references the CA certificate. This issuer is used to sign leaf certificates for individual services.

**Leaf Certificates**: Individual TLS certificates for each service. For example, the NATS server certificate (`nats-server-tls`) has:
- Subject Alternative Names (SANs) covering the in-cluster DNS name (`nats.tentacular-exoskeleton.svc.cluster.local`) and the pod name
- 1-year validity with automatic renewal 30 days before expiry
- Private key stored in a Kubernetes Secret accessible only to the NATS pod

### 6.5 Why the SPIRE CA key is not exportable

SPIRE manages its own CA independently of cert-manager. The SPIRE server generates and holds the CA private key in its own storage backend. This key is not exportable -- you cannot extract it and import it into cert-manager's CA issuer, or vice versa.

This is a security feature, not a limitation. SPIRE's threat model assumes that the CA key should never leave the SPIRE server process. If the key were exportable, a compromise of any other component (cert-manager, a Kubernetes Secret, a backup system) could allow an attacker to mint arbitrary SVIDs. By keeping the key internal to SPIRE, the only way to issue an SVID is through SPIRE's attestation workflow, which requires the requesting pod to prove its identity to the SPIRE agent.

The practical consequence is the dual-CA architecture described in Section 6.2: cert-manager issues server certificates, SPIRE issues client SVIDs, and NATS trusts both via a combined trust bundle.

For operational details on deploying and configuring cert-manager, see the [deployment guide](exoskeleton-deployment-guide.md).

---

## 7. Data Lifecycle

### 7.1 Overview

| Event | K8s resources | Backing-service data | Credentials |
|-------|---------------|---------------------|-------------|
| **Deploy** (first) | Created | Created (empty) | Generated |
| **Redeploy** | Replaced | Preserved | Rotated |
| **Undeploy** (cleanup OFF, default) | Deleted | Preserved | Revoked |
| **Undeploy** (cleanup ON) | Deleted | Destroyed | Revoked |
| **Redeploy after cleanup** | Created | Created (empty) | Generated |

### 7.2 Deploy (first time)

Step by step, here is what happens when a workflow with exoskeleton dependencies is deployed for the first time:

1. **Identity compilation**: `CompileIdentity(namespace, workflow)` produces the `Identity` struct with all service-specific identifiers.

2. **Postgres registration**: Creates the role with `LOGIN PASSWORD`, creates the schema with `AUTHORIZATION` set to the role, grants usage and default privileges. Returns connection details including the generated password.

3. **NATS registration**: In token mode, returns the shared URL and token with the scoped subject prefix. In SPIFFE mode, creates an authorization ConfigMap entry mapping the tentacle's SPIFFE URI to its permitted subjects.

4. **RustFS registration**: Ensures the bucket exists, creates the IAM user with a generated secret key, creates and attaches the prefix-scoped policy.

5. **SPIRE registration**: Creates a `ClusterSPIFFEID` CRD matching the workflow's pod labels and namespace.

6. **Secret creation**: `BuildSecretManifest()` produces a Kubernetes Secret with flat `<dep>.<field>` keys containing all credentials and connection details.

7. **Contract enrichment**: `enrichContractDeps()` updates the ConfigMap's `workflow.yaml` with resolved host/port/user/schema values. `patchDeploymentAllowNet()` adds exoskeleton service hostnames to the Deployment's `--allow-net` flags.

8. **Deployer annotation**: If SSO is active, `AnnotateDeployer()` adds provenance annotations to the Deployment.

9. **Kubernetes apply**: The MCP server applies the enriched manifests (ConfigMap, Secret, Deployment) to the cluster.

### 7.3 Redeploy

When the same workflow is deployed again to the same namespace, the exoskeleton performs idempotent re-registration:

- **Identity remains the same**: `CompileIdentity` produces identical identifiers because the namespace and workflow name have not changed. This is the foundation of idempotent re-registration.
- **Credentials rotate**: Every registrar generates new passwords/secret keys. The Postgres registrar issues `ALTER ROLE ... WITH PASSWORD '<new>'`. The RustFS registrar calls `AddUser` with the same access key but a new secret key. The old credentials become invalid when the new Secret is applied to Kubernetes.
- **Data is preserved**: The Postgres schema and its tables are untouched (`CREATE SCHEMA IF NOT EXISTS` is a no-op when the schema exists). RustFS objects under the prefix remain (the registrar does not touch the data plane during registration). NATS JetStream state persists (it is server-side state independent of client credentials).
- **Privileges are re-verified**: Postgres `GRANT` statements are re-applied. This is important because an administrator might have accidentally revoked privileges between deploys.
- **SPIRE entry is updated**: The `ClusterSPIFFEID` resource is updated with the same spec (preserving `resourceVersion`). If the spec has not changed, this is effectively a no-op.
- **The Secret is replaced**: The new Secret contains the rotated credentials. Kubernetes replaces the old Secret with the new one. The Deployment is also replaced, which triggers a new pod with the updated credentials.

### 7.4 Undeploy without cleanup (default)

When `cleanup_on_undeploy` is `false` (the default), `wf_remove` deletes only Kubernetes resources:

- **Deployment** is deleted. The engine pod terminates. Active database connections, NATS subscriptions, and S3 operations are interrupted.
- **ConfigMap** is deleted. The workflow.yaml and enriched contract are gone from the cluster, but the original source remains on disk.
- **Secret** is deleted. The credential values are removed from Kubernetes. However, the credentials themselves (Postgres password, RustFS secret key) remain valid on the backing services until a redeploy rotates them or cleanup destroys the accounts.
- **Backing-service state is completely untouched**: the Postgres schema and all its tables remain. RustFS objects under the prefix remain. NATS authorization entries remain. The ClusterSPIFFEID resource remains (SPIRE continues to watch for matching pods, though none exist).

A subsequent deploy of the same workflow to the same namespace reconnects to the existing data. The schema still contains its tables. The objects are still under the prefix. The new deploy generates fresh credentials, but because the schema and objects are addressed by the same deterministic identifiers, the reconnection is seamless.

This is the expected behavior for development workflows (where you want to iterate on code without losing test data) and production services (where an accidental undeploy should not destroy a database).

### 7.5 Undeploy with cleanup

When `cleanup_on_undeploy` is `true`, unregistration permanently destroys all backing-service data:

- **Postgres**: `DROP SCHEMA CASCADE` -- all tables, views, functions, indexes, and data within the schema are gone. The role is dropped.
- **RustFS**: All objects under the tentacle's prefix are recursively deleted. The IAM policy and user are removed.
- **NATS**: In SPIFFE mode, the authorization ConfigMap entry is removed. In token mode, this is a no-op (no per-tentacle artifacts exist).
- **SPIRE**: The `ClusterSPIFFEID` resource is deleted. The SPIRE controller removes the registration entry; the SVID expires naturally.

### 7.6 Why cleanup defaults to OFF

Data destruction is permanent and irreversible. There are several scenarios where the safe default matters:

- **Development iteration**: A developer undeploys a workflow to fix a bug and redeploys it. They expect their test data (Postgres tables populated with test fixtures, S3 objects uploaded for testing) to still be there. If cleanup were the default, every redeploy cycle would start from scratch.
- **Accidental undeploy**: An operator runs `tntc undeploy` on the wrong workflow in production. With cleanup off, the backing-service data survives. The workflow can be redeployed immediately with no data loss.
- **Namespace recycling**: A workflow is undeployed from one namespace and redeployed to another. The old namespace's data persists and can be cleaned up later, or the old namespace can be reused for the same workflow.
- **Audit and compliance**: Some environments require data retention even after workloads are decommissioned. Cleanup-off mode preserves all data for forensic or compliance purposes.

The safe default is to preserve data and require explicit opt-in for destruction via `TENTACULAR_EXOSKELETON_CLEANUP_ON_UNDEPLOY=true`.

### 7.7 CLI undeploy confirmation

When a user runs `tntc undeploy <workflow>` and cleanup is enabled, the CLI warns about data loss and requires explicit confirmation before proceeding. The warning lists the specific resources that will be destroyed (Postgres schema, RustFS objects, NATS artifacts, SPIRE identity).

The `--force` flag skips the prompt for automation use cases (CI/CD pipelines, scripted cleanup). Without `--force`, the CLI blocks and waits for the user to type `yes` or `y`.

### 7.8 Backward compatibility

The data lifecycle preserves full backward compatibility:

- **Workflows without `tentacular-*` dependencies** are completely unchanged. The exoskeleton controller checks for dependencies and short-circuits if none are found.
- **Workflows can mix user-managed and exoskeleton-managed dependencies** in the same contract. The engine resolves both through the same `ctx.dependency()` API.
- **The engine does not need code changes.** It reads whatever is in `/app/secrets`. Whether those secrets were created by the CLI from `.secrets.yaml` or by the MCP server's credential injector is irrelevant to the engine.
- **Simple-mode clusters reject `tentacular-*` dependencies at deploy time** with a clear error, never silently. If a workflow declares `tentacular-postgres` but the exoskeleton is disabled, the deployment fails immediately with `"workflow requires tentacular-postgres but TENTACULAR_EXOSKELETON_POSTGRES is not enabled or not configured"`.
- **Existing bearer-token auth continues to work** even when OIDC auth is enabled. The auth middleware tries both paths.

### 7.9 Operational considerations

**Credential rotation frequency**: Credentials rotate on every redeploy. For workflows that redeploy frequently (e.g., during development), this means the Postgres password changes on every `tntc deploy`. The old password becomes invalid immediately. If you have a database client connected with the old password, it will be disconnected on the next authentication attempt.

**Orphaned registrations**: If a workflow is deleted from disk but not undeployed (`tntc undeploy`), its backing-service registrations remain. The Postgres schema, RustFS objects, and NATS authorization entries persist indefinitely. There is no garbage collector. To clean up, either redeploy and then undeploy with cleanup enabled, or manually remove the resources.

**Cross-namespace deployment**: A workflow name is unique within a namespace but not across namespaces. Deploying `hn-digest` to both `tent-dev` and `tent-staging` creates two independent workspaces with separate schemas, prefixes, and credentials. The identity model guarantees collision-free identifiers because the namespace is part of every identifier.

---

## 8. Configuration

### 8.1 Environment variables

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

### 8.2 Helm values

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

### 8.3 Configuration profiles

#### Simple mode

```env
TENTACULAR_EXOSKELETON_ENABLED=false
```

Existing bearer-token auth. No service registration. No Keycloak/SPIRE dependency. This is the default.

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

## 9. Workflow Integration

### 9.1 Declaring exoskeleton dependencies

Workflows opt in to exoskeleton services through the existing `contract.dependencies` mechanism. No separate `exoskeleton:` block is needed:

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

The MCP server treats any dependency with the `tentacular-` prefix as an exoskeleton service request. The detection logic scans the ConfigMap containing `workflow.yaml`, parses the contract's dependencies block, and collects all names starting with `tentacular-`.

If the corresponding service is not enabled (e.g., the workflow declares `tentacular-postgres` but `TENTACULAR_POSTGRES_ADMIN_HOST` is not set), deployment fails with a clear error: `"workflow requires tentacular-postgres but TENTACULAR_EXOSKELETON_POSTGRES is not enabled or not configured"`.

A workflow with no `tentacular-*` dependencies deploys identically to simple mode, regardless of the cluster's exoskeleton configuration.

### 9.2 What the engine sees: enriched contract + mounted secrets

From the Deno workflow engine's perspective, there is no difference between exoskeleton-managed and manually-configured dependencies. The engine sees:

1. **An enriched ConfigMap** where `workflow.yaml` contains fully resolved dependency entries (host, port, user, schema, etc.) instead of just `protocol`.
2. **A mounted Secret** at `/app/secrets` containing credential keys in the `<dep>.<field>` format.

The engine's `resolveSecrets()` cascade reads `/app/secrets` at startup and populates the dependency map. Node code calls `ctx.dependency("tentacular-postgres")` and receives a complete object with all connection details.

### 9.3 Credential injection

The MCP server writes a Kubernetes Secret named `tentacular-exoskeleton-<workflow>` in the workflow namespace with flat `<dep>.<field>` keys:

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
  tentacular-nats.token: <shared token>
  tentacular-nats.subject_prefix: tentacular.tent-dev.hn-digest.>
  tentacular-nats.protocol: nats
  tentacular-nats.auth_method: token
  tentacular-rustfs.endpoint: http://rustfs-svc.tentacular-exoskeleton.svc.cluster.local:9000
  tentacular-rustfs.access_key: tn_tent_dev_hn_digest
  tentacular-rustfs.secret_key: <generated>
  tentacular-rustfs.bucket: tentacular
  tentacular-rustfs.prefix: ns/tent-dev/tentacles/hn-digest/
  tentacular-rustfs.region: us-east-1
  tentacular-rustfs.protocol: s3
  tentacular-identity.principal: spiffe://tentacular/ns/tent-dev/tentacles/hn-digest
  tentacular-identity.namespace: tent-dev
  tentacular-identity.workflow: hn-digest
```

### 9.4 Contract enrichment

The MCP server enriches the workflow's contract at deploy time. When a workflow declares `tentacular-*` dependencies with only `protocol`, the server fills in concrete connection details. The enrichment is service-specific:

| Service | Fields added |
|---------|-------------|
| Postgres | `host`, `port`, `database`, `user`, `schema` |
| NATS | `host`, `port`, `subject` |
| RustFS | `host`, `port`, `container` (bucket name), `prefix` |

The `enrichContractDeps()` function in `enrich.go` performs the enrichment:

1. Scans the manifest list for a ConfigMap containing `workflow.yaml`.
2. Parses the workflow YAML into a generic `map[string]interface{}` (not a typed struct) to preserve all fields during round-trip serialization.
3. For each `tentacular-*` dependency that has matching credentials, sets the appropriate fields in the dependency map.
4. Re-serializes the workflow YAML and updates the ConfigMap in place.

After contract enrichment, `patchDeploymentAllowNet()` modifies the Deployment manifest:

1. Collects `host:port` strings from each registered service (parsing URLs like `nats://host:4222` into `host:4222`).
2. Scans the first container's `args` (and falls back to `command`) for a `--allow-net=...` flag.
3. Appends the exoskeleton service hosts to the existing value. For example, `--allow-net=api.example.com:443` becomes `--allow-net=api.example.com:443,postgres-postgresql.tentacular-exoskeleton.svc.cluster.local:5432,nats.tentacular-exoskeleton.svc.cluster.local:4222`.

This is necessary because Deno runs with explicit network permissions. Without the patched `--allow-net` flag, the workflow would fail at runtime when attempting to connect to exoskeleton services.

Enrichment happens server-side in the `wf_apply` handler, after registration but before manifest generation. The original `workflow.yaml` on disk is not modified. The workflow author never sees the enriched version -- it exists only in the Kubernetes ConfigMap.

### 9.5 ctx.dependency() resolution

At runtime, node code accesses backing services through the standard dependency API:

```typescript
// All connection details are resolved from the Secret + enriched contract
const pg = ctx.dependency("tentacular-postgres");
// pg.host, pg.port, pg.database, pg.user, pg.password, pg.schema

const nats = ctx.dependency("tentacular-nats");
// nats.url, nats.token (if token mode), nats.subject_prefix

const s3 = ctx.dependency("tentacular-rustfs");
// s3.endpoint, s3.access_key, s3.secret_key, s3.bucket, s3.prefix
```

The values come from two sources merged by the engine:

1. **Contract fields** (host, port, protocol, etc.) from the enriched ConfigMap
2. **Secret fields** (password, access_key, secret_key, token) from the mounted exoskeleton Secret

### 9.6 Mixed dependencies

A workflow can mix exoskeleton-managed and manually-configured dependencies in the same contract:

```yaml
contract:
  dependencies:
    tentacular-postgres:
      protocol: postgresql
    tentacular-nats:
      protocol: nats
    my-external-api:
      protocol: https
      host: api.example.com
      port: "443"
```

The MCP server provisions credentials for `tentacular-postgres` and `tentacular-nats`, while `my-external-api` is resolved from user-managed secrets (`.secrets.yaml` or `.secrets/` directory) as it always has been. The engine does not distinguish between the two; both end up in the dependency map via the same `resolveSecrets()` cascade.

### 9.7 Example: workflow with exoskeleton

A complete workflow that uses all three exoskeleton services:

```yaml
# workflow.yaml
name: hn-digest
version: 0.1.0
contract:
  dependencies:
    tentacular-postgres:
      protocol: postgresql
    tentacular-nats:
      protocol: nats
    tentacular-rustfs:
      protocol: s3
  schedule: "0 */6 * * *"
nodes:
  - name: fetch
    type: deno
    entry: nodes/fetch.ts
  - name: store
    type: deno
    entry: nodes/store.ts
    deps: [fetch]
```

The `fetch` node might use NATS to publish digested articles:

```typescript
// nodes/fetch.ts
import { connect, StringCodec } from "nats";

export default async function(ctx: any) {
  const nats = ctx.dependency("tentacular-nats");
  const nc = await connect({
    servers: nats.url,
    token: nats.token,  // token mode; in SPIFFE mode, auth is via SVID
  });
  const sc = StringCodec();

  // Publish to the tentacle's scoped subject prefix
  const subject = `tentacular.${nats.subject_prefix.replace(".>", "")}.articles`;
  nc.publish(subject, sc.encode(JSON.stringify(articles)));
  await nc.drain();
}
```

The `store` node might use Postgres and RustFS:

```typescript
// nodes/store.ts
import postgres from "postgres";

export default async function(ctx: any) {
  const pg = ctx.dependency("tentacular-postgres");
  const sql = postgres({
    hostname: pg.host,
    port: Number(pg.port),
    database: pg.database,
    username: pg.user,
    password: pg.password,
  });

  // Tables are created in the tentacle's dedicated schema
  await sql`SET search_path TO ${sql(pg.schema)}`;
  await sql`CREATE TABLE IF NOT EXISTS articles (
    id SERIAL PRIMARY KEY,
    title TEXT NOT NULL,
    url TEXT NOT NULL,
    fetched_at TIMESTAMPTZ DEFAULT NOW()
  )`;

  // ... store articles ...
  await sql.end();
}
```

### 9.8 Example: workflow without exoskeleton

A workflow that does not use exoskeleton services deploys identically to simple mode:

```yaml
# workflow.yaml
name: simple-cron
version: 0.1.0
contract:
  dependencies:
    my-api:
      protocol: https
      host: api.example.com
      port: "443"
  schedule: "0 * * * *"
nodes:
  - name: check
    type: deno
    entry: nodes/check.ts
```

No `tentacular-*` dependencies means the exoskeleton is completely transparent. The MCP server's `ProcessManifests` detects zero exoskeleton dependencies and returns the manifests unchanged.

### 9.9 Example: mixed dependencies

A workflow can combine exoskeleton-managed and user-managed dependencies:

```yaml
contract:
  dependencies:
    tentacular-postgres:
      protocol: postgresql
    github-api:
      protocol: https
      host: api.github.com
      port: "443"
    slack-webhook:
      protocol: https
      host: hooks.slack.com
      port: "443"
```

The MCP server provisions `tentacular-postgres` automatically. The user provides credentials for `github-api` and `slack-webhook` via `.secrets.yaml` as usual. Both coexist in the engine's dependency map.

### 9.10 MCP tools

| Tool | Description |
|------|-------------|
| `exo_status` | Returns exoskeleton configuration state (enabled services, auth status) |
| `exo_registration` | Returns registration details for a specific tentacle |

---

## 10. Deployment Guide

This section covers the essential deployment steps. For NATS SPIFFE mTLS deployment specifically, see the [NATS SPIFFE mTLS Activation Guide](nats-spiffe-deployment.md).

### 10.1 Prerequisites

| Service | Requirement |
|---------|-------------|
| Postgres | Dedicated instance, `tentacular` database, admin role with `CREATEROLE` |
| NATS | Dedicated instance, token auth or TLS + SPIFFE, optional JetStream |
| RustFS | Dedicated instance, `tentacular` bucket pre-created |
| SPIRE | Server + agent in `tentacular-system`, trust domain `tentacular`. The MCP service account's ClusterRole must include permissions for `spire.spiffe.io` resources (e.g., `clusterspiffeids`). The Helm chart includes this, but verify on live clusters. |
| Keycloak | Realm `tentacular`, confidential client `tentacular-mcp` with device auth |
| Ingress + TLS | Required for Keycloak SSO (Google rejects non-HTTPS redirect URIs) |

### 10.2 Deployment order

The services must be deployed in dependency order. The reason for this ordering is that each subsequent service may depend on infrastructure provided by earlier ones:

1. **Ingress controller + cert-manager** (if SSO or SPIFFE mode needed). cert-manager provisions TLS certificates for ingress and internal services. Without it, Keycloak cannot serve HTTPS (Google rejects non-HTTPS redirect URIs), and NATS cannot get a server TLS certificate from the internal CA.

2. **Postgres**. No dependencies on other exoskeleton services. Verify connectivity: `psql -h <host> -U tentacular_admin -d tentacular -c 'SELECT 1'`.

3. **NATS**. No dependencies on other exoskeleton services for token mode. For SPIFFE mode, NATS depends on cert-manager (for its server certificate) and SPIRE (for the trust bundle). If deploying in SPIFFE mode, deploy SPIRE first.

4. **RustFS**. No dependencies on other exoskeleton services. After deployment, ensure the `tentacular` bucket exists: `mc mb rustfs/tentacular` or let the registrar create it on first deploy.

5. **Keycloak** (requires ingress + TLS for Google SSO). The Google OAuth consent screen requires HTTPS redirect URIs. Keycloak must be accessible at a stable HTTPS URL before configuring the Google IdP.

6. **SPIRE** (server + agent + controller-manager in `tentacular-system`). The controller-manager watches for `ClusterSPIFFEID` resources and creates SPIRE registration entries. Verify: `kubectl get clusterspiffeids` should work without errors.

7. **`tentacular-mcp`** with exoskeleton feature flags. This is the last component because it connects to all the services above at startup. If a service is unreachable, the corresponding registrar fails to initialize, and the MCP server starts without it (logging a warning).

Each service is independently optional. The MCP server gracefully handles disabled services: workflows that declare dependencies on a disabled service fail at deploy time with a clear error, never silently. This means you can deploy incrementally -- start with just Postgres, add NATS later, add RustFS later, add SPIRE and SSO when ready.

### 10.3 Bootstrap SQL for Postgres

The admin role needs `CREATEROLE` but not `SUPERUSER`:

```sql
CREATE DATABASE tentacular;
CREATE ROLE tentacular_admin WITH LOGIN PASSWORD '<strong-password>' CREATEROLE;
GRANT ALL PRIVILEGES ON DATABASE tentacular TO tentacular_admin;
```

As a hardening step, revoke default public schema access:

```sql
\c tentacular
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
```

### 10.4 Google SSO setup in Keycloak

1. **Google Cloud Console:** Create an OAuth 2.0 Client ID (Web application). Add redirect URI: `https://auth.<cluster>.<domain>/realms/tentacular/broker/google/endpoint`
2. **Keycloak admin:** In the `tentacular` realm, add Google as an identity provider. Set Client ID and Client Secret from the Google console. Enable `Trust Email`. Ensure `email`, `name`, `sub` claims are mapped.
3. **Restrict SSO domain:** In the Google IdP configuration, set the `Hosted Domain` parameter to the allowed Google Workspace domain (e.g., `mirantis.com`). This ensures only accounts from that domain can authenticate.
4. **Configure the tentacular-mcp client:**
   - Client ID: `tentacular-mcp`
   - Client type: Confidential
   - OAuth 2.0 Device Authorization Grant: enabled
   - Valid redirect URIs: `https://mcp.<cluster>.<domain>/*`
   - Scopes: `openid`, `profile`, `email`
5. **Verify:** Visit `https://auth.<cluster>.<domain>/realms/tentacular/account/` and sign in with a Google account from the allowed domain.

### 10.5 Helm install

Production deployment with existing secrets:

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

The `existingSecret` pattern keeps credentials out of Helm values files. Create the secrets before running Helm:

```bash
kubectl create secret generic tentacular-mcp-postgres-admin \
  -n tentacular-system \
  --from-literal=host=postgres-postgresql.tentacular-exoskeleton.svc.cluster.local \
  --from-literal=port=5432 \
  --from-literal=database=tentacular \
  --from-literal=user=tentacular_admin \
  --from-literal=password='<strong-password>'
```

### 10.6 Dev build and deploy cycle

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

### 10.7 Service endpoints (eastus-dev cluster)

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

### 10.8 Verifying the deployment

After deploying all services, verify the exoskeleton is operational:

```bash
# Check MCP server logs for registrar initialization
kubectl -n tentacular-system logs deploy/tentacular-mcp | grep "exoskeleton:"
# Expected output:
#   exoskeleton: postgres registrar initialized
#   exoskeleton: nats registrar initialized
#   exoskeleton: rustfs registrar initialized
#   exoskeleton: spire registrar initialized className=tentacular-system-spire
#   exoskeleton: controller ready postgres=true nats=true rustfs=true spire=true

# Test with exo_status MCP tool
tntc mcp exo_status
```

If any registrar fails to initialize (e.g., Postgres is unreachable), the MCP server logs the error and starts without that registrar. Workflows declaring a dependency on the failed service will get a clear error at deploy time.

---

## 11. Roadmap

For the development roadmap, see [docs/roadmap.md](roadmap.md).

---

## 12. Known Limitations

| Limitation | Impact | Mitigation |
|------------|--------|------------|
| NATS SPIRE CA bundle sync is manual | NATS server TLS is automated via cert-manager (internal CA, auto-renewed). However, when SPIRE rotates its CA, the combined trust bundle in the `nats-spire-ca` Secret must be refreshed manually. | Future: sidecar or CronJob to watch `spire-bundle` and sync. See [NATS SPIFFE mTLS Activation Guide](nats-spiffe-deployment.md#certificate-rotation). |
| NATS token mode (fallback) | Convention-only subject isolation when SPIFFE mode is not enabled. A tentacle can publish to another tentacle's subject prefix. | Enable SPIFFE mode for cryptographically enforced isolation. |
| RustFS alpha admin API | Returns HTTP 500 instead of 404 for missing resources. | Registrar treats 500 as not-found for idempotent operations. |
| Keycloak azp vs aud | Access tokens use `azp` not `aud` for client ID. Standard OIDC libraries do not validate `azp` by default. | Validator skips `aud` check, validates `azp` instead. |
| k0s kubelet path | SPIRE CSI driver hardcodes `/var/lib/kubelet/`; k0s uses `/var/lib/k0s/kubelet/`. | CSI driver disabled. SPIRE server + agent work without it. |
| No provenance persistence | Deployer identity is annotations-only, not queryable via a dedicated API. | Sufficient for Phase 1 audit trail via `kubectl` / `wf_describe`. |
| ARM64 Bitnami images | Debian-tagged Bitnami images lack ARM64 builds. | Keycloak uses official `quay.io` image. Others use `:latest` or non-Bitnami charts. |
| No compensating rollback | If Postgres registration succeeds but NATS fails, the Postgres role becomes orphaned until the next deploy or cleanup. | Registrations are idempotent; the next deploy re-registers cleanly. Future: compensating rollback logic. |
| SPIRE registration non-fatal | A failed SPIRE registration means the pod does not receive an SVID, degrading SPIFFE-mode NATS to failure. | Operator should verify SPIRE is healthy before enabling SPIFFE NATS mode. |
| JetStream streams not cleaned up | NATS unregistration does not delete JetStream streams or consumers owned by the tentacle. | Manual cleanup via NATS admin tools. Future: track stream ownership in exoskeleton metadata. |
| Single-cluster only | The exoskeleton manages backing services in one cluster. Cross-cluster identity and registration are not supported. | Phase 2 may add multi-cluster federation. |
| No credential caching | Each deploy regenerates all credentials, even if the existing ones are still valid. | This is by design (rotation on every deploy). Future: optional skip-rotation flag for development. |
| Postgres public schema | The registrar does not revoke default grants on the `public` schema. A tentacle could create tables in `public`. | Operator should run `REVOKE CREATE ON SCHEMA public FROM PUBLIC` as a hardening step. |
