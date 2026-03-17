package exoskeleton

import (
	"context"
	"strings"
	"testing"
)

// TestDetectExoDeps_DisabledServiceWithTentacularDep verifies that
// detectExoDeps finds the dependency even when the service is not
// configured -- the controller will then reject it at ProcessManifests.
func TestDetectExoDeps_DisabledServiceWithTentacularDep(t *testing.T) {
	manifests := []map[string]any{
		{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "test"},
			"data": map[string]any{
				"workflow.yaml": `
contract:
  dependencies:
    tentacular-postgres:
      protocol: postgresql
`,
			},
		},
	}
	deps := detectExoDeps(manifests)
	if len(deps) != 1 || deps[0] != "tentacular-postgres" {
		t.Errorf("expected [tentacular-postgres], got %v", deps)
	}
}

// TestControllerEnabled_NoTentacularDeps ensures that an enabled
// controller with manifests containing no tentacular-* deps passes
// them through unchanged.
func TestControllerEnabled_NoTentacularDeps(t *testing.T) {
	cfg := &Config{Enabled: true}
	ctrl := &Controller{cfg: cfg}
	defer ctrl.Close()

	manifests := []map[string]any{
		{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "test"},
			"data": map[string]any{
				"workflow.yaml": `
contract:
  dependencies:
    redis:
      protocol: redis
`,
			},
		},
	}
	result, err := ctrl.ProcessManifests(context.TODO(), "ns", "wf", manifests)
	if err != nil {
		t.Fatalf("ProcessManifests: %v", err)
	}
	if len(result) != len(manifests) {
		t.Errorf("expected %d manifests, got %d", len(manifests), len(result))
	}
}

// TestControllerEnabled_DisabledPostgres_RejectsPostgresDep confirms
// that when the controller is enabled but postgres is not configured,
// a workflow declaring tentacular-postgres gets an error.
func TestControllerEnabled_DisabledPostgres_RejectsPostgresDep(t *testing.T) {
	cfg := &Config{Enabled: true}
	ctrl := &Controller{cfg: cfg}
	defer ctrl.Close()

	manifests := []map[string]any{
		{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "test"},
			"data": map[string]any{
				"workflow.yaml": `
contract:
  dependencies:
    tentacular-postgres:
      protocol: postgresql
`,
			},
		},
	}
	_, err := ctrl.ProcessManifests(context.TODO(), "ns", "wf", manifests)
	if err == nil {
		t.Fatal("expected error for disabled postgres with tentacular-postgres dep")
	}
	if !strings.Contains(err.Error(), "not enabled") {
		t.Errorf("expected 'not enabled' in error, got: %v", err)
	}
}

// TestControllerEnabled_DisabledNATS_RejectsNATSDep confirms
// the same gating for NATS.
func TestControllerEnabled_DisabledNATS_RejectsNATSDep(t *testing.T) {
	cfg := &Config{Enabled: true}
	ctrl := &Controller{cfg: cfg}
	defer ctrl.Close()

	manifests := []map[string]any{
		{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "test"},
			"data": map[string]any{
				"workflow.yaml": `
contract:
  dependencies:
    tentacular-nats:
      protocol: nats
`,
			},
		},
	}
	_, err := ctrl.ProcessManifests(context.TODO(), "ns", "wf", manifests)
	if err == nil {
		t.Fatal("expected error for disabled nats with tentacular-nats dep")
	}
	if !strings.Contains(err.Error(), "not enabled") {
		t.Errorf("expected 'not enabled' in error, got: %v", err)
	}
}

// TestControllerEnabled_DisabledRustFS_RejectsRustFSDep confirms
// the same gating for RustFS.
func TestControllerEnabled_DisabledRustFS_RejectsRustFSDep(t *testing.T) {
	cfg := &Config{Enabled: true}
	ctrl := &Controller{cfg: cfg}
	defer ctrl.Close()

	manifests := []map[string]any{
		{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "test"},
			"data": map[string]any{
				"workflow.yaml": `
contract:
  dependencies:
    tentacular-rustfs:
      protocol: s3
`,
			},
		},
	}
	_, err := ctrl.ProcessManifests(context.TODO(), "ns", "wf", manifests)
	if err == nil {
		t.Fatal("expected error for disabled rustfs with tentacular-rustfs dep")
	}
	if !strings.Contains(err.Error(), "not enabled") {
		t.Errorf("expected 'not enabled' in error, got: %v", err)
	}
}

// TestControllerDisabled_NoConfigMapPassthrough ensures manifests
// without any ConfigMap pass through an enabled controller.
func TestControllerEnabled_NoConfigMapPassthrough(t *testing.T) {
	cfg := &Config{Enabled: true}
	ctrl := &Controller{cfg: cfg}
	defer ctrl.Close()

	manifests := []map[string]any{
		{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "test"}},
		{"apiVersion": "v1", "kind": "Service", "metadata": map[string]any{"name": "test-svc"}},
	}
	result, err := ctrl.ProcessManifests(context.TODO(), "ns", "wf", manifests)
	if err != nil {
		t.Fatalf("ProcessManifests: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 manifests unchanged, got %d", len(result))
	}
}

// TestCleanup_DisabledIsNoop verifies cleanup is a no-op when disabled.
func TestCleanup_DisabledIsNoop(t *testing.T) {
	cfg := &Config{Enabled: true, CleanupOnUndeploy: false}
	ctrl := &Controller{cfg: cfg}
	if err := ctrl.Cleanup(context.TODO(), "ns", "wf"); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
}
