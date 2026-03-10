package exoskeleton

import (
	"testing"
)

func TestDetectExoDeps(t *testing.T) {
	tests := []struct {
		name      string
		manifests []map[string]interface{}
		wantDeps  int
	}{
		{
			name: "no configmap",
			manifests: []map[string]interface{}{
				{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]interface{}{"name": "test"}},
			},
			wantDeps: 0,
		},
		{
			name: "configmap without workflow.yaml",
			manifests: []map[string]interface{}{
				{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata":   map[string]interface{}{"name": "test"},
					"data":       map[string]interface{}{"other.yaml": "foo: bar"},
				},
			},
			wantDeps: 0,
		},
		{
			name: "workflow with tentacular deps",
			manifests: []map[string]interface{}{
				{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata":   map[string]interface{}{"name": "test"},
					"data": map[string]interface{}{
						"workflow.yaml": `
contract:
  dependencies:
    tentacular-postgres:
      protocol: postgresql
    tentacular-nats:
      protocol: nats
    some-other-dep:
      protocol: http
`,
					},
				},
			},
			wantDeps: 2,
		},
		{
			name: "workflow without tentacular deps",
			manifests: []map[string]interface{}{
				{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata":   map[string]interface{}{"name": "test"},
					"data": map[string]interface{}{
						"workflow.yaml": `
contract:
  dependencies:
    redis:
      protocol: redis
`,
					},
				},
			},
			wantDeps: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := detectExoDeps(tt.manifests)
			if len(deps) != tt.wantDeps {
				t.Errorf("got %d deps, want %d: %v", len(deps), tt.wantDeps, deps)
			}
		})
	}
}

func TestControllerDisabled(t *testing.T) {
	cfg := &Config{Enabled: false}
	ctrl, err := NewController(cfg, nil)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	defer ctrl.Close()

	if ctrl.Enabled() {
		t.Error("expected Enabled()=false")
	}

	// ProcessManifests should pass through unchanged
	manifests := []map[string]interface{}{
		{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]interface{}{"name": "test"}},
	}
	result, err := ctrl.ProcessManifests(nil, "ns", "wf", manifests)
	if err != nil {
		t.Fatalf("ProcessManifests: %v", err)
	}
	if len(result) != len(manifests) {
		t.Errorf("got %d manifests, want %d", len(result), len(manifests))
	}

	// Cleanup should be a no-op
	if err := ctrl.Cleanup(nil, "ns", "wf"); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
}
