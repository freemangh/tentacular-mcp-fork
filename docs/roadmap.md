# Tentacular MCP Roadmap

Last updated: 2026-03-10

## Active

### P0 — In Progress

No items currently in progress.

### P1 — Next Up

Items planned for the near term.

| Item | Description | Dependencies | Target |
|------|-------------|--------------|--------|
| Vault integration | SVIDs become auth credentials for HashiCorp Vault. Deployer identity maps to a Vault policy. Vault issues short-lived Postgres credentials, NATS tokens, and S3 presigned URLs. Replaces static passwords with time-limited, automatically-rotated secrets. | SPIRE, OIDC auth | TBD |
| Provenance persistence | Persist deployment records (who, when, what, from where) in exoskeleton Postgres for querying and audit. | Exoskeleton Postgres | TBD |
| Registration audit log | Record all register/re-register/unregister events with timestamps, identities, and outcomes. | Exoskeleton Postgres | TBD |
| Credential rotation history | Track when credentials were rotated and by whom. | Provenance persistence | TBD |
| Deployment diff tracking | Record what changed between deploys (contract changes, config changes, node changes). | Provenance persistence | TBD |
| Audit API | MCP tools for querying provenance history by workflow, namespace, deployer, or time range. | Provenance persistence | TBD |
| Observability integration | Export audit events to cluster logging/monitoring for dashboards and alerting. | Audit API | TBD |

### P2 — Planned

Items planned but not yet scheduled.

| Item | Description | Dependencies |
|------|-------------|--------------|
| Cluster security posture validation | Internal MCP server background process (not exposed as a tool) that periodically validates all exoskeleton security controls: NetworkPolicies between namespaces, RBAC scoping, PSA labels, SPIRE registration entries match deployed tentacles, TLS certificates valid and not expiring, ingress/egress rules in place, ServiceAccount token mount disabled on data-plane pods. Runs on a configurable schedule. Notifies administrators via a TBD mechanism (webhook, Slack, cluster events, or logging/alerting integration) when violations or drift are detected. | Exoskeleton Phase 1, SPIRE |
| Istio ambient mode compatibility | SPIFFE IDs become the service mesh identity layer. Zero-config mTLS for all tentacle-to-service traffic. | SPIRE, NATS SPIFFE mode |
| Per-service enable/disable flags | Explicit `TENTACULAR_EXOSKELETON_<SERVICE>_ENABLED` flags for cases where admin credentials are configured but the service should not be offered to tentacles. | None |
| NATS config auto-reload | Automatically reload NATS server configuration when registration changes occur (authorization ConfigMap updates). | NATS SPIFFE mode |

## Archive

Completed items, most recent first.

### 2026-03-10 — NATS Server TLS (cert-manager)

| Item | Completed | Notes |
|------|-----------|-------|
| NATS server TLS via cert-manager | 2026-03-10 | Internal CA (`tentacular-internal-ca`) with 10y validity. Server cert auto-renewed 30 days before expiry. Combined trust bundle for SPIRE CA + cert-manager CA. Deployed on eastus-dev. |

### 2026-03-10 — Exoskeleton Phase 1

| Item | Completed | Notes |
|------|-----------|-------|
| Identity compiler | 2026-03-10 | `(namespace, workflow)` to deterministic identifiers across all services |
| Postgres registrar | 2026-03-10 | Role, schema, grants, cleanup lifecycle |
| NATS registrar (dual-mode) | 2026-03-10 | SPIFFE mTLS + token auth modes, scoped subject prefix |
| RustFS registrar | 2026-03-10 | Native admin API, prefix-scoped IAM policy |
| SPIRE registrar | 2026-03-10 | ClusterSPIFFEID resource provisioning via K8s API |
| Credential injector | 2026-03-10 | K8s Secret with `<dep>.<field>` keys per workflow |
| Controller + wf_apply/wf_remove integration | 2026-03-10 | Orchestrates full registration lifecycle |
| Contract enrichment + Deployment patching | 2026-03-10 | Server-side ConfigMap enrichment and `--allow-net` patching |
| OIDC/SSO authentication | 2026-03-10 | Keycloak + Google SSO, dual auth model (bearer + OIDC) |
| Deployer provenance annotations | 2026-03-10 | `tentacular.io/deployed-by`, `deployed-at`, `deployed-via` on Deployments |
| exo_status and exo_registration MCP tools | 2026-03-10 | Exoskeleton configuration state and per-tentacle registration details |
| Guard namespace protection | 2026-03-10 | Prevents operations on protected system namespaces |
| Helm chart exoskeleton config | 2026-03-10 | `exoskeleton` and `exoskeletonAuth` values sections with `existingSecret` support |
| Consolidated exoskeleton documentation | 2026-03-10 | Architecture diagram, full reference doc, deployment guide |
