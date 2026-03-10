package exoskeleton

import (
	"testing"
)

// TestBuildSecretManifestAllThreeServices verifies that a Secret with
// all three services (postgres, nats, rustfs) contains the expected
// keys from each service plus the identity fields.
func TestBuildSecretManifestAllThreeServices(t *testing.T) {
	creds := map[string]interface{}{
		"tentacular-postgres": &PostgresCreds{
			Host:     "pg.local",
			Port:     "5432",
			Database: "tentacular",
			User:     "tn_ns_wf",
			Password: "pgpass",
			Schema:   "tn_ns_wf",
			Protocol: "postgresql",
		},
		"tentacular-nats": &NATSCreds{
			URL:           "nats://nats.local:4222",
			Token:         "natstoken",
			SubjectPrefix: "tentacular.ns.wf.>",
			Protocol:      "nats",
		},
		"tentacular-rustfs": &RustFSCreds{
			Endpoint:  "http://minio:9000",
			AccessKey: "ak",
			SecretKey: "sk",
			Bucket:    "tentacular",
			Prefix:    "ns/myns/tentacles/mywf/",
			Region:    "us-east-1",
			Protocol:  "s3",
		},
	}

	manifest, err := BuildSecretManifest("myns", "mywf", creds)
	if err != nil {
		t.Fatalf("BuildSecretManifest returned error: %v", err)
	}
	sd := manifest["stringData"].(map[string]interface{})

	// Postgres keys
	pgKeys := []string{
		"tentacular-postgres.host",
		"tentacular-postgres.port",
		"tentacular-postgres.database",
		"tentacular-postgres.user",
		"tentacular-postgres.password",
		"tentacular-postgres.schema",
		"tentacular-postgres.protocol",
	}
	for _, k := range pgKeys {
		if _, ok := sd[k]; !ok {
			t.Errorf("missing postgres key %q", k)
		}
	}

	// NATS keys
	natsKeys := []string{
		"tentacular-nats.url",
		"tentacular-nats.token",
		"tentacular-nats.subject_prefix",
		"tentacular-nats.protocol",
	}
	for _, k := range natsKeys {
		if _, ok := sd[k]; !ok {
			t.Errorf("missing nats key %q", k)
		}
	}

	// RustFS keys
	rustfsKeys := []string{
		"tentacular-rustfs.endpoint",
		"tentacular-rustfs.access_key",
		"tentacular-rustfs.secret_key",
		"tentacular-rustfs.bucket",
		"tentacular-rustfs.prefix",
		"tentacular-rustfs.region",
		"tentacular-rustfs.protocol",
	}
	for _, k := range rustfsKeys {
		if _, ok := sd[k]; !ok {
			t.Errorf("missing rustfs key %q", k)
		}
	}

	// Identity keys
	idKeys := []string{
		"tentacular-identity.principal",
		"tentacular-identity.namespace",
		"tentacular-identity.workflow",
	}
	for _, k := range idKeys {
		if _, ok := sd[k]; !ok {
			t.Errorf("missing identity key %q", k)
		}
	}

	// Verify total key count: 7 pg + 4 nats + 7 rustfs + 3 identity = 21
	if len(sd) != 21 {
		t.Errorf("expected 21 stringData keys, got %d", len(sd))
	}
}

// TestBuildSecretManifestFallbackJSON verifies the JSON fallback for
// an unknown credential type.
func TestBuildSecretManifestFallbackJSON(t *testing.T) {
	creds := map[string]interface{}{
		"tentacular-unknown": map[string]string{"key": "value"},
	}

	manifest, err := BuildSecretManifest("ns", "wf", creds)
	if err != nil {
		t.Fatalf("BuildSecretManifest returned error: %v", err)
	}
	sd := manifest["stringData"].(map[string]interface{})

	val, ok := sd["tentacular-unknown"]
	if !ok {
		t.Fatal("missing fallback JSON key")
	}
	s, ok := val.(string)
	if !ok {
		t.Fatal("fallback value is not a string")
	}
	if s == "" {
		t.Error("fallback JSON value is empty")
	}
}
