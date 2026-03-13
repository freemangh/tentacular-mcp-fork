package exoskeleton

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// clusterSPIFFEIDGVR is the GroupVersionResource for SPIRE ClusterSPIFFEID CRDs.
var clusterSPIFFEIDGVR = schema.GroupVersionResource{
	Group:    "spire.spiffe.io",
	Version:  "v1alpha1",
	Resource: "clusterspiffeids",
}

// SPIRERegistrar creates and deletes ClusterSPIFFEID resources for
// tentacle workflows. The SPIRE controller manager automatically
// provisions X.509 SVIDs for pods matching the selectors.
type SPIRERegistrar struct {
	dynamic   dynamic.Interface
	className string
}

// NewSPIRERegistrar creates a SPIRE registrar using the dynamic client
// from the provided k8s.Client. The className specifies the SPIRE class
// to use in ClusterSPIFFEID specs.
func NewSPIRERegistrar(dyn dynamic.Interface, className string) *SPIRERegistrar {
	if className == "" {
		className = "tentacular-system-spire"
	}
	return &SPIRERegistrar{
		dynamic:   dyn,
		className: className,
	}
}

// Register creates (or updates) a ClusterSPIFFEID for the given workflow
// in the given namespace. The ClusterSPIFFEID selects pods with the
// tentacular.io/release label matching the workflow name, in the
// specified namespace.
func (r *SPIRERegistrar) Register(ctx context.Context, id Identity, namespace string) error {
	name := spireName(namespace, id.Workflow)

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "spire.spiffe.io/v1alpha1",
			"kind":       "ClusterSPIFFEID",
			"metadata": map[string]interface{}{
				"name": name,
				"labels": map[string]interface{}{
					"tentacular.io/release":     id.Workflow,
					"tentacular.io/exoskeleton": "true",
				},
			},
			"spec": map[string]interface{}{
				"className": r.className,
				"hint":      id.Workflow,
				"spiffeIDTemplate": fmt.Sprintf(
					"spiffe://{{ .TrustDomain }}/ns/{{ .PodMeta.Namespace }}/tentacles/%s",
					"{{ index .PodMeta.Labels \"tentacular.io/release\" }}",
				),
				"namespaceSelector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"kubernetes.io/metadata.name": namespace,
					},
				},
				"podSelector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"tentacular.io/release": id.Workflow,
					},
				},
			},
		},
	}

	// Try to get existing resource for update.
	existing, err := r.dynamic.Resource(clusterSPIFFEIDGVR).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		// Update: preserve resourceVersion.
		obj.SetResourceVersion(existing.GetResourceVersion())
		_, err = r.dynamic.Resource(clusterSPIFFEIDGVR).Update(ctx, obj, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("update ClusterSPIFFEID %s: %w", name, err)
		}
		slog.Info("spire: updated ClusterSPIFFEID", "name", name, "namespace", namespace, "workflow", id.Workflow)
		return nil
	}

	// Create new resource.
	_, err = r.dynamic.Resource(clusterSPIFFEIDGVR).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create ClusterSPIFFEID %s: %w", name, err)
	}

	slog.Info("spire: created ClusterSPIFFEID", "name", name, "namespace", namespace, "workflow", id.Workflow)
	return nil
}

// Unregister deletes the ClusterSPIFFEID for the given workflow.
func (r *SPIRERegistrar) Unregister(ctx context.Context, id Identity, namespace string) error {
	name := spireName(namespace, id.Workflow)

	err := r.dynamic.Resource(clusterSPIFFEIDGVR).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete ClusterSPIFFEID %s: %w", name, err)
	}

	slog.Info("spire: deleted ClusterSPIFFEID", "name", name, "namespace", namespace, "workflow", id.Workflow)
	return nil
}

// Close is a no-op for the SPIRE registrar.
func (r *SPIRERegistrar) Close() {}

// spireName generates the sanitized ClusterSPIFFEID resource name:
// tentacle-<namespace>-<workflow>. Uses the same sanitization as
// Postgres identifiers to ensure lowercase alphanumeric + hyphens.
func spireName(namespace, workflow string) string {
	raw := fmt.Sprintf("tentacle-%s-%s", sanitizeK8sName(namespace), sanitizeK8sName(workflow))
	// K8s resource names max 253 chars, but keep it reasonable.
	if len(raw) > 253 {
		raw = raw[:253]
	}
	return raw
}

// sanitizeK8sName lowercases and replaces non-alphanumeric chars
// (except hyphens) with hyphens, then trims leading/trailing hyphens.
func sanitizeK8sName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
