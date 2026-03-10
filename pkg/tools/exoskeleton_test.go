package tools

import (
	"testing"
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
