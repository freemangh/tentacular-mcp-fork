# NATS SPIFFE mTLS Activation Guide

Reconfigure the NATS server on a Tentacular cluster to use SPIFFE mTLS for per-tentacle authentication and authorization. The MCP server registrar code (`pkg/exoskeleton/registrar_nats.go`) is already built and tested. This guide covers the infrastructure changes required to activate it.

## Prerequisites

| Requirement | Details |
|-------------|---------|
| SPIRE server and agent | Running in `tentacular-system`, trust domain `tentacular` |
| NATS 2.12+ | Deployed in `tentacular-exoskeleton` namespace |
| cert-manager v1.12+ | For issuing the NATS server TLS certificate via internal CA |
| MCP server | Built with exoskeleton support, `TENTACULAR_NATS_SPIFFE_ENABLED=true` ready |
| kubectl access | Admin kubeconfig for the target cluster |
| ClusterSPIFFEID CRD | Installed by SPIRE (verify: `kubectl get crd clusterspiffeids.spire.spiffe.io`) |

Set your kubeconfig for all commands in this guide:

```bash
export KUBECONFIG=~/dev-secrets/kubeconfigs/eastus-admin.kubeconfig
```

> **Status:** These resources are deployed and working on the `eastus-dev` cluster as of 2026-03-10.

---

## Trust Chain Architecture

NATS mTLS requires two independent trust chains:

```
Server certificate chain (cert-manager):
  tentacular-internal-ca (self-signed root)
    └── nats-server-tls (server cert, 1-year validity)

Client certificate chain (SPIRE):
  SPIRE CA (managed internally by SPIRE server)
    └── Workload X.509 SVIDs (issued via SPIRE Agent Unix socket)
```

**Why two CAs?** SPIRE manages its own CA internally and does not expose the CA private key. The SPIRE CA key is not exportable, so it cannot be used as a cert-manager Issuer. Instead, cert-manager maintains its own internal CA (`tentacular-internal-ca`) for server certificates, while SPIRE issues client SVIDs through the Workload API.

NATS trusts both CAs via a combined trust bundle stored in the `nats-spire-ca` Secret. This bundle contains the SPIRE CA certificate(s) (for verifying client SVIDs) and the cert-manager internal CA certificate (for the server cert chain).

**No inbound connections to `tentacular-system`:** SPIRE Agent runs as a DaemonSet on each node. Workloads obtain SVIDs via a Unix domain socket on the local node, not via network calls.

---

## Step 1: Create Combined Trust Bundle

The NATS server needs a CA bundle that trusts both SPIRE-issued client SVIDs and its own server certificate chain (issued by the cert-manager internal CA). This step creates a combined PEM bundle containing both CA certificates.

### 1.1 Extract the SPIRE CA certificate

The SPIRE trust bundle is stored in ConfigMap `spire-bundle` in `tentacular-system`. The bundle is JWKS format with `x5c` certificates. Extract them as PEM:

```bash
kubectl -n tentacular-system get configmap spire-bundle \
  -o jsonpath='{.data.bundle\.spiffe}' | \
python3 -c "
import json, sys, base64
data = json.load(sys.stdin)
for key in data['keys']:
    if key.get('use') == 'x509-svid' and 'x5c' in key:
        for cert in key['x5c']:
            print('-----BEGIN CERTIFICATE-----')
            for i in range(0, len(cert), 64):
                print(cert[i:i+64])
            print('-----END CERTIFICATE-----')
" > /tmp/spire-ca.pem
```

Verify the extracted certificate:

```bash
openssl x509 -in /tmp/spire-ca.pem -noout -subject -issuer -dates
```

### 1.2 Extract the cert-manager internal CA certificate

After Step 2 creates the cert-manager internal CA, extract its certificate:

```bash
kubectl -n tentacular-exoskeleton get secret tentacular-internal-ca-tls \
  -o jsonpath='{.data.ca\.crt}' | base64 -d > /tmp/certmanager-ca.pem
```

### 1.3 Create the combined bundle Secret

Concatenate both CA certificates into a single PEM bundle and create the Secret:

```bash
cat /tmp/spire-ca.pem /tmp/certmanager-ca.pem > /tmp/combined-ca.pem

kubectl -n tentacular-exoskeleton create secret generic nats-spire-ca \
  --from-file=ca.pem=/tmp/combined-ca.pem \
  --dry-run=client -o yaml | kubectl apply -f -
```

Clean up:

```bash
rm /tmp/spire-ca.pem /tmp/certmanager-ca.pem /tmp/combined-ca.pem
```

---

## Step 2: Create NATS Server TLS Certificate (cert-manager Internal CA)

The NATS server needs its own TLS certificate, issued by a cert-manager internal CA. This is the **primary and recommended approach** -- deployed and tested on `eastus-dev`.

### 2.1 Bootstrap the internal CA

Create a self-signed ClusterIssuer, then use it to issue a long-lived CA certificate. The CA certificate is stored as a Secret (`tentacular-internal-ca-tls`) and referenced by a namespace-scoped Issuer.

```yaml
# 1. Self-signed ClusterIssuer (bootstraps the CA)
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: tentacular-selfsigned-bootstrap
spec:
  selfSigned: {}
---
# 2. CA certificate (10-year validity, auto-renewed 1 year before expiry)
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: tentacular-internal-ca
  namespace: tentacular-exoskeleton
spec:
  isCA: true
  secretName: tentacular-internal-ca-tls
  issuerRef:
    name: tentacular-selfsigned-bootstrap
    kind: ClusterIssuer
  commonName: tentacular-internal-ca
  duration: 87600h     # 10 years
  renewBefore: 8760h   # 1 year before expiry
  privateKey:
    algorithm: ECDSA
    size: 256
---
# 3. CA Issuer (references the CA Secret)
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: tentacular-internal-ca
  namespace: tentacular-exoskeleton
spec:
  ca:
    secretName: tentacular-internal-ca-tls
```

Apply it:

```bash
kubectl apply -f tentacular-internal-ca.yaml
```

Verify the CA certificate was issued:

```bash
kubectl -n tentacular-exoskeleton get certificate tentacular-internal-ca
kubectl -n tentacular-exoskeleton get secret tentacular-internal-ca-tls
```

### 2.2 Issue the NATS server certificate

Create a Certificate resource issued by the internal CA:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: nats-server-tls
  namespace: tentacular-exoskeleton
spec:
  secretName: nats-server-tls
  issuerRef:
    name: tentacular-internal-ca
    kind: Issuer
  commonName: nats.tentacular-exoskeleton.svc.cluster.local
  dnsNames:
    - nats
    - nats.tentacular-exoskeleton
    - nats.tentacular-exoskeleton.svc
    - nats.tentacular-exoskeleton.svc.cluster.local
    - "*.nats-headless.tentacular-exoskeleton.svc.cluster.local"
  duration: 8760h    # 1 year
  renewBefore: 720h  # 30 days
  privateKey:
    algorithm: ECDSA
    size: 256
```

Apply it:

```bash
kubectl apply -f nats-server-tls.yaml
```

Verify the Secret was created and the certificate is valid:

```bash
kubectl -n tentacular-exoskeleton get certificate nats-server-tls
kubectl -n tentacular-exoskeleton get secret nats-server-tls
```

### Resources summary

| Resource | Kind | Namespace | Purpose |
|----------|------|-----------|---------|
| `tentacular-selfsigned-bootstrap` | ClusterIssuer | (cluster-scoped) | Bootstraps the internal CA |
| `tentacular-internal-ca` | Certificate | `tentacular-exoskeleton` | CA cert (10y validity, auto-renewed) |
| `tentacular-internal-ca-tls` | Secret | `tentacular-exoskeleton` | CA cert + key (created by cert-manager) |
| `tentacular-internal-ca` | Issuer | `tentacular-exoskeleton` | Issues server certs using the CA |
| `nats-server-tls` | Certificate | `tentacular-exoskeleton` | NATS server cert (1y validity, auto-renewed) |
| `nats-server-tls` | Secret | `tentacular-exoskeleton` | Server cert + key (created by cert-manager) |

### Fallback: Self-signed certificate (dev only)

For quick local testing without cert-manager, generate a self-signed cert directly:

```bash
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
  -keyout /tmp/nats-key.pem -out /tmp/nats-cert.pem \
  -days 365 -nodes \
  -subj "/CN=nats.tentacular-exoskeleton.svc.cluster.local" \
  -addext "subjectAltName=DNS:nats,DNS:nats.tentacular-exoskeleton,DNS:nats.tentacular-exoskeleton.svc.cluster.local"

