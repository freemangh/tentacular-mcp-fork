# Exoskeleton Deployment Guide

Deploy the Tentacular exoskeleton -- a bundle of dedicated backing services (Postgres,
NATS, RustFS, Keycloak) plus SPIRE workload identity -- on a Kubernetes cluster. The
MCP server manages the lifecycle of per-tentacle credentials against these services
automatically.

For architecture rationale and design decisions, see
[exoskeleton.md](exoskeleton.md). For NATS SPIFFE mTLS activation details, see
[nats-spiffe-deployment.md](nats-spiffe-deployment.md).

## Prerequisites

| Requirement | Minimum version | Notes |
|-------------|-----------------|-------|
| Kubernetes cluster | 1.27+ | Single cluster; multi-cluster is not supported |
| Helm | 3.12+ | Used for all chart installs |
| cert-manager | 1.12+ | Must be installed cluster-wide before starting |
| kubectl | 1.27+ | Admin kubeconfig for the target cluster |
| openssl | 3.x | For verification commands |
| tntc CLI | latest | For final verification and CLI config |

**Time estimate:** ~45 minutes for a new cluster with all services.

Set your kubeconfig for all commands in this guide:

```bash
export KUBECONFIG=~/dev-secrets/kubeconfigs/<cluster>-admin.kubeconfig
```

---

## 1. Namespace Setup

Tentacular uses four namespace categories:

| Namespace | Purpose |
|-----------|---------|
| `tentacular-system` | MCP server, SPIRE server + agent |
| `tentacular-exoskeleton` | Postgres, NATS, RustFS, Keycloak, cert-manager internal CA |
| `tentacular-support` | esm.sh module proxy, shared dev utilities |
| `tent-*` | Workflow namespaces (created dynamically by `ns_create`) |

Create the namespaces with Pod Security Admission labels:

```bash
kubectl create namespace tentacular-system
kubectl label namespace tentacular-system \
  pod-security.kubernetes.io/enforce=restricted \
  pod-security.kubernetes.io/warn=restricted

kubectl create namespace tentacular-exoskeleton
kubectl label namespace tentacular-exoskeleton \
  pod-security.kubernetes.io/enforce=restricted \
  pod-security.kubernetes.io/warn=restricted

kubectl create namespace tentacular-support
kubectl label namespace tentacular-support \
  pod-security.kubernetes.io/enforce=baseline \
  pod-security.kubernetes.io/warn=restricted
```

> **Note:** `tent-*` namespaces are created by the MCP server's `ns_create` tool with
> appropriate PSA labels. Do not create them manually.

### Verify

```bash
kubectl get namespaces -l pod-security.kubernetes.io/enforce
```

All three namespaces appear with their PSA labels.

---

## 2. SPIRE Deployment

SPIRE provides workload identity (SPIFFE SVIDs) for tentacle pods. The SPIRE server
runs in `tentacular-system`; agents run as a DaemonSet on every node.

### 2.1 Install SPIRE CRDs

```bash
helm repo add spiffe https://spiffe.github.io/helm-charts-hardened/
helm repo update spiffe

helm install spire-crds spiffe/spire-crds \
  --namespace tentacular-system
```

### 2.2 Install SPIRE

```bash
helm install spire spiffe/spire \
  --namespace tentacular-system \
  --set global.spire.trustDomain=tentacular \
  --set global.spire.clusterName=tentacular-cluster \
  --set spire-server.nodeAttestor.k8sPsat.enabled=true \
  --set spire-agent.socketPath=/run/spire/sockets/agent.sock
```

### 2.3 Create a default ClusterSPIFFEID

This ClusterSPIFFEID issues SVIDs to workloads in `tent-*` namespaces:

```bash
kubectl apply -f - <<'EOF'
apiVersion: spire.spiffe.io/v1alpha1
kind: ClusterSPIFFEID
metadata:
  name: tentacular-default
spec:
  className: tentacular-system-spire
  spiffeIDTemplate: "spiffe://tentacular/ns/{{ .PodMeta.Namespace }}/sa/{{ .PodSpec.ServiceAccountName }}"
  namespaceSelector:
    matchExpressions:
      - key: kubernetes.io/metadata.name
        operator: In
        values: []
  podSelector: {}
EOF
```

> **Note:** The MCP server's SPIRE registrar creates per-tentacle ClusterSPIFFEID
> resources at deploy time. This default entry provides a fallback for workloads not
> yet registered.

### Verify

```bash
kubectl -n tentacular-system get pods -l app=spire-server
kubectl -n tentacular-system get pods -l app=spire-agent
kubectl -n tentacular-system exec deploy/spire-server -- \
  /opt/spire/bin/spire-server entry show
```

The SPIRE server pod is Running and the agent DaemonSet has one pod per node.

---

## 3. cert-manager Internal CA

Create an internal CA hierarchy for issuing TLS certificates to exoskeleton services.
This CA is independent of any public CA -- it provides auto-rotating internal TLS.

### 3.1 Create the CA chain

Apply all three resources in a single manifest:

```yaml
# Save as tentacular-internal-ca.yaml
---
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
# 3. CA Issuer (references the CA Secret, scoped to tentacular-exoskeleton)
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: tentacular-internal-ca
  namespace: tentacular-exoskeleton
spec:
  ca:
    secretName: tentacular-internal-ca-tls
```

```bash
kubectl apply -f tentacular-internal-ca.yaml
```

### Why this matters

cert-manager auto-rotates all certificates issued by this CA. The NATS server cert,
for example, renews 30 days before expiry with zero manual intervention.

### Verify

