# Glossary

Working glossary of Tentacular-specific terms. Will be unified into a central glossary when documentation moves to a dedicated repo.

| Term | Definition |
|------|-----------|
| **Tentacle** | A deployed workflow. The unit of work in Tentacular — a DAG of nodes with a contract, triggers, and configuration. Identified by `(namespace, workflow name)`. |
| **Workspace** | The scoped bundle of backing-service resources provisioned for a tentacle by the exoskeleton. Includes a Postgres schema, NATS subject prefix, S3 object prefix, and SPIFFE identity. Deterministically derived from the tentacle's identity. |
| **Exoskeleton** | Optional, feature-flagged extension that provisions per-tentacle workspaces. Manages registration lifecycle for Postgres, NATS, RustFS, and SPIRE. |
| **Registrar** | A component that provisions scoped access for a tentacle in a specific backing service. Four registrars exist: Postgres (role+schema), NATS (authorization entry), RustFS (IAM user+policy), SPIRE (ClusterSPIFFEID). |
| **Contract** | The `contract:` section of workflow.yaml declaring dependencies, auth requirements, and network policy. Drives NetworkPolicy generation, Deno permission flags, and exoskeleton provisioning. |
| **Dependency** | A service a workflow needs, declared in `contract.dependencies`. Manual deps require all fields; `tentacular-*` deps are auto-provisioned by the exoskeleton. |
| **Identity** | The deterministic set of identifiers derived from `(namespace, workflow)`. Includes SPIFFE URI, Postgres role/schema, NATS user/prefix, S3 user/prefix. Computed by the identity compiler. |
| **SVID** | SPIFFE Verifiable Identity Document. An X.509 certificate issued by SPIRE that proves a workload's identity. Contains the SPIFFE URI in the Subject Alternative Name (SAN). |
| **ClusterSPIFFEID** | A Kubernetes CRD that tells the SPIRE controller which pods should receive SVIDs. The SPIRE registrar creates one per tentacle. |
| **Trust bundle** | The set of CA certificates needed to verify SVIDs. Published by SPIRE in the `spire-bundle` ConfigMap. |
| **Dual auth** | The MCP server's authentication model: OIDC tokens (from Keycloak/Google SSO) are tried first, bearer tokens are the fallback. Both always work. |
| **Deployer provenance** | Kubernetes annotations (`tentacular.io/deployed-by`, `deployed-at`, `deployed-via`) on Deployment resources recording who deployed a tentacle. |
| **Device Authorization Grant** | The OAuth 2.0 flow used by `tntc login`. The CLI displays a code, the user authenticates in a browser, the CLI polls until complete. Works for headless/agent scenarios. |
| **Contract enrichment** | The MCP server filling in host/port/database/user fields on `tentacular-*` dependencies in the workflow.yaml ConfigMap at deploy time. |
| **Credential injection** | The MCP server building a K8s Secret with per-service credentials (flat `<dep>.<field>` keys) and appending it to the deployment manifests. |
| **wf_apply** | The MCP tool that applies workflow manifests to the cluster. The exoskeleton controller intercepts this to run registrars, enrich contracts, and inject credentials. |
| **wf_remove** | The MCP tool that removes a workflow's K8s resources. When cleanup is enabled, also runs unregistrars to destroy backing-service data. |
| **Cleanup** | Destructive removal of exoskeleton backing-service data (Postgres schema CASCADE, RustFS object deletion, NATS authz entry removal). Off by default. Requires explicit confirmation via `--force`. |
