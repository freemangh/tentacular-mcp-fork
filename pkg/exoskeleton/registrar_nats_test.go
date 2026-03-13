package exoskeleton

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNATSCredsMapping(t *testing.T) {
	id, err := CompileIdentity("tent-dev", "hn-digest")
	if err != nil {
		t.Fatalf("CompileIdentity returned error: %v", err)
	}

	if id.NATSUser != "tentacle.tent-dev.hn-digest" {
		t.Errorf("NATSUser = %q, want tentacle.tent-dev.hn-digest", id.NATSUser)
	}
	if id.NATSPrefix != "tentacular.tent-dev.hn-digest.>" {
		t.Errorf("NATSPrefix = %q, want tentacular.tent-dev.hn-digest.>", id.NATSPrefix)
	}
}

func TestNATSRegistrarClose(t *testing.T) {
	// Close should not panic on a nil registrar fields.
	r := &NATSRegistrar{cfg: NATSConfig{}}
	r.Close()
}

func TestTokenModeRegister(t *testing.T) {
	r := &NATSRegistrar{
		cfg: NATSConfig{
			URL:   "nats://localhost:4222",
			Token: "test-token",
		},
	}

	id, err := CompileIdentity("tent-dev", "hn-digest")
	if err != nil {
		t.Fatal(err)
	}

	creds, err := r.Register(context.Background(), id)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if creds.URL != "nats://localhost:4222" {
		t.Errorf("URL = %q, want nats://localhost:4222", creds.URL)
	}
	if creds.Token != "test-token" {
		t.Errorf("Token = %q, want test-token", creds.Token)
	}
	if creds.AuthMethod != "token" {
		t.Errorf("AuthMethod = %q, want token", creds.AuthMethod)
	}
	if creds.SubjectPrefix != "tentacular.tent-dev.hn-digest.>" {
		t.Errorf("SubjectPrefix = %q, want tentacular.tent-dev.hn-digest.>", creds.SubjectPrefix)
	}
	if creds.Protocol != "nats" {
		t.Errorf("Protocol = %q, want nats", creds.Protocol)
	}
}

func TestTokenModeUnregister(t *testing.T) {
	r := &NATSRegistrar{
		cfg: NATSConfig{
			URL:   "nats://localhost:4222",
			Token: "test-token",
		},
	}

	id, _ := CompileIdentity("tent-dev", "hn-digest")
	if err := r.Unregister(context.Background(), id); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
}

func TestSPIFFEModeRegisterCreatesConfigMap(t *testing.T) {
	cs := fake.NewClientset()
	r := &NATSRegistrar{
		clientset: cs,
		cfg: NATSConfig{
			URL:            "nats://nats.tentacular-exoskeleton.svc.cluster.local:4222",
			SPIFFEEnabled:  true,
			AuthzConfigMap: "nats-tentacular-authz",
			AuthzNamespace: "tentacular-exoskeleton",
		},
	}

	// Create the namespace so the ConfigMap can live in it.
	ctx := context.Background()
	if _, err := cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "tentacular-exoskeleton"},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	id, _ := CompileIdentity("tent-dev", "hn-digest")
	creds, err := r.Register(ctx, id)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if creds.AuthMethod != "spiffe" {
		t.Errorf("AuthMethod = %q, want spiffe", creds.AuthMethod)
	}
	if creds.Token != "" {
		t.Errorf("Token should be empty in SPIFFE mode, got %q", creds.Token)
	}
	if creds.SubjectPrefix != "tentacular.tent-dev.hn-digest.>" {
		t.Errorf("SubjectPrefix = %q", creds.SubjectPrefix)
	}

	// Verify ConfigMap was created.
	cm, err := cs.CoreV1().ConfigMaps("tentacular-exoskeleton").Get(
		ctx, "nats-tentacular-authz", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("ConfigMap not found: %v", err)
	}

	conf := cm.Data["authorization.conf"]
	if conf == "" {
		t.Fatal("authorization.conf is empty")
	}

	entries := parseAuthzConfig(conf)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].User != "spiffe://tentacular/ns/tent-dev/tentacles/hn-digest" {
		t.Errorf("user = %q", entries[0].User)
	}
	if len(entries[0].PublishAllow) != 1 || entries[0].PublishAllow[0] != "tentacular.tent-dev.hn-digest.>" {
		t.Errorf("publish allow = %v", entries[0].PublishAllow)
	}
}

