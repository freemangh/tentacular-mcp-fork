package exoskeleton

import (
	"os"
	"testing"
)

func TestLoadFromEnv(t *testing.T) {
	// Save and restore environment
	envVars := []string{
		"TENTACULAR_EXOSKELETON_ENABLED",
		"TENTACULAR_EXOSKELETON_CLEANUP_ON_UNDEPLOY",
		"TENTACULAR_POSTGRES_ADMIN_HOST",
		"TENTACULAR_POSTGRES_ADMIN_PORT",
		"TENTACULAR_POSTGRES_ADMIN_DATABASE",
		"TENTACULAR_POSTGRES_ADMIN_USER",
		"TENTACULAR_POSTGRES_ADMIN_PASSWORD",
		"TENTACULAR_NATS_URL",
		"TENTACULAR_NATS_TOKEN",
		"TENTACULAR_RUSTFS_ENDPOINT",
		"TENTACULAR_RUSTFS_ACCESS_KEY",
		"TENTACULAR_RUSTFS_SECRET_KEY",
		"TENTACULAR_RUSTFS_BUCKET",
		"TENTACULAR_RUSTFS_REGION",
		"TENTACULAR_EXOSKELETON_AUTH_ENABLED",
		"TENTACULAR_KEYCLOAK_ISSUER",
		"TENTACULAR_KEYCLOAK_CLIENT_ID",
		"TENTACULAR_KEYCLOAK_CLIENT_SECRET",
	}
	saved := make(map[string]string)
	for _, k := range envVars {
		saved[k] = os.Getenv(k)
	}
	t.Cleanup(func() {
		for k, v := range saved {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	})

	// Clear all
	for _, k := range envVars {
		os.Unsetenv(k)
	}

	t.Run("disabled by default", func(t *testing.T) {
		cfg := LoadFromEnv()
		if cfg.Enabled {
			t.Error("expected Enabled=false when env not set")
		}
		if cfg.PostgresEnabled() {
			t.Error("expected PostgresEnabled()=false")
		}
		if cfg.NATSEnabled() {
			t.Error("expected NATSEnabled()=false")
		}
		if cfg.RustFSEnabled() {
			t.Error("expected RustFSEnabled()=false")
		}
	})

	t.Run("enabled with postgres", func(t *testing.T) {
		os.Setenv("TENTACULAR_EXOSKELETON_ENABLED", "true")
		os.Setenv("TENTACULAR_POSTGRES_ADMIN_HOST", "pg.local")
		os.Setenv("TENTACULAR_POSTGRES_ADMIN_USER", "admin")
		os.Setenv("TENTACULAR_POSTGRES_ADMIN_PASSWORD", "secret")

		cfg := LoadFromEnv()
		if !cfg.Enabled {
			t.Error("expected Enabled=true")
		}
		if !cfg.PostgresEnabled() {
			t.Error("expected PostgresEnabled()=true")
		}
		if cfg.Postgres.Port != "5432" {
			t.Errorf("expected default port 5432, got %q", cfg.Postgres.Port)
		}
		if cfg.Postgres.Database != "tentacular" {
			t.Errorf("expected default database tentacular, got %q", cfg.Postgres.Database)
		}

		os.Unsetenv("TENTACULAR_EXOSKELETON_ENABLED")
		os.Unsetenv("TENTACULAR_POSTGRES_ADMIN_HOST")
		os.Unsetenv("TENTACULAR_POSTGRES_ADMIN_USER")
		os.Unsetenv("TENTACULAR_POSTGRES_ADMIN_PASSWORD")
	})

	t.Run("enabled with nats", func(t *testing.T) {
		os.Setenv("TENTACULAR_EXOSKELETON_ENABLED", "1")
		os.Setenv("TENTACULAR_NATS_URL", "nats://localhost:4222")
		os.Setenv("TENTACULAR_NATS_TOKEN", "tok")

		cfg := LoadFromEnv()
		if !cfg.NATSEnabled() {
			t.Error("expected NATSEnabled()=true")
		}

		os.Unsetenv("TENTACULAR_EXOSKELETON_ENABLED")
		os.Unsetenv("TENTACULAR_NATS_URL")
		os.Unsetenv("TENTACULAR_NATS_TOKEN")
	})

	t.Run("enabled with rustfs", func(t *testing.T) {
		os.Setenv("TENTACULAR_EXOSKELETON_ENABLED", "yes")
		os.Setenv("TENTACULAR_RUSTFS_ENDPOINT", "http://minio:9000")
		os.Setenv("TENTACULAR_RUSTFS_ACCESS_KEY", "ak")
		os.Setenv("TENTACULAR_RUSTFS_SECRET_KEY", "sk")

		cfg := LoadFromEnv()
		if !cfg.RustFSEnabled() {
			t.Error("expected RustFSEnabled()=true")
		}
		if cfg.RustFS.Bucket != "tentacular" {
			t.Errorf("expected default bucket tentacular, got %q", cfg.RustFS.Bucket)
		}
		if cfg.RustFS.Region != "us-east-1" {
			t.Errorf("expected default region us-east-1, got %q", cfg.RustFS.Region)
		}

		os.Unsetenv("TENTACULAR_EXOSKELETON_ENABLED")
		os.Unsetenv("TENTACULAR_RUSTFS_ENDPOINT")
		os.Unsetenv("TENTACULAR_RUSTFS_ACCESS_KEY")
		os.Unsetenv("TENTACULAR_RUSTFS_SECRET_KEY")
	})

	t.Run("cleanup on undeploy", func(t *testing.T) {
		os.Setenv("TENTACULAR_EXOSKELETON_CLEANUP_ON_UNDEPLOY", "true")
		cfg := LoadFromEnv()
		if !cfg.CleanupOnUndeploy {
			t.Error("expected CleanupOnUndeploy=true")
		}
		os.Unsetenv("TENTACULAR_EXOSKELETON_CLEANUP_ON_UNDEPLOY")
	})

	t.Run("auth disabled by default", func(t *testing.T) {
		cfg := LoadFromEnv()
		if cfg.AuthEnabled() {
			t.Error("expected AuthEnabled()=false when env not set")
		}
	})

	t.Run("auth enabled with config", func(t *testing.T) {
		os.Setenv("TENTACULAR_EXOSKELETON_AUTH_ENABLED", "true")
		os.Setenv("TENTACULAR_KEYCLOAK_ISSUER", "http://keycloak.local/realms/test")
		os.Setenv("TENTACULAR_KEYCLOAK_CLIENT_ID", "tentacular-mcp")
		os.Setenv("TENTACULAR_KEYCLOAK_CLIENT_SECRET", "secret123")

		cfg := LoadFromEnv()
		if !cfg.AuthEnabled() {
			t.Error("expected AuthEnabled()=true")
		}
		if cfg.Auth.IssuerURL != "http://keycloak.local/realms/test" {
			t.Errorf("expected issuer URL, got %q", cfg.Auth.IssuerURL)
		}
		if cfg.Auth.ClientID != "tentacular-mcp" {
			t.Errorf("expected client ID, got %q", cfg.Auth.ClientID)
		}
		if cfg.Auth.ClientSecret != "secret123" {
			t.Errorf("expected client secret, got %q", cfg.Auth.ClientSecret)
		}

		os.Unsetenv("TENTACULAR_EXOSKELETON_AUTH_ENABLED")
		os.Unsetenv("TENTACULAR_KEYCLOAK_ISSUER")
		os.Unsetenv("TENTACULAR_KEYCLOAK_CLIENT_ID")
		os.Unsetenv("TENTACULAR_KEYCLOAK_CLIENT_SECRET")
	})

	t.Run("auth enabled but missing issuer", func(t *testing.T) {
		os.Setenv("TENTACULAR_EXOSKELETON_AUTH_ENABLED", "true")
		os.Setenv("TENTACULAR_KEYCLOAK_CLIENT_ID", "tentacular-mcp")

		cfg := LoadFromEnv()
		if cfg.AuthEnabled() {
			t.Error("expected AuthEnabled()=false when issuer is missing")
		}

		os.Unsetenv("TENTACULAR_EXOSKELETON_AUTH_ENABLED")
		os.Unsetenv("TENTACULAR_KEYCLOAK_CLIENT_ID")
	})

	t.Run("not enabled without top flag", func(t *testing.T) {
		// Postgres creds present but top-level flag off
		os.Setenv("TENTACULAR_POSTGRES_ADMIN_HOST", "pg.local")
		os.Setenv("TENTACULAR_POSTGRES_ADMIN_USER", "admin")
		os.Setenv("TENTACULAR_POSTGRES_ADMIN_PASSWORD", "secret")

		cfg := LoadFromEnv()
		if cfg.PostgresEnabled() {
			t.Error("expected PostgresEnabled()=false when top-level disabled")
		}

		os.Unsetenv("TENTACULAR_POSTGRES_ADMIN_HOST")
		os.Unsetenv("TENTACULAR_POSTGRES_ADMIN_USER")
		os.Unsetenv("TENTACULAR_POSTGRES_ADMIN_PASSWORD")
	})
}
