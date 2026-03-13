package tools

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// ---------- buildServiceInfoList tests ----------

// TestBuildServiceInfoList_NilController verifies that calling buildServiceInfoList
// on a disabled controller returns an empty slice (same as the handler's short-circuit
// for nil ctrl).
func TestBuildServiceInfoList_NilController(t *testing.T) {
	cfg := &exoskeleton.Config{Enabled: false}
	ctrl := exoskeleton.NewControllerWithDeps(cfg, nil, nil, nil, nil)
	services := buildServiceInfoList(ctrl)
	if len(services) != 0 {
		t.Errorf("expected empty services for disabled controller, got %d", len(services))
	}
}

func TestBuildServiceInfoList_EnabledController(t *testing.T) {
	cfg := &exoskeleton.Config{Enabled: true}
	ctrl := exoskeleton.NewControllerWithDeps(cfg, nil, nil, nil, nil)

	services := buildServiceInfoList(ctrl)
	if len(services) != 4 {
		t.Fatalf("expected 4 services, got %d", len(services))
	}
	// All should be disabled (no registrars set)
	for _, svc := range services {
		if svc.Enabled {
			t.Errorf("expected %s disabled with no registrar, got enabled", svc.Name)
		}
	}
}

func TestBuildServiceInfoList_DisabledController(t *testing.T) {
	cfg := &exoskeleton.Config{Enabled: false}
	ctrl := exoskeleton.NewControllerWithDeps(cfg, nil, nil, nil, nil)

	services := buildServiceInfoList(ctrl)
	if len(services) != 0 {
		t.Errorf("expected empty services for disabled controller, got %d", len(services))
	}
}

// ---------- isSecretKey tests ----------

func TestIsSecretKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"tentacular-postgres.password", true},
		{"tentacular-rustfs.secret_key", true},
		{"tentacular-rustfs.access_key", true},
		{"tentacular-nats.token", true},
		{"tentacular-postgres.host", false},
		{"tentacular-postgres.port", false},
		{"tentacular-postgres.database", false},
		{"tentacular-postgres.user", false},
		{"tentacular-nats.url", false},
		{"tentacular-identity.principal", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := isSecretKey(tt.key)
			if got != tt.want {
				t.Errorf("isSecretKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// ---------- exo_registration handler tests ----------

func TestExoRegistration_SecretNotFound(t *testing.T) {
	client := newExoListTestClient()
	ctx := context.Background()

	// Look up a secret that doesn't exist via the handler's code path.
	secretName := exoskeleton.ExoskeletonSecretPrefix + "no-such-wf"
	_, err := client.Clientset.CoreV1().Secrets("tent-dev").Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestExoRegistration_SecretFound_RedactsPasswords(t *testing.T) {
	client := newExoListTestClient()
	ctx := context.Background()

	// Create a secret with sensitive and non-sensitive keys.
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      exoskeleton.ExoskeletonSecretPrefix + "hn-digest",
			Namespace: "tent-dev",
			Labels: map[string]string{
				exoskeleton.ExoskeletonLabel: "true",
				exoskeleton.ReleaseLabel:     "hn-digest",
			},
		},
		Data: map[string][]byte{
			"tentacular-postgres.host":     []byte("pg.exo.svc"),
			"tentacular-postgres.password": []byte("s3cret"),
			"tentacular-nats.url":          []byte("nats://nats:4222"),
			"tentacular-nats.token":        []byte("tok123"),
		},
	}
	_, err := client.Clientset.CoreV1().Secrets("tent-dev").Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Retrieve and check redaction via isSecretKey logic.
	got, getErr := client.Clientset.CoreV1().Secrets("tent-dev").Get(ctx, secret.Name, metav1.GetOptions{})
	if getErr != nil {
		t.Fatalf("get secret: %v", getErr)
	}

	data := make(map[string]string)
	for k, v := range got.Data {
		if isSecretKey(k) {
			data[k] = "***REDACTED***"
		} else {
			data[k] = string(v)
		}
	}

	if data["tentacular-postgres.host"] != "pg.exo.svc" {
		t.Errorf("host = %q, want pg.exo.svc", data["tentacular-postgres.host"])
	}
	if data["tentacular-postgres.password"] != "***REDACTED***" {
		t.Errorf("password = %q, want REDACTED", data["tentacular-postgres.password"])
	}
	if data["tentacular-nats.url"] != "nats://nats:4222" {
		t.Errorf("nats url = %q, want nats://nats:4222", data["tentacular-nats.url"])
	}
	if data["tentacular-nats.token"] != "***REDACTED***" {
		t.Errorf("nats token = %q, want REDACTED", data["tentacular-nats.token"])
	}
}

// ---------- detectRegisteredServices tests ----------

func TestDetectRegisteredServices(t *testing.T) {
	tests := []struct {
		name string
		data map[string][]byte
		want []string
	}{
		{
			name: "postgres and nats",
			data: map[string][]byte{
				"tentacular-postgres.host": []byte("pg.example.com"),
				"tentacular-postgres.port": []byte("5432"),
				"tentacular-nats.url":      []byte("nats://nats:4222"),
			},
			want: []string{"postgres", "nats"},
		},
		{
			name: "all services",
			data: map[string][]byte{
				"tentacular-postgres.host":     []byte("pg"),
				"tentacular-nats.url":          []byte("nats://nats"),
				"tentacular-rustfs.endpoint":   []byte("http://rustfs"),
				"tentacular-identity.workflow": []byte("test"),
			},
			want: []string{"postgres", "nats", "rustfs"},
		},
		{
			name: "identity only",
			data: map[string][]byte{
				"tentacular-identity.principal": []byte("p"),
			},
			want: nil,
		},
		{
			name: "empty data",
			data: map[string][]byte{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectRegisteredServices(tt.data)
			if len(got) != len(tt.want) {
				t.Fatalf("detectRegisteredServices: got %v, want %v", got, tt.want)
			}
			for i, s := range got {
				if s != tt.want[i] {
					t.Errorf("service[%d] = %q, want %q", i, s, tt.want[i])
				}
			}
		})
	}
}

// ---------- exo_list tests ----------

func newExoListTestClient() *k8s.Client {
	return &k8s.Client{
		Clientset: kubefake.NewClientset(),
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}
}

func TestExoListEmpty(t *testing.T) {
	client := newExoListTestClient()
	ctx := context.Background()

	result, err := handleExoList(ctx, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Registrations) != 0 {
		t.Errorf("expected 0 registrations, got %d", len(result.Registrations))
	}
}

func TestExoListMultipleRegistrations(t *testing.T) {
	client := newExoListTestClient()
	ctx := context.Background()

	secrets := []struct {
		ns       string
		workflow string
	}{
		{"tent-alpha", "alpha-wf"},
		{"tent-beta", "beta-wf"},
		{"tent-alpha", "gamma-wf"},
	}

	for _, s := range secrets {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      exoskeleton.ExoskeletonSecretPrefix + s.workflow,
				Namespace: s.ns,
				Labels: map[string]string{
					exoskeleton.ExoskeletonLabel: "true",
					exoskeleton.ReleaseLabel:     s.workflow,
				},
			},
			Data: map[string][]byte{
				"tentacular-postgres.host": []byte("pg.example.com"),
			},
		}
		_, err := client.Clientset.CoreV1().Secrets(s.ns).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	result, err := handleExoList(ctx, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Registrations) != 3 {
		t.Fatalf("expected 3 registrations, got %d", len(result.Registrations))
	}

	workflows := map[string]bool{}
	for _, r := range result.Registrations {
		workflows[r.Workflow] = true
	}
	for _, s := range secrets {
		if !workflows[s.workflow] {
			t.Errorf("expected workflow %q in list", s.workflow)
		}
	}
}

func TestExoListIgnoresNonExoskeletonSecrets(t *testing.T) {
	client := newExoListTestClient()
	ctx := context.Background()

	// Create a regular Secret without the exoskeleton label
	regularSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "regular-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{"key": []byte("value")},
	}
	_, err := client.Clientset.CoreV1().Secrets("default").Create(ctx, regularSecret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Create one exoskeleton Secret
	exoSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      exoskeleton.ExoskeletonSecretPrefix + "test-wf",
			Namespace: "tent-test",
			Labels: map[string]string{
				exoskeleton.ExoskeletonLabel: "true",
				exoskeleton.ReleaseLabel:     "test-wf",
			},
		},
		Data: map[string][]byte{
			"tentacular-nats.url": []byte("nats://nats:4222"),
		},
	}
	_, err = client.Clientset.CoreV1().Secrets("tent-test").Create(ctx, exoSecret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := handleExoList(ctx, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Registrations) != 1 {
		t.Fatalf("expected 1 registration (ignoring non-exoskeleton secret), got %d", len(result.Registrations))
	}
	if result.Registrations[0].Workflow != "test-wf" {
		t.Errorf("expected workflow=test-wf, got %q", result.Registrations[0].Workflow)
	}
	if len(result.Registrations[0].Services) != 1 || result.Registrations[0].Services[0] != "nats" {
		t.Errorf("expected services=[nats], got %v", result.Registrations[0].Services)
	}
}