```bash
kubectl -n tentacular-exoskeleton get certificate tentacular-internal-ca
kubectl -n tentacular-exoskeleton get secret tentacular-internal-ca-tls
kubectl -n tentacular-exoskeleton get issuer tentacular-internal-ca
```

The Certificate shows `READY=True` and the Secret exists with `tls.crt` and `tls.key`.

---

## 4. Postgres Deployment

Postgres provides per-tentacle relational state. Each tentacle receives a deterministic
role and schema derived from its `(namespace, workflow)` identity.

### 4.1 Install the Bitnami Helm chart

```bash
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update bitnami

helm install postgres bitnami/postgresql \
  --namespace tentacular-exoskeleton \
  --set auth.postgresPassword="$(openssl rand -hex 16)" \
  --set auth.database=tentacular \
  --set primary.persistence.size=10Gi \
  --set primary.resources.requests.memory=256Mi \
  --set primary.resources.requests.cpu=100m \
  --set primary.podSecurityContext.enabled=true \
  --set primary.containerSecurityContext.enabled=true
```

Record the superuser password:

```bash
kubectl -n tentacular-exoskeleton get secret postgres-postgresql \
  -o jsonpath='{.data.postgres-password}' | base64 -d; echo
```

### 4.2 Create the admin role

Connect to Postgres and create the `tentacular_admin` role. This role has `CREATEROLE`
(required for per-tentacle role provisioning) but not `SUPERUSER`:

```bash
PGPASSWORD=$(kubectl -n tentacular-exoskeleton get secret postgres-postgresql \
  -o jsonpath='{.data.postgres-password}' | base64 -d)

kubectl -n tentacular-exoskeleton exec -it postgres-postgresql-0 -- \
  env PGPASSWORD="$PGPASSWORD" psql -U postgres -d tentacular -c "
    CREATE ROLE tentacular_admin WITH LOGIN PASSWORD '<strong-password>' CREATEROLE;
    GRANT ALL PRIVILEGES ON DATABASE tentacular TO tentacular_admin;
    GRANT ALL ON SCHEMA public TO tentacular_admin;
  "
```

Replace `<strong-password>` with a generated password:

```bash
openssl rand -hex 12
```

> **Warning:** Do not grant `SUPERUSER`. The MCP server needs `CREATEROLE` to create
> per-tentacle roles, but superuser access is unnecessary and violates least-privilege.

### Verify

```bash
kubectl -n tentacular-exoskeleton exec -it postgres-postgresql-0 -- \
  env PGPASSWORD="<admin-password>" psql -U tentacular_admin -d tentacular -c '\conninfo'
```

The connection succeeds and shows `database "tentacular"` and `user "tentacular_admin"`.

---

## 5. NATS Deployment

NATS provides scoped pub/sub messaging for each tentacle. The deployment supports two
auth modes: token auth (simple, default) and SPIFFE mTLS (per-tentacle identity).

### 5.1 Issue the NATS server TLS certificate

Create a cert-manager Certificate for the NATS server:

```bash
kubectl apply -f - <<'EOF'
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
  renewBefore: 720h  # 30 days before expiry
  privateKey:
    algorithm: ECDSA
    size: 256
EOF
```

### 5.2 Create the combined CA trust bundle

NATS needs to trust both the cert-manager CA (for its own server cert chain) and the
SPIRE CA (for client SVIDs). See [nats-spiffe-deployment.md](nats-spiffe-deployment.md)
Step 1 for the full procedure. Summary:

```bash
# Extract SPIRE CA
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
" > scratch/spire-ca.pem

# Extract cert-manager CA
kubectl -n tentacular-exoskeleton get secret tentacular-internal-ca-tls \
  -o jsonpath='{.data.ca\.crt}' | base64 -d > scratch/certmanager-ca.pem

# Combine and create Secret
cat scratch/spire-ca.pem scratch/certmanager-ca.pem > scratch/combined-ca.pem

kubectl -n tentacular-exoskeleton create secret generic nats-spire-ca \
  --from-file=ca.pem=scratch/combined-ca.pem \
  --dry-run=client -o yaml | kubectl apply -f -
```

### 5.3 Create the NATS ConfigMap

```bash
NATS_TOKEN=$(openssl rand -hex 32)

kubectl -n tentacular-exoskeleton apply -f - <<EOF
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

    # Fallback token auth (kept for backward compatibility)
    authorization {
      token: $NATS_TOKEN
    }

    # Per-tentacle authorization managed by MCP registrar
    # File may not exist on first boot -- optional include
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

Record the token for later use:

```bash
echo "$NATS_TOKEN"
```

### 5.4 Create the empty authz ConfigMap

The MCP registrar populates this ConfigMap when tentacles are deployed with SPIFFE
mode enabled. Start with an empty file:

```bash
kubectl -n tentacular-exoskeleton create configmap nats-tentacular-authz \
  --from-literal=authorization.conf="" \
  --dry-run=client -o yaml | kubectl apply -f -
