package exoskeleton

import (
	"os"
	"strings"
)

// Config holds all exoskeleton configuration, loaded from environment variables.
type Config struct {
	Enabled           bool
	CleanupOnUndeploy bool
	Postgres          PostgresConfig
	NATS              NATSConfig
	RustFS            RustFSConfig
	Auth              AuthConfig
	SPIRE             SPIREConfig
}

// SPIREConfig holds SPIRE identity registration configuration.
type SPIREConfig struct {
	Enabled   bool
	ClassName string // default: "tentacular-system-spire"
}

// PostgresConfig holds admin connection details for the Postgres registrar.
type PostgresConfig struct {
	Host     string
	Port     string
	Database string
	User     string
	Password string
	SSLMode  string
}

// NATSConfig holds connection details for the NATS registrar.
type NATSConfig struct {
	URL   string
	Token string
}

// RustFSConfig holds admin connection details for the RustFS (MinIO-compatible) registrar.
type RustFSConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
}

// LoadFromEnv reads exoskeleton configuration from environment variables.
func LoadFromEnv() *Config {
	return &Config{
		Enabled:           envBool("TENTACULAR_EXOSKELETON_ENABLED"),
		CleanupOnUndeploy: envBool("TENTACULAR_EXOSKELETON_CLEANUP_ON_UNDEPLOY"),
		Postgres: PostgresConfig{
			Host:     os.Getenv("TENTACULAR_POSTGRES_ADMIN_HOST"),
			Port:     envDefault("TENTACULAR_POSTGRES_ADMIN_PORT", "5432"),
			Database: envDefault("TENTACULAR_POSTGRES_ADMIN_DATABASE", "tentacular"),
			User:     os.Getenv("TENTACULAR_POSTGRES_ADMIN_USER"),
			Password: os.Getenv("TENTACULAR_POSTGRES_ADMIN_PASSWORD"),
			SSLMode:  envDefault("TENTACULAR_POSTGRES_SSLMODE", "disable"),
		},
		NATS: NATSConfig{
			URL:   os.Getenv("TENTACULAR_NATS_URL"),
			Token: os.Getenv("TENTACULAR_NATS_TOKEN"),
		},
		RustFS: RustFSConfig{
			Endpoint:  os.Getenv("TENTACULAR_RUSTFS_ENDPOINT"),
			AccessKey: os.Getenv("TENTACULAR_RUSTFS_ACCESS_KEY"),
			SecretKey: os.Getenv("TENTACULAR_RUSTFS_SECRET_KEY"),
			Bucket:    envDefault("TENTACULAR_RUSTFS_BUCKET", "tentacular"),
			Region:    envDefault("TENTACULAR_RUSTFS_REGION", "us-east-1"),
		},
		Auth: AuthConfig{
			Enabled:      envBool("TENTACULAR_EXOSKELETON_AUTH_ENABLED"),
			IssuerURL:    os.Getenv("TENTACULAR_KEYCLOAK_ISSUER"),
			ClientID:     os.Getenv("TENTACULAR_KEYCLOAK_CLIENT_ID"),
			ClientSecret: os.Getenv("TENTACULAR_KEYCLOAK_CLIENT_SECRET"),
		},
		SPIRE: SPIREConfig{
			Enabled:   envBool("TENTACULAR_EXOSKELETON_SPIRE_ENABLED"),
			ClassName: envDefault("TENTACULAR_SPIRE_CLASS_NAME", "tentacular-system-spire"),
		},
	}
}

// PostgresEnabled returns true when exoskeleton is enabled and Postgres
// admin credentials are configured.
func (c *Config) PostgresEnabled() bool {
	return c.Enabled && c.Postgres.Host != "" && c.Postgres.User != "" && c.Postgres.Password != ""
}

// NATSEnabled returns true when exoskeleton is enabled and the NATS URL
// is configured.
func (c *Config) NATSEnabled() bool {
	return c.Enabled && c.NATS.URL != ""
}

// RustFSEnabled returns true when exoskeleton is enabled and the RustFS
// endpoint and credentials are configured.
func (c *Config) RustFSEnabled() bool {
	return c.Enabled && c.RustFS.Endpoint != "" && c.RustFS.AccessKey != "" && c.RustFS.SecretKey != ""
}

// AuthEnabled returns true when exoskeleton auth is enabled and the
// required OIDC configuration is present.
func (c *Config) AuthEnabled() bool {
	return c.Auth.Enabled && c.Auth.IssuerURL != "" && c.Auth.ClientID != ""
}

// SPIREEnabled returns true when exoskeleton is enabled and SPIRE
// identity registration is enabled.
func (c *Config) SPIREEnabled() bool {
	return c.Enabled && c.SPIRE.Enabled
}

// envBool returns true if the named environment variable is set to a
// truthy value (true, 1, yes).
func envBool(key string) bool {
	v := strings.ToLower(os.Getenv(key))
	return v == "true" || v == "1" || v == "yes"
}

// envDefault returns the value of the named environment variable, or
// the default if the variable is empty.
func envDefault(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}