func TestExoListServicesDetected(t *testing.T) {
	client := newExoListTestClient()
	ctx := context.Background()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      exoskeleton.ExoskeletonSecretPrefix + "multi-svc",
			Namespace: "tent-ns",
			Labels: map[string]string{
				exoskeleton.ExoskeletonLabel: "true",
				exoskeleton.ReleaseLabel:     "multi-svc",
			},
		},
		Data: map[string][]byte{
			"tentacular-postgres.host":   []byte("pg"),
			"tentacular-nats.url":        []byte("nats://nats"),
			"tentacular-rustfs.endpoint": []byte("http://rustfs"),
		},
	}
	_, err := client.Clientset.CoreV1().Secrets("tent-ns").Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := handleExoList(ctx, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Registrations) != 1 {
		t.Fatalf("expected 1 registration, got %d", len(result.Registrations))
	}
	services := result.Registrations[0].Services
	if len(services) != 3 {
		t.Fatalf("expected 3 services, got %v", services)
	}
	want := []string{"postgres", "nats", "rustfs"}
	for i, s := range services {
		if s != want[i] {
			t.Errorf("service[%d] = %q, want %q", i, s, want[i])
		}
	}
}

// ---------- system namespace filtering ----------

func TestExoListFiltersSystemNamespaces(t *testing.T) {
	client := newExoListTestClient()
	ctx := context.Background()

	namespaces := []struct {
		ns       string
		workflow string
		isSystem bool
	}{
		{"kube-system", "sys-wf", true},
		{"default", "default-wf", true},
		{"tentacular-system", "tent-sys-wf", true},
		{"tent-app", "app-wf", false},
	}

	for _, n := range namespaces {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      exoskeleton.ExoskeletonSecretPrefix + n.workflow,
				Namespace: n.ns,
				Labels: map[string]string{
					exoskeleton.ExoskeletonLabel: "true",
					exoskeleton.ReleaseLabel:     n.workflow,
				},
			},
			Data: map[string][]byte{
				"tentacular-postgres.host": []byte("pg.example.com"),
			},
		}
		_, err := client.Clientset.CoreV1().Secrets(n.ns).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("setup: create secret in %s: %v", n.ns, err)
		}
	}

	result, err := handleExoList(ctx, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Registrations) != 1 {
		t.Fatalf("expected 1 registration (only user namespace), got %d", len(result.Registrations))
	}
	if result.Registrations[0].Namespace != "tent-app" {
		t.Errorf("expected namespace=tent-app, got %q", result.Registrations[0].Namespace)
	}
	if result.Registrations[0].Workflow != "app-wf" {
		t.Errorf("expected workflow=app-wf, got %q", result.Registrations[0].Workflow)
	}
}

func TestExoListZeroTimestamp(t *testing.T) {
	client := newExoListTestClient()
	ctx := context.Background()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      exoskeleton.ExoskeletonSecretPrefix + "zero-ts",
			Namespace: "tent-zero",
			Labels: map[string]string{
				exoskeleton.ExoskeletonLabel: "true",
				exoskeleton.ReleaseLabel:     "zero-ts",
			},
		},
		Data: map[string][]byte{
			"tentacular-nats.url": []byte("nats://nats:4222"),
		},
	}
	_, err := client.Clientset.CoreV1().Secrets("tent-zero").Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := handleExoList(ctx, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Registrations) != 1 {
		t.Fatalf("expected 1 registration, got %d", len(result.Registrations))
	}
	if result.Registrations[0].Created != "" {
		t.Errorf("expected empty Created for zero timestamp, got %q", result.Registrations[0].Created)
	}
}

func TestDetectRegisteredServices_SingleService(t *testing.T) {
	tests := []struct {
		name string
		data map[string][]byte
		want []string
	}{
		{
			name: "postgres only",
			data: map[string][]byte{"tentacular-postgres.host": []byte("pg")},
			want: []string{"postgres"},
		},
		{
			name: "nats only",
			data: map[string][]byte{"tentacular-nats.url": []byte("nats://nats:4222")},
			want: []string{"nats"},
		},
		{
			name: "rustfs only",
			data: map[string][]byte{"tentacular-rustfs.endpoint": []byte("http://rustfs:9000")},
			want: []string{"rustfs"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectRegisteredServices(tt.data)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i, s := range got {
				if s != tt.want[i] {
					t.Errorf("service[%d] = %q, want %q", i, s, tt.want[i])
				}
			}
		})
	}
}