```

### 5.5 Deploy NATS StatefulSet

Deploy NATS with all required volume mounts. If using a Helm chart, configure the
volumes accordingly. For a raw StatefulSet, ensure these volumes are mounted:

| Volume | Source | Mount path |
|--------|--------|------------|
| `nats-config` | ConfigMap `nats-config` | `/etc/nats/config` |
| `nats-server-tls` | Secret `nats-server-tls` | `/etc/nats/tls` (readOnly) |
| `nats-spire-ca` | Secret `nats-spire-ca` | `/etc/nats/spire-ca` (readOnly) |
| `nats-tentacular-authz` | ConfigMap `nats-tentacular-authz` | `/etc/nats/authz` (readOnly, optional) |
| `data` | PVC | `/data` |

The `nats-tentacular-authz` volume must use `optional: true` because the ConfigMap may
not have content until the first tentacle registers.

### 5.6 Config reloader sidecar

Add a config reloader sidecar to the NATS pod so that ConfigMap changes (authz updates
from the MCP registrar) trigger a NATS config reload without restarting the pod:

```yaml
- name: nats-config-reloader
  image: natsio/nats-server-config-reloader:latest
  args:
    - -config=/etc/nats/config/nats.conf
    - -pid=/var/run/nats/nats.pid
  volumeMounts:
    - name: nats-config
      mountPath: /etc/nats/config
    - name: nats-tentacular-authz
      mountPath: /etc/nats/authz
    - name: nats-pid
      mountPath: /var/run/nats
```

### Verify

```bash
kubectl -n tentacular-exoskeleton get pods -l app=nats
kubectl -n tentacular-exoskeleton get certificate nats-server-tls

# Test TLS handshake via port-forward
kubectl -n tentacular-exoskeleton port-forward svc/nats 4222:4222 &
PF_PID=$!
sleep 2
openssl s_client -connect localhost:4222 -brief < /dev/null 2>&1 | head -5
kill $PF_PID
```

The TLS handshake succeeds and shows a valid certificate chain.

---

## 6. RustFS Deployment

RustFS (MinIO-compatible) provides S3-compatible object storage with per-tentacle
prefix isolation.

### 6.1 Deploy RustFS

```bash
kubectl -n tentacular-exoskeleton apply -f - <<'EOF'
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rustfs
  namespace: tentacular-exoskeleton
spec:
  serviceName: rustfs-headless
  replicas: 1
  selector:
    matchLabels:
      app: rustfs
  template:
    metadata:
      labels:
        app: rustfs
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        fsGroup: 1000
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: rustfs
          image: ghcr.io/aspect-build/rustfs:latest
          args: ["server", "/data", "--console-address", ":9001"]
          ports:
            - containerPort: 9000
              name: api
            - containerPort: 9001
              name: console
          env:
            - name: MINIO_ROOT_USER
              valueFrom:
                secretKeyRef:
                  name: rustfs-admin
                  key: access_key
            - name: MINIO_ROOT_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: rustfs-admin
                  key: secret_key
          volumeMounts:
            - name: data
              mountPath: /data
          securityContext:
            allowPrivilegeEscalation: false
            capabilities:
              drop: ["ALL"]
            readOnlyRootFilesystem: false
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: 10Gi
---
apiVersion: v1
kind: Service
metadata:
  name: rustfs-svc
  namespace: tentacular-exoskeleton
spec:
  selector:
    app: rustfs
  ports:
    - name: api
      port: 9000
      targetPort: api
    - name: console
      port: 9001
      targetPort: console
EOF
```

### 6.2 Create admin credentials

```bash
RUSTFS_ACCESS_KEY=$(openssl rand -hex 16)
RUSTFS_SECRET_KEY=$(openssl rand -hex 32)

kubectl -n tentacular-exoskeleton create secret generic rustfs-admin \
  --from-literal=access_key="$RUSTFS_ACCESS_KEY" \
  --from-literal=secret_key="$RUSTFS_SECRET_KEY"

echo "Access key: $RUSTFS_ACCESS_KEY"
echo "Secret key: $RUSTFS_SECRET_KEY"
```

### 6.3 Create the tentacular bucket

Wait for the RustFS pod to become ready, then create the bucket:

```bash
kubectl -n tentacular-exoskeleton exec -it rustfs-0 -- \
  mc alias set local http://localhost:9000 "$RUSTFS_ACCESS_KEY" "$RUSTFS_SECRET_KEY"

kubectl -n tentacular-exoskeleton exec -it rustfs-0 -- \
  mc mb local/tentacular --ignore-existing
```

> **Note:** If `mc` is not available in the RustFS image, use `rc` (the RustFS client)
> or port-forward and run the client locally.

### Verify

```bash
kubectl -n tentacular-exoskeleton get pods -l app=rustfs
kubectl -n tentacular-exoskeleton exec -it rustfs-0 -- \
  mc admin info local 2>/dev/null || \
kubectl -n tentacular-exoskeleton exec -it rustfs-0 -- \
  rc admin info local
```

The pod is Running and the admin info command returns server status.

---

## 7. Keycloak Deployment

Keycloak provides OIDC-based deployer identity and SSO. Google is federated as an
upstream identity provider through Keycloak.

### 7.1 Install the Bitnami Helm chart

Keycloak uses the Postgres instance deployed in step 4 as its database backend:

```bash
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update bitnami

helm install keycloak bitnami/keycloak \
  --namespace tentacular-exoskeleton \
  --set auth.adminUser=admin \
  --set auth.adminPassword="$(openssl rand -base64 24)" \
  --set postgresql.enabled=false \
  --set externalDatabase.host=postgres-postgresql.tentacular-exoskeleton.svc.cluster.local \
  --set externalDatabase.port=5432 \
  --set externalDatabase.database=keycloak \
  --set externalDatabase.user=keycloak \
  --set externalDatabase.password="<keycloak-db-password>" \
  --set httpRelativePath="/" \
  --set proxy=edge \
  --set extraEnvVars[0].name=KC_HOSTNAME \
  --set extraEnvVars[0].value="auth.<cluster>.<domain>" \
  --set extraEnvVars[1].name=KC_PROXY_HEADERS \
  --set extraEnvVars[1].value=xforwarded
