# tentacular-mcp Helm Chart

Helm chart for deploying [tentacular-mcp](https://github.com/randybias/tentacular-mcp) — an in-cluster MCP server for Kubernetes namespace lifecycle, credential management, workflow introspection, and cluster operations.

## Prerequisites

- Kubernetes 1.28+
- Helm 3.x

## Install

Generate an auth token and install the chart:

```bash
helm install tentacular-mcp charts/tentacular-mcp/ \
  --namespace tentacular-system \
  --create-namespace \
  --set auth.token=$(openssl rand -hex 32)
```

## Uninstall

```bash
helm uninstall tentacular-mcp --namespace tentacular-system
```

> **Note:** If the chart created the namespace (`namespace.create: true`), uninstalling the release will also delete the namespace and everything in it.

## Examples

### Use an existing Secret for the auth token

If you manage the auth Secret externally (e.g., via Sealed Secrets or External Secrets Operator):

```bash
# Create the secret beforehand
kubectl create secret generic my-mcp-token \
  --namespace tentacular-system \
  --from-literal=token=$(openssl rand -hex 32)

# Install referencing the existing secret
helm install tentacular-mcp charts/tentacular-mcp/ \
  --namespace tentacular-system \
  --create-namespace \
  --set auth.existingSecret=my-mcp-token
```

### Expose via NodePort

```bash
helm install tentacular-mcp charts/tentacular-mcp/ \
  --namespace tentacular-system \
  --create-namespace \
  --set auth.token=$(openssl rand -hex 32) \
  --set service.type=NodePort \
  --set service.nodePort=30080
```

### Custom image and resources

```bash
helm install tentacular-mcp charts/tentacular-mcp/ \
  --namespace tentacular-system \
  --create-namespace \
  --set auth.token=$(openssl rand -hex 32) \
  --set image.registry=my-registry.example.com \
  --set image.repository=my-org/tentacular-mcp \
  --set image.tag=v1.2.3 \
  --set resources.requests.cpu=200m \
  --set resources.requests.memory=128Mi \
  --set resources.limits.cpu=1 \
  --set resources.limits.memory=512Mi
```

### Install with a values file

Create a `my-values.yaml`:

```yaml
auth:
  token: "your-generated-token-here"

replicaCount: 2

service:
  type: NodePort
  nodePort: 30080

resources:
  requests:
    cpu: 200m
    memory: 128Mi
  limits:
    cpu: 1
    memory: 512Mi

nodeSelector:
  kubernetes.io/os: linux

tolerations:
  - key: "dedicated"
    operator: "Equal"
    value: "platform"
    effect: "NoSchedule"
```

```bash
helm install tentacular-mcp charts/tentacular-mcp/ \
  --namespace tentacular-system \
  --create-namespace \
  -f my-values.yaml
```

### Private registry with image pull secrets

```bash
kubectl create secret docker-registry regcred \
  --namespace tentacular-system \
  --docker-server=my-registry.example.com \
  --docker-username=user \
  --docker-password=pass

helm install tentacular-mcp charts/tentacular-mcp/ \
  --namespace tentacular-system \
  --create-namespace \
  --set auth.token=$(openssl rand -hex 32) \
  --set image.registry=my-registry.example.com \
  --set image.repository=my-org/tentacular-mcp \
  --set imagePullSecrets[0].name=regcred
```

## Connecting

### From outside the cluster (port-forward)

```bash
kubectl port-forward -n tentacular-system svc/tentacular-tentacular-mcp 8080:8080 &
curl http://localhost:8080/healthz
```

### From inside the cluster

The MCP server is reachable at:

```
http://<release>-tentacular-mcp.<namespace>.svc:8080
```

For example, with the default install:

```
http://tentacular-tentacular-mcp.tentacular-system.svc:8080
```

### With the tentacular CLI

```bash
# Via port-forward
tntc cluster check --mcp-url http://localhost:8080

# Via in-cluster DNS
tntc cluster check --mcp-url http://tentacular-tentacular-mcp.tentacular-system.svc:8080
```

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `replicaCount` | int | `1` | Number of pod replicas |
| `image.registry` | string | `ghcr.io` | Container image registry |
| `image.repository` | string | `randybias/tentacular-mcp` | Container image repository |
| `image.tag` | string | `""` (appVersion) | Container image tag |
| `image.pullPolicy` | string | `IfNotPresent` | Image pull policy |
| `imagePullSecrets` | list | `[]` | Image pull secrets for private registries |
| `nameOverride` | string | `""` | Override the release name |
| `fullnameOverride` | string | `""` | Override the full release name |
| `namespace.create` | bool | `true` | Create the namespace resource |
| `serviceAccount.create` | bool | `true` | Create the ServiceAccount |
| `serviceAccount.annotations` | object | `{}` | Annotations for the ServiceAccount |
| `rbac.create` | bool | `true` | Create ClusterRole and ClusterRoleBinding |
| `auth.existingSecret` | string | `""` | Name of an existing Secret (key must be `token`) |
| `auth.token` | string | `""` | Auth token value (used when `existingSecret` is empty) |
| `service.type` | string | `ClusterIP` | Service type (ClusterIP, NodePort, LoadBalancer) |
| `service.port` | int | `8080` | Service port |
| `service.nodePort` | string | `""` | NodePort (only when type is NodePort) |
| `service.annotations` | object | `{}` | Service annotations |
| `deployment.strategy` | object | `{type: RollingUpdate}` | Deployment strategy |
| `deployment.annotations` | object | `{}` | Deployment annotations |
| `deployment.podAnnotations` | object | `{}` | Pod template annotations |
| `resources.requests.cpu` | string | `100m` | CPU request |
| `resources.requests.memory` | string | `64Mi` | Memory request |
| `resources.limits.cpu` | string | `500m` | CPU limit |
| `resources.limits.memory` | string | `256Mi` | Memory limit |
| `podSecurityContext` | object | See values.yaml | Pod-level security context |
| `securityContext` | object | See values.yaml | Container-level security context |
| `livenessProbe` | object | See values.yaml | Liveness probe configuration |
| `readinessProbe` | object | See values.yaml | Readiness probe configuration |
| `nodeSelector` | object | `{}` | Node selector for pod scheduling |
| `tolerations` | list | `[]` | Tolerations for pod scheduling |
| `affinity` | object | `{}` | Affinity rules for pod scheduling |