func TestSPIFFEModeRegisterMultipleTentacles(t *testing.T) {
	cs := fake.NewClientset()
	r := &NATSRegistrar{
		clientset: cs,
		cfg: NATSConfig{
			URL:            "nats://nats.local:4222",
			SPIFFEEnabled:  true,
			AuthzConfigMap: "nats-tentacular-authz",
			AuthzNamespace: "tentacular-exoskeleton",
		},
	}

	ctx := context.Background()
	if _, err := cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "tentacular-exoskeleton"},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	id1, _ := CompileIdentity("tent-dev", "hn-digest")
	id2, _ := CompileIdentity("tent-dev", "ai-roundup")

	if _, err := r.Register(ctx, id1); err != nil {
		t.Fatalf("Register id1: %v", err)
	}
	if _, err := r.Register(ctx, id2); err != nil {
		t.Fatalf("Register id2: %v", err)
	}

	cm, err := cs.CoreV1().ConfigMaps("tentacular-exoskeleton").Get(
		ctx, "nats-tentacular-authz", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	entries := parseAuthzConfig(cm.Data["authorization.conf"])
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Entries are sorted by user (SPIFFE URI).
	if entries[0].User != id2.Principal {
		t.Errorf("entry[0].User = %q, want %q", entries[0].User, id2.Principal)
	}
	if entries[1].User != id1.Principal {
		t.Errorf("entry[1].User = %q, want %q", entries[1].User, id1.Principal)
	}
}

func TestSPIFFEModeReRegisterUpdatesEntry(t *testing.T) {
	cs := fake.NewClientset()
	r := &NATSRegistrar{
		clientset: cs,
		cfg: NATSConfig{
			URL:            "nats://nats.local:4222",
			SPIFFEEnabled:  true,
			AuthzConfigMap: "nats-tentacular-authz",
			AuthzNamespace: "tentacular-exoskeleton",
		},
	}

	ctx := context.Background()
	if _, err := cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "tentacular-exoskeleton"},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	id, _ := CompileIdentity("tent-dev", "hn-digest")

	// Register twice.
	if _, err := r.Register(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Register(ctx, id); err != nil {
		t.Fatal(err)
	}

	cm, _ := cs.CoreV1().ConfigMaps("tentacular-exoskeleton").Get(
		ctx, "nats-tentacular-authz", metav1.GetOptions{})

	entries := parseAuthzConfig(cm.Data["authorization.conf"])
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after re-register, got %d", len(entries))
	}
}

func TestSPIFFEModeUnregisterRemovesEntry(t *testing.T) {
	cs := fake.NewClientset()
	r := &NATSRegistrar{
		clientset: cs,
		cfg: NATSConfig{
			URL:            "nats://nats.local:4222",
			SPIFFEEnabled:  true,
			AuthzConfigMap: "nats-tentacular-authz",
			AuthzNamespace: "tentacular-exoskeleton",
		},
	}

	ctx := context.Background()
	if _, err := cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "tentacular-exoskeleton"},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	id1, _ := CompileIdentity("tent-dev", "hn-digest")
	id2, _ := CompileIdentity("tent-dev", "ai-roundup")

	// Register both.
	if _, err := r.Register(ctx, id1); err != nil {
		t.Fatalf("Register id1: %v", err)
	}
	if _, err := r.Register(ctx, id2); err != nil {
		t.Fatalf("Register id2: %v", err)
	}

	// Unregister id1.
	if err := r.Unregister(ctx, id1); err != nil {
		t.Fatalf("Unregister: %v", err)
	}

	cm, _ := cs.CoreV1().ConfigMaps("tentacular-exoskeleton").Get(
		ctx, "nats-tentacular-authz", metav1.GetOptions{})

	entries := parseAuthzConfig(cm.Data["authorization.conf"])
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after unregister, got %d", len(entries))
	}
	if entries[0].User != id2.Principal {
		t.Errorf("remaining entry user = %q, want %q", entries[0].User, id2.Principal)
	}
}