```

> **Warning:** Replace `<keycloak-db-password>` with a generated password. Create the
> `keycloak` database and user in Postgres before installing Keycloak:
>
> ```bash
> kubectl -n tentacular-exoskeleton exec -it postgres-postgresql-0 -- \
>   env PGPASSWORD="$PGPASSWORD" psql -U postgres -c "
>     CREATE DATABASE keycloak;
>     CREATE ROLE keycloak WITH LOGIN PASSWORD '<keycloak-db-password>';
>     GRANT ALL PRIVILEGES ON DATABASE keycloak TO keycloak;
>   "
> ```

Record the Keycloak admin password:

```bash
kubectl -n tentacular-exoskeleton get secret keycloak \
  -o jsonpath='{.data.admin-password}' | base64 -d; echo
```

### 7.2 Configure the tentacular realm

Access the Keycloak admin console (via port-forward or ingress) and create:

1. **Realm:** `tentacular`
2. **Client:** `tentacular-mcp`
   - Client type: Confidential
   - Client authentication: On
   - Valid redirect URIs: `https://mcp.<cluster>.<domain>/*`
   - Web origins: `https://mcp.<cluster>.<domain>`
   - Enable "OAuth 2.0 Device Authorization Grant" under Authentication Flow settings
   - Standard flow: Enabled
   - Direct access grants: Disabled
3. **Scopes:** `openid`, `profile`, `email`

Record the client secret from the Credentials tab.

### 7.3 Configure Google SSO Identity Provider

#### Google Cloud Console setup

1. Navigate to **APIs & Services > Credentials** in the Google Cloud Console
2. Create an **OAuth 2.0 Client ID** (Web application type)
3. Add the authorized redirect URI:
   ```
   https://auth.<cluster>.<domain>/realms/tentacular/broker/google/endpoint
   ```
4. Record the Google Client ID and Client Secret

#### Keycloak setup

1. In the `tentacular` realm, navigate to **Identity Providers > Add provider > Google**
2. Set the Client ID and Client Secret from the Google Console
3. Enable **Trust Email** (Google-verified emails skip Keycloak email verification)
4. Set **First Login Flow** to `first broker login`
5. Optional: Set **Hosted Domain** to restrict to a specific Google Workspace domain

### 7.4 Token lifespan configuration

Configure these in the `tentacular` realm settings:

| Setting | Recommended (dev) | Recommended (prod) |
|---------|-------------------|---------------------|
| Access Token Lifespan | 30 minutes | 5 minutes |
| Refresh Token Lifespan | 60 days | 30 days |
| SSO Session Idle | 30 minutes | 15 minutes |

### Verify

```bash
# OIDC discovery endpoint
kubectl -n tentacular-exoskeleton port-forward svc/keycloak 8080:8080 &
PF_PID=$!
sleep 2
curl -s http://localhost:8080/realms/tentacular/.well-known/openid-configuration | \
  python3 -m json.tool | head -10
kill $PF_PID
```

The `issuer` field in the response matches the configured Keycloak URL.

---

## 8. MCP Server Deployment

Two approaches: Helm chart (recommended for new installs) or manual env patching
(existing kustomize deployments).

### 8.1 Helm install (recommended)

Create a values file with all exoskeleton configuration:

```yaml
# Save as tentacular-mcp-values.yaml
image:
  registry: ghcr.io
  repository: randybias/tentacular-mcp
  tag: "latest"

namespace:
  create: true

auth:
  token: "<mcp-bearer-token>"   # openssl rand -hex 32

exoskeleton:
  enabled: true
  cleanupOnUndeploy: false
  postgres:
    host: "postgres-postgresql.tentacular-exoskeleton.svc.cluster.local"
    port: "5432"
    database: "tentacular"
    user: "tentacular_admin"
    password: "<postgres-admin-password>"
    sslMode: "disable"
  nats:
    url: "nats://nats.tentacular-exoskeleton.svc.cluster.local:4222"
    token: "<nats-token>"
  rustfs:
    endpoint: "http://rustfs-svc.tentacular-exoskeleton.svc.cluster.local:9000"
    accessKey: "<rustfs-access-key>"
    secretKey: "<rustfs-secret-key>"
    bucket: "tentacular"
    region: "us-east-1"

exoskeletonAuth:
  enabled: true
  issuerURL: "http://keycloak.tentacular-exoskeleton.svc.cluster.local:8080/realms/tentacular"
  clientID: "tentacular-mcp"
  clientSecret: "<keycloak-client-secret>"
```

Install:

```bash
helm install tentacular-mcp ./charts/tentacular-mcp \
  --namespace tentacular-system \
  --values tentacular-mcp-values.yaml
```

> **Note:** Use the internal Keycloak URL for `issuerURL` when the MCP server and
> Keycloak are in the same cluster. The MCP server validates tokens by calling Keycloak
> directly, not through the ingress.

### 8.2 Manual patching (existing kustomize deployment)

For an existing MCP server deployment, set environment variables from Secrets:

```bash
# Create Secrets for each service
kubectl -n tentacular-system create secret generic exo-postgres \
  --from-literal=host=postgres-postgresql.tentacular-exoskeleton.svc.cluster.local \
  --from-literal=port=5432 \
  --from-literal=database=tentacular \
  --from-literal=user=tentacular_admin \
  --from-literal=password="<postgres-admin-password>"

kubectl -n tentacular-system create secret generic exo-nats \
  --from-literal=url=nats://nats.tentacular-exoskeleton.svc.cluster.local:4222 \
  --from-literal=token="<nats-token>"

kubectl -n tentacular-system create secret generic exo-rustfs \
  --from-literal=endpoint=http://rustfs-svc.tentacular-exoskeleton.svc.cluster.local:9000 \
  --from-literal=access_key="<rustfs-access-key>" \
  --from-literal=secret_key="<rustfs-secret-key>"

kubectl -n tentacular-system create secret generic exo-keycloak \
  --from-literal=issuer-url=http://keycloak.tentacular-exoskeleton.svc.cluster.local:8080/realms/tentacular \
  --from-literal=client-id=tentacular-mcp \
  --from-literal=client-secret="<keycloak-client-secret>"

# Set feature flags
kubectl -n tentacular-system set env deployment/tentacular-mcp \
  TENTACULAR_EXOSKELETON_ENABLED=true \
  TENTACULAR_EXOSKELETON_CLEANUP_ON_UNDEPLOY=false \
  TENTACULAR_EXOSKELETON_AUTH_ENABLED=true \
  TENTACULAR_EXOSKELETON_SPIRE_ENABLED=true \
  TENTACULAR_SPIRE_CLASS_NAME=tentacular-system-spire

# Set Postgres env vars from Secret
kubectl -n tentacular-system set env deployment/tentacular-mcp \
  --from=secret/exo-postgres \
  --prefix=TENTACULAR_POSTGRES_ADMIN_

# Set NATS env vars from Secret
kubectl -n tentacular-system set env deployment/tentacular-mcp \
  TENTACULAR_NATS_URL=nats://nats.tentacular-exoskeleton.svc.cluster.local:4222
kubectl -n tentacular-system set env deployment/tentacular-mcp \
  --from=secret/exo-nats

# Set RustFS env vars
kubectl -n tentacular-system set env deployment/tentacular-mcp \
  TENTACULAR_RUSTFS_ENDPOINT=http://rustfs-svc.tentacular-exoskeleton.svc.cluster.local:9000 \
  TENTACULAR_RUSTFS_BUCKET=tentacular \
  TENTACULAR_RUSTFS_REGION=us-east-1
kubectl -n tentacular-system set env deployment/tentacular-mcp \
  --from=secret/exo-rustfs

# Set Keycloak env vars
kubectl -n tentacular-system set env deployment/tentacular-mcp \
  --from=secret/exo-keycloak
```

### 8.3 SPIRE RBAC for the MCP server

The MCP server's ClusterRole needs permission to manage ClusterSPIFFEID resources and
ConfigMaps in the exoskeleton namespace:

```bash
kubectl apply -f - <<'EOF'
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: tentacular-mcp-spire
rules:
  - apiGroups: ["spire.spiffe.io"]
    resources: ["clusterspiffeids"]
    verbs: ["get", "list", "create", "update", "delete"]
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "list", "create", "update", "patch"]
    # Scoped to tentacular-exoskeleton via the binding or namespace field
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: tentacular-mcp-spire
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: tentacular-mcp-spire
subjects:
  - kind: ServiceAccount
    name: tentacular-mcp
    namespace: tentacular-system
EOF
```

### 8.4 Enable NATS SPIFFE mode (optional)

If using SPIFFE mTLS for NATS (instead of or alongside token auth):

```bash
kubectl -n tentacular-system set env deployment/tentacular-mcp \
  TENTACULAR_NATS_SPIFFE_ENABLED=true \
  TENTACULAR_NATS_AUTHZ_CONFIGMAP=nats-tentacular-authz \
  TENTACULAR_NATS_AUTHZ_NAMESPACE=tentacular-exoskeleton
```

### Verify

```bash
kubectl -n tentacular-system rollout status deployment/tentacular-mcp --timeout=60s
kubectl -n tentacular-system logs deployment/tentacular-mcp --tail=30

# Health check
kubectl -n tentacular-system port-forward svc/tentacular-mcp 8080:8080 &
PF_PID=$!
sleep 2
curl -s http://localhost:8080/healthz
kill $PF_PID
```

The pod is Running, logs show successful registrar initialization for each enabled
service, and `/healthz` returns `200 OK`.

---

## 9. CLI Configuration

Configure the `tntc` CLI to connect to the MCP server with OIDC authentication.

### 9.1 Basic MCP connection

```bash
tntc config set mcp_endpoint https://mcp.<cluster>.<domain>
tntc config set mcp_token_path ~/.tentacular/auth-token
```

### 9.2 OIDC configuration

```bash
tntc config set oidc_issuer https://auth.<cluster>.<domain>/realms/tentacular
tntc config set oidc_client_id tentacular-mcp
tntc config set oidc_client_secret <keycloak-client-secret>
```

### 9.3 Authenticate

```bash
tntc login
```

This starts the OAuth 2.0 Device Authorization Grant flow:

1. The CLI prints a URL and a user code
2. Open the URL in a browser
3. Enter the user code and authenticate via Google SSO
4. The CLI receives tokens and stores them at `~/.tentacular/auth-token`

### Verify

```bash
tntc whoami
```

The output shows the authenticated identity (email and subject from the OIDC token).

---

## 10. Environment Variable Reference

All environment variables read by the MCP server's exoskeleton config loader
(`pkg/exoskeleton/config.go`):

### Feature flags

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `TENTACULAR_EXOSKELETON_ENABLED` | bool | `false` | Master switch for the exoskeleton |
| `TENTACULAR_EXOSKELETON_CLEANUP_ON_UNDEPLOY` | bool | `false` | Delete backing-service data on undeploy |
| `TENTACULAR_EXOSKELETON_AUTH_ENABLED` | bool | `false` | Enable OIDC authentication |
| `TENTACULAR_EXOSKELETON_SPIRE_ENABLED` | bool | `false` | Enable SPIRE identity registration |