kubectl -n tentacular-exoskeleton create secret tls nats-server-tls \
  --cert=/tmp/nats-cert.pem --key=/tmp/nats-key.pem \
  --dry-run=client -o yaml | kubectl apply -f -

rm /tmp/nats-key.pem /tmp/nats-cert.pem
```

> **Note:** With a self-signed server cert, you must add the self-signed cert to the combined CA bundle in the `nats-spire-ca` Secret (Step 1) or clients will reject the server certificate.

---

## Step 3: Update NATS ConfigMap

Replace the NATS server configuration to enable TLS with `verify_and_map`. This maps client certificate SPIFFE URIs to NATS authorization users.

```bash
kubectl -n tentacular-exoskeleton apply -f - <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: nats-config
  namespace: tentacular-exoskeleton
data:
  nats.conf: |
    listen: 0.0.0.0:4222
    server_name: tentacular-nats

    # TLS with SPIFFE mTLS
    tls {
      cert_file: "/etc/nats/tls/tls.crt"
      key_file: "/etc/nats/tls/tls.key"
      ca_file: "/etc/nats/spire-ca/ca.pem"
      verify_and_map: true
    }

    # Fallback token auth (kept for backward compatibility during migration).
    # Clients presenting a valid token bypass mTLS. Remove this block
    # once all tentacles use SPIFFE SVIDs.
    authorization {
      token: $NATS_TOKEN
    }

    # Per-tentacle authorization managed by the MCP registrar.
    # The ConfigMap nats-tentacular-authz is created/updated by
    # registrar_nats.go when SPIFFE mode is enabled.
    # File may not exist on first boot -- optional include.
    include "/etc/nats/authz/authorization.conf"

    # JetStream
    jetstream {
      store_dir: "/data/jetstream"
      max_mem: 256MB
      max_file: 1Gi
    }

    # Monitoring
    http_port: 8222
EOF
```

**Key points:**
- `verify_and_map: true` tells NATS to extract the SPIFFE URI from the client certificate SAN and use it as the username for authorization matching.
- The `authorization.token` block keeps token auth working for existing clients during migration.
- The `include` directive loads per-tentacle permissions from the registrar-managed ConfigMap (`nats-tentacular-authz`). If the file does not exist (no tentacles registered yet), NATS starts without per-tentacle rules.

---

## Step 4: Update NATS StatefulSet Volumes

Patch the NATS StatefulSet to mount the three new sources: the server TLS cert, the SPIRE CA bundle, and the registrar-managed authorization ConfigMap.

```bash
kubectl -n tentacular-exoskeleton patch statefulset nats --type=json -p='[
  {
    "op": "add",
    "path": "/spec/template/spec/volumes/-",
    "value": {
      "name": "nats-server-tls",
      "secret": {
        "secretName": "nats-server-tls"
      }
    }
  },
  {
    "op": "add",
    "path": "/spec/template/spec/volumes/-",
    "value": {
      "name": "nats-spire-ca",
      "secret": {
        "secretName": "nats-spire-ca"
      }
    }
  },
  {
    "op": "add",
    "path": "/spec/template/spec/volumes/-",
    "value": {
      "name": "nats-tentacular-authz",
      "configMap": {
        "name": "nats-tentacular-authz",
        "optional": true
      }
    }
  },
  {
    "op": "add",
    "path": "/spec/template/spec/containers/0/volumeMounts/-",
    "value": {
      "name": "nats-server-tls",
      "mountPath": "/etc/nats/tls",
      "readOnly": true
    }
  },
  {
    "op": "add",
    "path": "/spec/template/spec/containers/0/volumeMounts/-",
    "value": {
      "name": "nats-spire-ca",
      "mountPath": "/etc/nats/spire-ca",
      "readOnly": true
    }
  },
  {
    "op": "add",
    "path": "/spec/template/spec/containers/0/volumeMounts/-",
    "value": {
      "name": "nats-tentacular-authz",
      "mountPath": "/etc/nats/authz",
      "readOnly": true
    }
  }
]'
```

**Volume summary:**

| Volume | Source | Mount path | Notes |
|--------|--------|-----------|-------|
| `nats-server-tls` | Secret `nats-server-tls` | `/etc/nats/tls` | Server cert + key |
| `nats-spire-ca` | Secret `nats-spire-ca` | `/etc/nats/spire-ca` | Combined CA bundle (SPIRE CA + cert-manager internal CA) |
| `nats-tentacular-authz` | ConfigMap `nats-tentacular-authz` | `/etc/nats/authz` | `optional: true` -- may not exist until first tentacle registers |

---

## Step 5: Apply and Verify

Restart NATS to pick up the new configuration:

```bash
kubectl -n tentacular-exoskeleton rollout restart statefulset nats
```

Wait for the rollout:

```bash
kubectl -n tentacular-exoskeleton rollout status statefulset nats --timeout=120s
```

Verify TLS is active:

```bash
kubectl -n tentacular-exoskeleton exec -it nats-0 -- \
  nats-server --signal reload 2>/dev/null; \
