package exoskeleton

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func newFakeDynamic() *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			clusterSPIFFEIDGVR: "ClusterSPIFFEIDList",
		},
	)
}

func TestSPIREName(t *testing.T) {
	tests := []struct {
		ns, wf, want string
	}{
		{"my-ns", "my-wf", "tentacle-my-ns-my-wf"},
		{"NS-Upper", "wf_under", "tentacle-ns-upper-wf-under"},
		{"a", "b", "tentacle-a-b"},
	}
	for _, tt := range tests {
		got := spireName(tt.ns, tt.wf)
		if got != tt.want {
			t.Errorf("spireName(%q, %q) = %q, want %q", tt.ns, tt.wf, got, tt.want)
		}
	}
}

func TestSanitizeK8sName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello-world", "hello-world"},
		{"Hello_World", "hello-world"},
		{"a.b.c", "a-b-c"},
		{"-trim-", "trim"},
	}
	for _, tt := range tests {
		got := sanitizeK8sName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeK8sName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSPIRERegisterCreatesResource(t *testing.T) {
	fake := newFakeDynamic()
	reg := NewSPIRERegistrar(fake, "tentacular-system-spire")

	id, err := CompileIdentity("test-ns", "my-workflow")
	if err != nil {
		t.Fatalf("CompileIdentity: %v", err)
	}

	ctx := context.Background()
	if err := reg.Register(ctx, id, "test-ns"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Verify the resource was created.
	name := spireName("test-ns", "my-workflow")
	obj, err := fake.Resource(clusterSPIFFEIDGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get ClusterSPIFFEID: %v", err)
	}

	// Check metadata.
	if obj.GetName() != name {
		t.Errorf("name = %q, want %q", obj.GetName(), name)
	}

	labels := obj.GetLabels()
	if labels["tentacular.io/release"] != "my-workflow" {
		t.Errorf("release label = %q, want %q", labels["tentacular.io/release"], "my-workflow")
	}
	if labels["tentacular.io/exoskeleton"] != "true" {
		t.Errorf("exoskeleton label = %q, want %q", labels["tentacular.io/exoskeleton"], "true")
	}

	// Check spec fields.
	className, _, _ := unstructured.NestedString(obj.Object, "spec", "className")
	if className != "tentacular-system-spire" {
		t.Errorf("className = %q, want %q", className, "tentacular-system-spire")
	}

	hint, _, _ := unstructured.NestedString(obj.Object, "spec", "hint")
	if hint != "my-workflow" {
		t.Errorf("hint = %q, want %q", hint, "my-workflow")
	}

	// Check namespace selector.
	nsLabel, _, _ := unstructured.NestedString(obj.Object, "spec", "namespaceSelector", "matchLabels", "kubernetes.io/metadata.name")
	if nsLabel != "test-ns" {
		t.Errorf("namespace selector = %q, want %q", nsLabel, "test-ns")
	}

	// Check pod selector.
	podLabel, _, _ := unstructured.NestedString(obj.Object, "spec", "podSelector", "matchLabels", "tentacular.io/release")
	if podLabel != "my-workflow" {
		t.Errorf("pod selector = %q, want %q", podLabel, "my-workflow")
	}
}

func TestSPIRERegisterUpdatesExisting(t *testing.T) {
	fake := newFakeDynamic()
	reg := NewSPIRERegistrar(fake, "tentacular-system-spire")

	id, err := CompileIdentity("test-ns", "my-workflow")
	if err != nil {
		t.Fatalf("CompileIdentity: %v", err)
	}

	ctx := context.Background()

	// Register once.
	if err := reg.Register(ctx, id, "test-ns"); err != nil {
		t.Fatalf("Register (first): %v", err)
	}

	// Register again (should update, not error).
	if err := reg.Register(ctx, id, "test-ns"); err != nil {
		t.Fatalf("Register (second): %v", err)
	}

	// Verify still only one resource.
	list, err := fake.Resource(clusterSPIFFEIDGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list.Items) != 1 {
		t.Errorf("expected 1 ClusterSPIFFEID, got %d", len(list.Items))
	}
}

func TestSPIREUnregisterDeletesResource(t *testing.T) {
	fake := newFakeDynamic()
	reg := NewSPIRERegistrar(fake, "tentacular-system-spire")

	id, err := CompileIdentity("test-ns", "my-workflow")
	if err != nil {
		t.Fatalf("CompileIdentity: %v", err)
	}

	ctx := context.Background()

	// Register first.
	if err := reg.Register(ctx, id, "test-ns"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Unregister.
	if err := reg.Unregister(ctx, id, "test-ns"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}

	// Verify the resource is gone.
	name := spireName("test-ns", "my-workflow")
	_, err = fake.Resource(clusterSPIFFEIDGVR).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		t.Error("expected NotFound error after Unregister, got nil")
	}
}

func TestSPIREUnregisterNonExistent(t *testing.T) {
	fake := newFakeDynamic()
	reg := NewSPIRERegistrar(fake, "tentacular-system-spire")

	id, err := CompileIdentity("test-ns", "my-workflow")
	if err != nil {
		t.Fatalf("CompileIdentity: %v", err)
	}

	ctx := context.Background()

	// Unregister something that doesn't exist should error.
	err = reg.Unregister(ctx, id, "test-ns")
	if err == nil {
		t.Error("expected error when unregistering non-existent resource")
	}
}

func TestControllerWithSPIREDisabled(t *testing.T) {
	cfg := &Config{
		Enabled: true,
		SPIRE: SPIREConfig{
			Enabled: false,
		},
	}
	ctrl, err := NewController(cfg, nil)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	defer ctrl.Close()

	if ctrl.SPIREAvailable() {
		t.Error("expected SPIREAvailable()=false when SPIRE disabled")
	}
}

func TestSPIREDefaultClassName(t *testing.T) {
	fake := newFakeDynamic()
	reg := NewSPIRERegistrar(fake, "")

	if reg.className != "tentacular-system-spire" {
		t.Errorf("default className = %q, want %q", reg.className, "tentacular-system-spire")
	}
}

func TestSPIRECustomClassName(t *testing.T) {
	fake := newFakeDynamic()
	reg := NewSPIRERegistrar(fake, "custom-spire-class")

	if reg.className != "custom-spire-class" {
		t.Errorf("className = %q, want %q", reg.className, "custom-spire-class")
	}

	id, _ := CompileIdentity("ns", "wf")
	ctx := context.Background()
	if err := reg.Register(ctx, id, "ns"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	obj, err := fake.Resource(clusterSPIFFEIDGVR).Get(ctx, spireName("ns", "wf"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	className, _, _ := unstructured.NestedString(obj.Object, "spec", "className")
	if className != "custom-spire-class" {
		t.Errorf("className = %q, want %q", className, "custom-spire-class")
	}
}