func TestSPIFFEModeUnregisterMissingConfigMap(t *testing.T) {
	cs := fake.NewClientset()
	r := &NATSRegistrar{
		clientset: cs,
		cfg: NATSConfig{
			URL:            "nats://nats.local:4222",
			SPIFFEEnabled:  true,
			AuthzConfigMap: "nats-tentacular-authz",
			AuthzNamespace: "tentacular-exoskeleton",
		},
	}

	ctx := context.Background()
	id, _ := CompileIdentity("tent-dev", "hn-digest")

	// Should not error when ConfigMap doesn't exist.
	if err := r.Unregister(ctx, id); err != nil {
		t.Fatalf("Unregister should not error for missing ConfigMap: %v", err)
	}
}

func TestSPIFFEModeUnregisterMissingEntry(t *testing.T) {
	cs := fake.NewClientset()
	r := &NATSRegistrar{
		clientset: cs,
		cfg: NATSConfig{
			URL:            "nats://nats.local:4222",
			SPIFFEEnabled:  true,
			AuthzConfigMap: "nats-tentacular-authz",
			AuthzNamespace: "tentacular-exoskeleton",
		},
	}

	ctx := context.Background()
	if _, err := cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "tentacular-exoskeleton"},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	id1, _ := CompileIdentity("tent-dev", "hn-digest")
	id2, _ := CompileIdentity("tent-dev", "ai-roundup")

	// Register id1 only.
	if _, err := r.Register(ctx, id1); err != nil {
		t.Fatalf("Register id1: %v", err)
	}

	// Unregister id2 (not registered) should succeed.
	if err := r.Unregister(ctx, id2); err != nil {
		t.Fatalf("Unregister should not error for missing entry: %v", err)
	}

	// id1 should still be there.
	cm, _ := cs.CoreV1().ConfigMaps("tentacular-exoskeleton").Get(
		ctx, "nats-tentacular-authz", metav1.GetOptions{})
	entries := parseAuthzConfig(cm.Data["authorization.conf"])
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestNewNATSRegistrarSPIFFERequiresClientset(t *testing.T) {
	cfg := NATSConfig{
		URL:            "nats://nats.local:4222",
		SPIFFEEnabled:  true,
		AuthzConfigMap: "nats-tentacular-authz",
		AuthzNamespace: "tentacular-exoskeleton",
	}

	_, err := NewNATSRegistrar(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected error when clientset is nil in SPIFFE mode")
	}
}

func TestNewNATSRegistrarSPIFFEWithClientset(t *testing.T) {
	cs := fake.NewClientset()
	cfg := NATSConfig{
		URL:            "nats://nats.local:4222",
		SPIFFEEnabled:  true,
		AuthzConfigMap: "nats-tentacular-authz",
		AuthzNamespace: "tentacular-exoskeleton",
	}

	r, err := NewNATSRegistrar(context.Background(), cfg, cs)
	if err != nil {
		t.Fatalf("NewNATSRegistrar: %v", err)
	}
	if r.clientset == nil {
		t.Error("clientset should be set")
	}
}