kubectl -n tentacular-exoskeleton logs nats-0 --tail=20 | grep -i tls
```

Verify NATS is listening with TLS from a port-forward:

```bash
kubectl -n tentacular-exoskeleton port-forward svc/nats 4222:4222 &
PF_PID=$!

# Test TLS handshake (expect certificate info, not connection refused)
openssl s_client -connect localhost:4222 -brief < /dev/null 2>&1 | head -5

kill $PF_PID
```

Check NATS monitoring endpoint for server status:

```bash
kubectl -n tentacular-exoskeleton exec nats-0 -- \
  wget -qO- http://localhost:8222/varz 2>/dev/null | python3 -m json.tool | grep -E '"tls|verify'
```

---

## Step 6: Enable SPIFFE Mode on the MCP Server

Set the three environment variables that activate SPIFFE mode in the NATS registrar:

```bash
kubectl -n tentacular-system set env deployment/tentacular-mcp \
  TENTACULAR_NATS_SPIFFE_ENABLED=true \
  TENTACULAR_NATS_AUTHZ_CONFIGMAP=nats-tentacular-authz \
  TENTACULAR_NATS_AUTHZ_NAMESPACE=tentacular-exoskeleton
```

Wait for the MCP server to restart:

```bash
kubectl -n tentacular-system rollout status deployment/tentacular-mcp --timeout=60s
```

Verify the MCP server logs show SPIFFE mode:

```bash
kubectl -n tentacular-system logs deployment/tentacular-mcp --tail=30 | grep -i spiffe
```

Expected log line:
```
nats: SPIFFE mode enabled  authzConfigMap=nats-tentacular-authz authzNamespace=tentacular-exoskeleton
```

---

## Step 7: End-to-End Verification

Deploy a test tentacle and verify the full SPIFFE mTLS chain works.

### 7.1 Deploy a test workflow

```bash
tntc deploy test-spiffe-verify --namespace tent-dev
```

### 7.2 Verify ClusterSPIFFEID was created

```bash
kubectl get clusterspiffeids -l tentacular.io/exoskeleton=true
```

Expected output includes `tentacle-tent-dev-test-spiffe-verify`.

### 7.3 Verify NATS authorization ConfigMap entry

```bash
kubectl -n tentacular-exoskeleton get configmap nats-tentacular-authz -o yaml
```

The `authorization.conf` key should contain an entry like:

```
authorization {
  users = [
    {
      user = "spiffe://tentacular/ns/tent-dev/tentacles/test-spiffe-verify"
      permissions = {
        publish = {
          allow = ["tentacular.tent-dev.test-spiffe-verify.>"]
        }
        subscribe = {
          allow = ["tentacular.tent-dev.test-spiffe-verify.>"]
        }
      }
    }
  ]
}
```

### 7.4 Verify the tentacle pod has an SVID

```bash
kubectl -n tent-dev exec deploy/test-spiffe-verify -- \
  ls /run/spire/sockets/ 2>/dev/null && echo "SPIRE socket present" || echo "No SPIRE socket"