### Postgres

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `TENTACULAR_POSTGRES_ADMIN_HOST` | string | (required) | Postgres hostname |
| `TENTACULAR_POSTGRES_ADMIN_PORT` | string | `5432` | Postgres port |
| `TENTACULAR_POSTGRES_ADMIN_DATABASE` | string | `tentacular` | Database name |
| `TENTACULAR_POSTGRES_ADMIN_USER` | string | (required) | Admin username |
| `TENTACULAR_POSTGRES_ADMIN_PASSWORD` | string | (required) | Admin password |
| `TENTACULAR_POSTGRES_SSLMODE` | string | `disable` | SSL mode |

### NATS

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `TENTACULAR_NATS_URL` | string | (required) | NATS server URL |
| `TENTACULAR_NATS_TOKEN` | string | | Auth token (token mode) |
| `TENTACULAR_NATS_SPIFFE_ENABLED` | bool | `false` | Use SPIFFE mTLS instead of token |
| `TENTACULAR_NATS_AUTHZ_CONFIGMAP` | string | `nats-tentacular-authz` | ConfigMap for NATS authz rules |
| `TENTACULAR_NATS_AUTHZ_NAMESPACE` | string | `tentacular-exoskeleton` | Namespace of the authz ConfigMap |

### RustFS

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `TENTACULAR_RUSTFS_ENDPOINT` | string | (required) | RustFS API endpoint |
| `TENTACULAR_RUSTFS_ACCESS_KEY` | string | (required) | Admin access key |
| `TENTACULAR_RUSTFS_SECRET_KEY` | string | (required) | Admin secret key |
| `TENTACULAR_RUSTFS_BUCKET` | string | `tentacular` | Bucket name |
| `TENTACULAR_RUSTFS_REGION` | string | `us-east-1` | Region |

### Keycloak / OIDC

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `TENTACULAR_KEYCLOAK_ISSUER` | string | (required) | OIDC issuer URL |
| `TENTACULAR_KEYCLOAK_CLIENT_ID` | string | (required) | OIDC client ID |
| `TENTACULAR_KEYCLOAK_CLIENT_SECRET` | string | | OIDC client secret |

### SPIRE

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `TENTACULAR_SPIRE_CLASS_NAME` | string | `tentacular-system-spire` | SPIRE class name for ClusterSPIFFEID |

Boolean variables accept `true`, `1`, or `yes` (case-insensitive).

---

## 11. Verification Checklist

Run through this table after completing the deployment to confirm every component is
operational:

| # | Component | Check command | Expected result |
|---|-----------|---------------|-----------------|
| 1 | MCP server | `curl https://mcp.<cluster>.<domain>/healthz` | `200 OK` |
| 2 | Exo status | MCP tool call: `exo_status` | All enabled services show `available` |
| 3 | Postgres | `psql -U tentacular_admin -d tentacular -c '\conninfo'` | Connection succeeds |
| 4 | NATS TLS | `openssl s_client -connect <nats-host>:4222 -brief` | Valid cert chain with correct CN |
| 5 | NATS pub/sub | `nats pub test.hello "hi" && nats sub test.hello` | Message received |
| 6 | RustFS | `mc admin info local` or `rc admin info local` | Server responds with status |
| 7 | RustFS bucket | `mc ls local/tentacular` | Bucket exists (empty is OK) |
| 8 | SPIRE server | `spire-server entry show` | Entries listed (may be empty initially) |
| 9 | SPIRE agent | `kubectl get pods -l app=spire-agent` | One pod Running per node |
| 10 | cert-manager CA | `kubectl get certificate tentacular-internal-ca` | `READY=True` |
| 11 | NATS server cert | `kubectl get certificate nats-server-tls` | `READY=True` |
| 12 | Keycloak OIDC | `curl <issuer>/.well-known/openid-configuration` | Issuer matches config |
| 13 | tntc login | `tntc login` | Device auth flow completes |
| 14 | tntc whoami | `tntc whoami` | Shows authenticated identity |
| 15 | Test deploy | `tntc deploy test-verify --namespace tent-dev` | Workflow deploys, Secret created with exo credentials |

---

## 12. Credential Reference

Every credential required by the exoskeleton, where it comes from, and how it is
stored:

| Credential | Source | Storage | Used by |
|------------|--------|---------|---------|
| MCP bearer token | Generated (`openssl rand -hex 32`) | K8s Secret `tentacular-mcp-auth` in `tentacular-system` | CLI, MCP clients (fallback auth) |
| Postgres superuser password | Helm install | K8s Secret `postgres-postgresql` in `tentacular-exoskeleton` | Initial setup only |
| Postgres admin password | Generated manually | K8s Secret (Helm values or manual) | MCP server (registrar) |
| NATS token | Generated (`openssl rand -hex 32`) | K8s Secret / ConfigMap env var | MCP server (fallback auth) |
| RustFS admin access key | Generated (`openssl rand -hex 16`) | K8s Secret `rustfs-admin` in `tentacular-exoskeleton` | MCP server (registrar) |
| RustFS admin secret key | Generated (`openssl rand -hex 32`) | K8s Secret `rustfs-admin` in `tentacular-exoskeleton` | MCP server (registrar) |
| Keycloak admin password | Helm install | K8s Secret `keycloak` in `tentacular-exoskeleton` | Keycloak admin console |
| Keycloak DB password | Generated manually | K8s Secret (Helm values) | Keycloak pod |
| Keycloak client secret | Keycloak admin console (Credentials tab) | K8s Secret or Helm values | MCP server (OIDC validation) |
| Google OAuth client ID | Google Cloud Console | Keycloak IdP config | Keycloak (Google SSO federation) |
| Google OAuth client secret | Google Cloud Console | Keycloak IdP config | Keycloak (Google SSO federation) |

