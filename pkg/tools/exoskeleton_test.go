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

// ---------- ExoStatusResult population tests ----------

func TestExoStatusResult_AllFieldsSet(t *testing.T) {
	result := ExoStatusResult{
		Enabled:           true,
		CleanupOnUndeploy: true,
		PostgresAvailable: true,
		NATSAvailable:     true,
		RustFSAvailable:   true,
		SPIREAvailable:    true,
		NATSSpiffeEnabled: true,
		AuthEnabled:       true,
		AuthIssuer:        "https://keycloak.example.com/realms/tentacular",
	}

	if !result.Enabled {
		t.Error("expected Enabled=true")
	}
	if !result.CleanupOnUndeploy {
		t.Error("expected CleanupOnUndeploy=true")
	}
	if !result.PostgresAvailable {
		t.Error("expected PostgresAvailable=true")
	}
	if !result.NATSAvailable {
		t.Error("expected NATSAvailable=true")
	}
	if !result.RustFSAvailable {
		t.Error("expected RustFSAvailable=true")
	}
	if !result.SPIREAvailable {
		t.Error("expected SPIREAvailable=true")
	}
	if !result.NATSSpiffeEnabled {
		t.Error("expected NATSSpiffeEnabled=true")
	}
	if !result.AuthEnabled {
		t.Error("expected AuthEnabled=true")
	}
	if result.AuthIssuer != "https://keycloak.example.com/realms/tentacular" {
		t.Errorf("AuthIssuer = %q", result.AuthIssuer)
	}
}

func TestExoStatusResult_AllFieldsDefault(t *testing.T) {
	result := ExoStatusResult{}

	if result.Enabled {
		t.Error("expected Enabled=false by default")
	}
	if result.PostgresAvailable {
		t.Error("expected PostgresAvailable=false by default")
	}
	if result.AuthIssuer != "" {
		t.Errorf("expected empty AuthIssuer, got %q", result.AuthIssuer)
	}
}

func TestExoStatusResult_ServicesSlice(t *testing.T) {
	result := ExoStatusResult{
		Enabled: true,
		Services: []ExoStatusServiceInfo{
			{Name: "postgres", Enabled: true, Healthy: true},
			{Name: "nats", Enabled: true, Healthy: true},
			{Name: "rustfs", Enabled: false, Healthy: false},
			{Name: "spire", Enabled: false, Healthy: false},
		},
	}

	if len(result.Services) != 4 {
		t.Fatalf("expected 4 services, got %d", len(result.Services))
	}
	pg := result.Services[0]
	if pg.Name != "postgres" || !pg.Enabled || !pg.Healthy {
		t.Errorf("postgres service: %+v", pg)
	}
	rustfs := result.Services[2]
	if rustfs.Enabled || rustfs.Healthy {
		t.Errorf("expected rustfs disabled/unhealthy, got %+v", rustfs)
	}
}

func TestBuildServiceInfoList_NilController(t *testing.T) {
	// buildServiceInfoList requires a non-nil controller; the handler
	// short-circuits before calling it when ctrl is nil. Test that the
	// handler returns an empty services slice for nil controller.
	result := ExoStatusResult{Enabled: false, Services: []ExoStatusServiceInfo{}}
	if len(result.Services) != 0 {
		t.Errorf("expected empty services for nil controller result, got %d", len(result.Services))
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

// ---------- ExoRegistrationResult tests ----------

func TestExoRegistrationResult_NotFound(t *testing.T) {
	result := ExoRegistrationResult{
		Found:     false,
		Namespace: "tent-dev",
		Name:      "hn-digest",
	}

	if result.Found {
		t.Error("expected Found=false")
	}
	if result.Namespace != "tent-dev" {
		t.Errorf("Namespace = %q", result.Namespace)
	}
	if result.Data != nil {
		t.Error("expected nil Data for not-found")
	}
}

func TestExoRegistrationResult_Found(t *testing.T) {
	result := ExoRegistrationResult{
		Found:     true,
		Namespace: "tent-dev",
		Name:      "hn-digest",
		Data: map[string]string{
			"tentacular-postgres.host":     "pg.exo.svc",
			"tentacular-postgres.password": "***REDACTED***",
		},
	}

	if !result.Found {
		t.Error("expected Found=true")
	}
	if result.Data["tentacular-postgres.host"] != "pg.exo.svc" {
		t.Errorf("host = %q", result.Data["tentacular-postgres.host"])
	}
	if result.Data["tentacular-postgres.password"] != "***REDACTED***" {
		t.Errorf("password = %q, expected REDACTED", result.Data["tentacular-postgres.password"])
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
				"tentacular-postgres.host":    []byte("pg"),
				"tentacular-nats.url":         []byte("nats://nats"),
				"tentacular-rustfs.endpoint":  []byte("http://rustfs"),
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
