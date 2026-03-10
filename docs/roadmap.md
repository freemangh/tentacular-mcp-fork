# Tentacular MCP Roadmap

Last updated: 2026-03-10

## Active

### P0 — In Progress

No items currently in progress.

### P1 — Next Up

Items planned for the near term.

| Item | Description | Dependencies | Target |
|------|-------------|--------------|--------|
| Per-tentacle operation locking | The MCP server handles requests concurrently (Go HTTP, goroutine per request). Concurrent deploys for different tentacles that both modify the NATS authz ConfigMap race on resourceVersion — one succeeds, one gets 409 Conflict with no retry. Fix: per-tentacle mutex keyed on `namespace/workflow` in the controller, plus retry-on-conflict for all shared-state K8s writes (ConfigMap, Secret). Also protects against concurrent Postgres role creation and RustFS IAM user creation races. | None | TBD |
| SPIRE CA TTL tuning | Current `ca_ttl: 24h` (SPIRE default) causes CA rotation every ~12 hours, requiring frequent trust bundle syncs to the NATS `nats-spire-ca` Secret. Increase to `168h` (7 days) for production. SVID TTL (4h) is fine — short-lived workload certs are the design intent. The CA TTL controls signing authority rotation, not workload cert lifetime. Weekly CA rotation reduces operational risk without weakening the security model. | SPIRE config change | TBD |
| SPIRE trust bundle auto-sync | Automate the sync from SPIRE's `spire-bundle` ConfigMap (JWKS format) to the NATS `nats-spire-ca` Secret (PEM format). Options: CronJob with JWKS-to-PEM conversion, sidecar in the NATS pod, or a controller that watches the ConfigMap. Currently manual — must be refreshed when SPIRE rotates its CA. With `ca_ttl: 168h`, this becomes a weekly operation rather than twice-daily, but still must be automated for production. | SPIRE CA TTL tuning | TBD |
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
| SPIRE HA (multi-replica) | Current SPIRE server uses SQLite (single-instance only). For production HA: migrate datastore to exoskeleton Postgres, scale SPIRE server StatefulSet to 3 replicas. Each replica maintains its own CA (or shares via UpstreamAuthority). Note: SPIRE HA does NOT fix the orphaned ClusterSPIFFEID issue — that's a K8s API concern handled by the security posture validator and retry-on-failure logic. | Exoskeleton Postgres |
| Orphaned ClusterSPIFFEID cleanup | If ClusterSPIFFEID deletion fails during undeploy (best-effort), orphaned identities accumulate. Two mitigations: (1) the security posture validator audits for orphans by comparing ClusterSPIFFEIDs against active workflows, (2) the MCP server retries failed deletions on startup or on a schedule. | Security posture validation |
| Cluster security posture validation | Internal MCP server background process (not exposed as a tool) that periodically validates all exoskeleton security controls: NetworkPolicies between namespaces, RBAC scoping, PSA labels, SPIRE registration entries match deployed tentacles, TLS certificates valid and not expiring, ingress/egress rules in place, ServiceAccount token mount disabled on data-plane pods. Runs on a configurable schedule. Notifies administrators via a TBD mechanism (webhook, Slack, cluster events, or logging/alerting integration) when violations or drift are detected. | Exoskeleton Phase 1, SPIRE |
| Istio ambient mode compatibility | SPIFFE IDs become the service mesh identity layer. Zero-config mTLS for all tentacle-to-service traffic. SPIRE SVIDs are directly compatible with Istio ambient mode's identity model — ambient uses SPIFFE URIs from client certs for authorization. | SPIRE, NATS SPIFFE mode |
| Per-service enable/disable flags | Explicit `TENTACULAR_EXOSKELETON_<SERVICE>_ENABLED` flags for cases where admin credentials are configured but the service should not be offered to tentacles. | None |
| NATS config auto-reload | Automatically reload NATS server configuration when registration changes occur (authorization ConfigMap updates). Currently relies on the config reloader sidecar watching the mounted ConfigMap volume. | NATS SPIFFE mode |

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