### Credential format reference

The credentials file follows this format (actual values redacted):

```env
# Postgres
TENTACULAR_POSTGRES_ADMIN_HOST=postgres-postgresql.tentacular-exoskeleton.svc.cluster.local
TENTACULAR_POSTGRES_ADMIN_PORT=5432
TENTACULAR_POSTGRES_ADMIN_USER=tentacular_admin
TENTACULAR_POSTGRES_ADMIN_PASSWORD=<generated>
TENTACULAR_POSTGRES_ADMIN_DATABASE=tentacular

# NATS
TENTACULAR_NATS_URL=nats://nats.tentacular-exoskeleton.svc.cluster.local:4222
TENTACULAR_NATS_TOKEN=<generated>

# RustFS
TENTACULAR_RUSTFS_ENDPOINT=http://rustfs-svc.tentacular-exoskeleton.svc.cluster.local:9000
TENTACULAR_RUSTFS_ACCESS_KEY=<generated>
TENTACULAR_RUSTFS_SECRET_KEY=<generated>
TENTACULAR_RUSTFS_BUCKET=tentacular
TENTACULAR_RUSTFS_REGION=us-east-1

# Keycloak
TENTACULAR_KEYCLOAK_ISSUER=https://auth.<cluster>.<domain>/realms/tentacular
TENTACULAR_KEYCLOAK_CLIENT_ID=tentacular-mcp
TENTACULAR_KEYCLOAK_CLIENT_SECRET=<from-keycloak-admin>
```

---

## 13. Token and Certificate Lifespans

All expiring credentials and their rotation mechanisms:

| Item | Lifespan | Rotation method | Managed by |
|------|----------|-----------------|------------|
| OIDC access token | 30 min (dev) / 5 min (prod) | Auto-refresh via CLI using refresh token | Keycloak |
| OIDC refresh token | 60 days (dev) / 30 days (prod) | Re-login (`tntc login`) | Keycloak |
| NATS server cert | 1 year | Auto-renewed 30 days before expiry | cert-manager |
| Internal CA cert | 10 years | Auto-renewed 1 year before expiry | cert-manager |
| SPIRE server CA | ~12 hours (configurable) | Automatic rotation | SPIRE server |
| Workflow SVIDs | ~1 hour | Automatic via SPIRE Agent Workload API | SPIRE Agent |
| Postgres per-tentacle passwords | Per deploy cycle | On re-registration (credential refresh) | MCP server registrar |
| RustFS per-tentacle secret keys | Per deploy cycle | On re-registration (credential refresh) | MCP server registrar |
| MCP bearer token | No expiry | Manual rotation (regenerate + update Secret) | Operator |
| NATS auth token | No expiry | Manual rotation (regenerate + update ConfigMap) | Operator |

### Combined CA bundle rotation (manual)

The `nats-spire-ca` Secret contains both the SPIRE CA and the cert-manager CA. When
SPIRE rotates its CA, the combined bundle must be refreshed manually:

```bash
# Re-extract SPIRE CA and rebuild the combined bundle (see Section 5.2)
# Then restart NATS to pick up the new bundle
kubectl -n tentacular-exoskeleton rollout restart statefulset nats
```

> **Future improvement:** A sidecar or CronJob that watches `spire-bundle` in
> `tentacular-system` and syncs the SPIRE CA into `nats-spire-ca` automatically.

---

## 14. Troubleshooting

### MCP server CrashLoopBackOff

**Symptom:** The MCP server pod restarts repeatedly after enabling the exoskeleton.

**Diagnose:**

```bash
kubectl -n tentacular-system logs deployment/tentacular-mcp --previous --tail=50
```

**Common causes:**

- Registrar init failure: The MCP server fails to connect to Postgres, NATS, or RustFS
  on startup. Check that service endpoints are reachable and credentials are correct.
- Missing Secret: A referenced `existingSecret` does not exist in the namespace.
- Invalid env var: A boolean flag has an unexpected value (must be `true`, `1`, or
  `yes`).

**Fix:** Correct the environment variables or Secrets and wait for the next rollout.

---

### OIDC issuer mismatch

**Symptom:** Token validation fails with "issuer does not match" or similar errors in
MCP server logs.

**Diagnose:**

```bash
# Check what the MCP server expects
kubectl -n tentacular-system set env deployment/tentacular-mcp --list | grep KEYCLOAK_ISSUER

# Check what Keycloak actually returns
curl -s http://keycloak.tentacular-exoskeleton.svc.cluster.local:8080/realms/tentacular/.well-known/openid-configuration | \
  python3 -c "import json,sys; print(json.load(sys.stdin)['issuer'])"
```

**Common cause:** The MCP server uses the internal URL
(`http://keycloak...svc.cluster.local:8080/realms/tentacular`) but Keycloak returns the
external URL (`https://auth.<cluster>.<domain>/realms/tentacular`) as the issuer because
`KC_HOSTNAME` is set.

**Fix:** Set `TENTACULAR_KEYCLOAK_ISSUER` to match the value Keycloak returns in its
OIDC discovery document. If Keycloak's `KC_HOSTNAME` is set to the external hostname,
use the external URL. Alternatively, use the internal URL and unset `KC_HOSTNAME` (not
recommended for production).

---

### SPIRE ClusterSPIFFEID RBAC errors

**Symptom:** MCP server logs show `forbidden` errors when creating ClusterSPIFFEID
resources.

**Diagnose:**

```bash
kubectl auth can-i create clusterspiffeids \
  --as=system:serviceaccount:tentacular-system:tentacular-mcp
```

