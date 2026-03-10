# Exoskeleton Phase 1 - MCP Server

## Problem
Tentacular workflows that need database, messaging, or object storage must manually configure all connection details. This is error-prone and creates friction for both human developers and AI agents.

## Solution
Add an exoskeleton control plane inside tentacular-mcp that automatically provisions scoped credentials for Postgres, NATS, and RustFS when workflows declare `tentacular-*` dependencies.

## Scope (tentacular-mcp only)
- Identity compiler: deterministic mapping from (namespace, workflow) to service-specific identifiers
- Config loader: feature-flagged configuration from environment variables
- Postgres registrar: create role, schema, grant privileges (pgx driver)
- NATS registrar: shared token auth with scoped subject convention
- RustFS registrar: per-user IAM via madmin-go with prefix-scoped policies
- Credential injector: build K8s Secret manifests with JSON-structured per-service credentials
- Controller: orchestrate registration during wf_apply, unregistration during wf_remove
- Guard update: protect tentacular-exoskeleton namespace
- MCP tools: exo_status and exo_registration
- Helm chart: exoskeleton configuration block and conditional env vars

## Non-goals
- SPIRE integration (Phase 2)
- Keycloak/auth flows (Phase 2)
- Provenance store (Phase 2)
- Per-user NATS JWT (Phase 2)