```

### 7.5 Verify NATS connectivity with SVID auth

Check that the tentacle's exoskeleton Secret has `auth_method: spiffe`:

```bash
kubectl -n tent-dev get secret tentacular-exoskeleton-test-spiffe-verify \
  -o jsonpath='{.data.tentacular-nats\.auth_method}' | base64 -d; echo
```

Expected: `spiffe`

### 7.6 Clean up

```bash
tntc undeploy test-spiffe-verify --namespace tent-dev --force
```

---

## Step 8: Rollback Procedure

If SPIFFE mode causes issues, revert to token-only auth.

### 8.1 Disable SPIFFE mode on the MCP server

```bash
kubectl -n tentacular-system set env deployment/tentacular-mcp \
  TENTACULAR_NATS_SPIFFE_ENABLED=false
```

This immediately switches the registrar back to token mode. New deployments get shared tokens. Existing tentacles continue working until redeployed.

### 8.2 Remove TLS from NATS (if needed)

Restore the original NATS ConfigMap without the `tls` block and `include` directive:

```bash
kubectl -n tentacular-exoskeleton apply -f - <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: nats-config
  namespace: tentacular-exoskeleton
data:
  nats.conf: |
    listen: 0.0.0.0:4222
    server_name: tentacular-nats

    authorization {
      token: $NATS_TOKEN
    }

    jetstream {
      store_dir: "/data/jetstream"
      max_mem: 256MB
      max_file: 1Gi
    }

    http_port: 8222
EOF
```

Remove the volume mounts from the StatefulSet:

```bash
kubectl -n tentacular-exoskeleton rollout restart statefulset nats
kubectl -n tentacular-exoskeleton rollout status statefulset nats --timeout=120s
```

### 8.3 Clean up SPIFFE artifacts (optional)

```bash
kubectl -n tentacular-exoskeleton delete secret nats-spire-ca --ignore-not-found
kubectl -n tentacular-exoskeleton delete secret nats-server-tls --ignore-not-found
kubectl -n tentacular-exoskeleton delete configmap nats-tentacular-authz --ignore-not-found
```

---

## Certificate Rotation

### Server certificate (automated)

cert-manager handles NATS server certificate rotation automatically. The `nats-server-tls` Certificate has `renewBefore: 720h` (30 days), so cert-manager renews it before expiry. The new certificate is written to the `nats-server-tls` Secret. NATS picks up the new cert on the next pod restart or rollout.

The internal CA certificate (`tentacular-internal-ca`) has a 10-year validity with 1-year `renewBefore`. cert-manager handles this rotation as well.

### Client SVIDs (automated)

SPIRE handles SVID rotation internally. Engine pods receive updated SVIDs through the SPIRE Workload API (Unix socket) without restarts.

### SPIRE CA bundle sync (manual -- known limitation)

When SPIRE rotates its CA, the `spire-bundle` ConfigMap in `tentacular-system` updates automatically. However, the `nats-spire-ca` Secret in `tentacular-exoskeleton` does **not** update automatically. The combined CA bundle must be refreshed manually:

```bash
# Re-run Step 1 to rebuild the combined bundle with the new SPIRE CA
# Then restart NATS to pick up the updated bundle
kubectl -n tentacular-exoskeleton rollout restart statefulset nats
```

**Future fix:** A sidecar container or CronJob that watches `spire-bundle` and syncs the SPIRE CA cert into the `nats-spire-ca` Secret.

---

## Step 9: Troubleshooting

### Certificate mismatch: NATS rejects client connections

**Symptom:** NATS logs show `tls: failed to verify client certificate` or tentacle pods fail to connect.

**Check:** Verify the SPIRE trust bundle in the `nats-spire-ca` Secret matches the current SPIRE server CA.

```bash
# Current trust bundle from SPIRE
kubectl -n tentacular-system get configmap spire-bundle -o jsonpath='{.data.bundle\.spiffe}' | \
  python3 -c "import json,sys; d=json.load(sys.stdin); print(len(d.get('keys',[])),'keys')"