func TestParseAuthzConfigEmpty(t *testing.T) {
	entries := parseAuthzConfig("")
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestParseAuthzConfigRoundTrip(t *testing.T) {
	original := []natsAuthzEntry{
		{
			User:           "spiffe://tentacular/ns/tent-dev/tentacles/ai-roundup",
			PublishAllow:   []string{"tentacular.tent-dev.ai-roundup.>"},
			SubscribeAllow: []string{"tentacular.tent-dev.ai-roundup.>"},
		},
		{
			User:           "spiffe://tentacular/ns/tent-dev/tentacles/hn-digest",
			PublishAllow:   []string{"tentacular.tent-dev.hn-digest.>"},
			SubscribeAllow: []string{"tentacular.tent-dev.hn-digest.>"},
		},
	}

	rendered := renderAuthzConfig(original)
	parsed := parseAuthzConfig(rendered)

	if len(parsed) != len(original) {
		t.Fatalf("expected %d entries, got %d", len(original), len(parsed))
	}

	for i, e := range parsed {
		if e.User != original[i].User {
			t.Errorf("entry[%d].User = %q, want %q", i, e.User, original[i].User)
		}
		if len(e.PublishAllow) != 1 || e.PublishAllow[0] != original[i].PublishAllow[0] {
			t.Errorf("entry[%d].PublishAllow = %v, want %v", i, e.PublishAllow, original[i].PublishAllow)
		}
		if len(e.SubscribeAllow) != 1 || e.SubscribeAllow[0] != original[i].SubscribeAllow[0] {
			t.Errorf("entry[%d].SubscribeAllow = %v, want %v", i, e.SubscribeAllow, original[i].SubscribeAllow)
		}
	}
}

func TestRenderAuthzConfigSortsDeterministically(t *testing.T) {
	entries := []natsAuthzEntry{
		{User: "spiffe://b", PublishAllow: []string{"b.>"}, SubscribeAllow: []string{"b.>"}},
		{User: "spiffe://a", PublishAllow: []string{"a.>"}, SubscribeAllow: []string{"a.>"}},
	}

	rendered := renderAuthzConfig(entries)
	parsed := parseAuthzConfig(rendered)

	if parsed[0].User != "spiffe://a" {
		t.Errorf("first entry should be spiffe://a, got %q", parsed[0].User)
	}
	if parsed[1].User != "spiffe://b" {
		t.Errorf("second entry should be spiffe://b, got %q", parsed[1].User)
	}
}

func TestConfigMapLabels(t *testing.T) {
	cs := fake.NewClientset()
	r := &NATSRegistrar{
		clientset: cs,
		cfg: NATSConfig{
			URL:            "nats://nats.local:4222",
			SPIFFEEnabled:  true,
			AuthzConfigMap: "nats-tentacular-authz",
			AuthzNamespace: "tentacular-exoskeleton",
		},
	}

	ctx := context.Background()
	if _, err := cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "tentacular-exoskeleton"},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	id, _ := CompileIdentity("tent-dev", "hn-digest")
	if _, err := r.Register(ctx, id); err != nil {
		t.Fatalf("Register: %v", err)
	}

	cm, _ := cs.CoreV1().ConfigMaps("tentacular-exoskeleton").Get(
		ctx, "nats-tentacular-authz", metav1.GetOptions{})

	if cm.Labels["app.kubernetes.io/managed-by"] != "tentacular" {
		t.Errorf("missing managed-by label")
	}
	if cm.Labels["tentacular.io/exoskeleton"] != "true" {
		t.Errorf("missing exoskeleton label")
	}
}

func TestDualModeSelection(t *testing.T) {
	// Token mode returns token, SPIFFE mode does not.
	cs := fake.NewClientset()
	ctx := context.Background()
	if _, err := cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "tentacular-exoskeleton"},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	id, _ := CompileIdentity("tent-dev", "test-wf")

	// Token mode.
	tokenReg := &NATSRegistrar{
		cfg: NATSConfig{
			URL:           "nats://localhost:4222",
			Token:         "my-token",
			SPIFFEEnabled: false,
		},
	}

	tokenCreds, err := tokenReg.Register(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if tokenCreds.AuthMethod != "token" {
		t.Errorf("token mode: AuthMethod = %q", tokenCreds.AuthMethod)
	}
	if tokenCreds.Token != "my-token" {
		t.Errorf("token mode: Token = %q", tokenCreds.Token)
	}

	// SPIFFE mode.
	spiffeReg := &NATSRegistrar{
		clientset: cs,
		cfg: NATSConfig{
			URL:            "nats://localhost:4222",
			SPIFFEEnabled:  true,
			AuthzConfigMap: "nats-tentacular-authz",
			AuthzNamespace: "tentacular-exoskeleton",
		},
	}

	spiffeCreds, err := spiffeReg.Register(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if spiffeCreds.AuthMethod != "spiffe" {
		t.Errorf("spiffe mode: AuthMethod = %q", spiffeCreds.AuthMethod)
	}
	if spiffeCreds.Token != "" {
		t.Errorf("spiffe mode: Token should be empty, got %q", spiffeCreds.Token)
	}
}