**Fix:** Apply the ClusterRole and ClusterRoleBinding from Section 8.3.

---

### NATS TLS handshake failures

**Symptom:** Clients cannot connect to NATS. Errors include `tls: failed to verify
certificate` or `x509: certificate signed by unknown authority`.

**Diagnose:**

```bash
# Check the NATS server certificate
kubectl -n tentacular-exoskeleton get certificate nats-server-tls -o yaml | \
  grep -A5 "status:"

# Verify the mounted CA bundle matches what NATS trusts
kubectl -n tentacular-exoskeleton exec nats-0 -- \
  cat /etc/nats/spire-ca/ca.pem | openssl x509 -noout -subject -issuer

# Test TLS handshake directly
kubectl -n tentacular-exoskeleton port-forward svc/nats 4222:4222 &
PF_PID=$!
sleep 2
openssl s_client -connect localhost:4222 -CAfile scratch/combined-ca.pem -brief < /dev/null
kill $PF_PID
```

**Common causes:**

- The `nats-server-tls` Secret was not created (cert-manager Certificate not Ready)
- The combined CA bundle is stale (SPIRE rotated its CA)
- DNS SANs in the certificate do not match the service hostname

**Fix:** Check `kubectl get certificate nats-server-tls` for readiness. Rebuild the
combined CA bundle if SPIRE CA has rotated (see Section 13).

---

### RustFS admin API errors (500)

**Symptom:** The MCP server registrar logs `500 Internal Server Error` when calling
RustFS admin APIs.

**Diagnose:**

```bash
kubectl -n tentacular-exoskeleton logs rustfs-0 --tail=30
```

**Common cause:** RustFS returns 500 for operations on entities that do not exist (for
example, deleting a non-existent user). This is a known API behavior, not a server
error.

**Fix:** The MCP registrar handles 500 responses for idempotent operations. If the
error persists on create operations, verify the RustFS admin credentials are correct and
the bucket exists.

---

### Postgres CREATEROLE permissions denied

**Symptom:** The MCP server cannot create per-tentacle roles. Logs show `permission
denied` for `CREATE ROLE`.

**Diagnose:**

```bash
kubectl -n tentacular-exoskeleton exec -it postgres-postgresql-0 -- \
  env PGPASSWORD="<admin-password>" psql -U tentacular_admin -d tentacular -c \
  "SELECT rolname, rolcreaterole FROM pg_roles WHERE rolname = 'tentacular_admin';"
```

**Fix:** The `rolcreaterole` column must be `t`. If not, grant it:

```bash
kubectl -n tentacular-exoskeleton exec -it postgres-postgresql-0 -- \
  env PGPASSWORD="<superuser-password>" psql -U postgres -c \
  "ALTER ROLE tentacular_admin CREATEROLE;"
```

---

### NATS authorization ConfigMap not taking effect

**Symptom:** Per-tentacle NATS permissions are not enforced after deployment.

**Diagnose:**

```bash
# Check the ConfigMap content
kubectl -n tentacular-exoskeleton get configmap nats-tentacular-authz -o yaml

# Check if the ConfigMap is mounted
kubectl -n tentacular-exoskeleton exec nats-0 -- ls -la /etc/nats/authz/

# Check if NATS loaded the config
kubectl -n tentacular-exoskeleton exec nats-0 -- \
  wget -qO- http://localhost:8222/varz 2>/dev/null | python3 -m json.tool | grep config
```

**Fix:** If the config reloader sidecar is not running, manually reload NATS:

```bash
kubectl -n tentacular-exoskeleton exec nats-0 -- nats-server --signal reload
```

Or restart the StatefulSet:

```bash
kubectl -n tentacular-exoskeleton rollout restart statefulset nats
```

---

## 15. Upgrading

### MCP server upgrade

Build and push a new image, then upgrade:

```bash
# From the tentacular-mcp repo
make dev-release

# Helm upgrade
helm upgrade tentacular-mcp ./charts/tentacular-mcp \
  --namespace tentacular-system \
  --values tentacular-mcp-values.yaml \
  --set image.tag=<new-tag>
```

Or for kustomize deployments:

```bash
kubectl -n tentacular-system set image deployment/tentacular-mcp \
  tentacular-mcp=ghcr.io/randybias/tentacular-mcp:<new-tag>
```

### Rolling update behavior

- The Deployment uses `RollingUpdate` strategy by default
- Existing tentacle registrations are preserved across restarts
- The new pod re-validates connections to all enabled exoskeleton services on startup
- If a backing service is temporarily unreachable, the registrar retries

### Credential rotation during upgrade

- Rotating Postgres/NATS/RustFS admin credentials requires updating the corresponding
  Secrets or Helm values and restarting the MCP server
- Existing per-tentacle credentials remain valid until the next re-registration
- To force credential rotation for all tentacles, undeploy and redeploy each workflow

### Adding or removing exoskeleton services

To enable a previously disabled service:

1. Deploy the service (follow the relevant section above)
2. Add the service credentials to the MCP server config
3. Set the feature flag (e.g., `TENTACULAR_EXOSKELETON_SPIRE_ENABLED=true`)
4. Restart the MCP server
5. Redeploy affected workflows to trigger registration against the new service

To disable a service:

1. Unset the feature flag
2. Restart the MCP server
3. Existing tentacles retain their credentials but the registrar stops managing that
   service

> **Warning:** Enabling `cleanupOnUndeploy` and then undeploying a workflow deletes
> all data in that workflow's backing-service scope (Postgres schema, RustFS prefix,
> NATS authz entry). This is irreversible.