# Bundle mounted in NATS
kubectl -n tentacular-exoskeleton exec nats-0 -- cat /etc/nats/spire-ca/ca.pem | \
  openssl x509 -noout -fingerprint -sha256
```

If they differ, re-extract the trust bundle (Step 1) and restart NATS.

### Trust bundle rotation

With the recommended `ca_ttl` of `87600h` (10 years, matching Istio's default), SPIRE CA rotation is extremely infrequent. The SPIRE trust bundle in `spire-bundle` ConfigMap auto-updates, but the `nats-spire-ca` Secret (which contains the PEM-encoded combined bundle) needs manual refresh after a CA rotation:

```bash
# Re-run Step 1 to extract the new bundle
# Then restart NATS to pick up the new CA
kubectl -n tentacular-exoskeleton rollout restart statefulset nats
```

With a 10-year CA, this is a rare operational event — not a recurring maintenance burden. If using a shorter `ca_ttl`, consider automating the sync via a CronJob or controller.

### SPIRE agent not running on node

**Symptom:** Tentacle pods have no SVID. The `spire-agent` DaemonSet pod is not running on the node where the tentacle is scheduled.

```bash
kubectl -n tentacular-system get pods -l app=spire-agent -o wide
kubectl get nodes -o wide
```

Verify the SPIRE agent DaemonSet has no scheduling constraints that exclude the node:

```bash
kubectl -n tentacular-system describe daemonset spire-agent | grep -A5 -i tolerations
```

### Authorization ConfigMap not mounted

**Symptom:** NATS starts but does not enforce per-tentacle permissions. Tentacles can publish to any subject.

**Check:** The `nats-tentacular-authz` ConfigMap must exist AND be mounted.

```bash
# Does the ConfigMap exist?
kubectl -n tentacular-exoskeleton get configmap nats-tentacular-authz

# Is it mounted?
kubectl -n tentacular-exoskeleton exec nats-0 -- ls -la /etc/nats/authz/
```

If the ConfigMap exists but NATS does not enforce rules, reload NATS config:

```bash
kubectl -n tentacular-exoskeleton exec nats-0 -- nats-server --signal reload
```

Note: Kubernetes ConfigMap volume mounts update automatically (with a delay of up to 60s by default), but NATS does not hot-reload `include` files. After the MCP registrar updates the ConfigMap, NATS must be signaled or restarted for changes to take effect.

### NATS fails to start after config change

**Symptom:** NATS pod in `CrashLoopBackOff`.

```bash
kubectl -n tentacular-exoskeleton logs nats-0 --previous
```

Common causes:
- **Missing TLS files:** Secret not created or wrong key names. cert-manager uses `tls.crt` and `tls.key`; self-signed may use different names. Verify paths in `nats.conf` match what is mounted.
- **Include file syntax error:** If the `nats-tentacular-authz` ConfigMap has malformed NATS config, NATS fails to parse. Delete the ConfigMap (it has `optional: true`) and restart to isolate.
- **Token variable not set:** The `$NATS_TOKEN` in the config requires the environment variable to be set in the NATS container. Verify the NATS StatefulSet has the token env var configured.

### MCP server cannot manage the authorization ConfigMap

**Symptom:** MCP logs show `forbidden` errors when creating/updating the `nats-tentacular-authz` ConfigMap.

The MCP server's ClusterRole needs permission to manage ConfigMaps in `tentacular-exoskeleton`:

```bash
kubectl get clusterrole tentacular-mcp -o yaml | grep -A10 configmaps
```

If missing, the Helm chart's ClusterRole should already include this. Verify the chart is up to date and re-run `helm upgrade`.
