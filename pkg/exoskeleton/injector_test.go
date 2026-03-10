package exoskeleton

import (
	"testing"
)

func TestBuildSecretManifest(t *testing.T) {
	creds := map[string]interface{}{
		"tentacular-postgres": &PostgresCreds{
			Host:     "pg.local",
			Port:     "5432",
			Database: "tentacular",
			User:     "tn_tent_dev_myapp",
			Password: "secret123",
			Schema:   "tn_tent_dev_myapp",
			Protocol: "postgresql",
		},
		"tentacular-nats": &NATSCreds{
			URL:           "nats://nats.local:4222",
			Token:         "tok",
			SubjectPrefix: "tentacular.tent-dev.myapp.>",
			Protocol:      "nats",
		},
	}

	manifest, err := BuildSecretManifest("tent-dev", "myapp", creds)
	if err != nil {
		t.Fatalf("BuildSecretManifest returned error: %v", err)
	}

	// Check top-level structure
	if manifest["apiVersion"] != "v1" {
		t.Errorf("apiVersion = %v, want v1", manifest["apiVersion"])
	}
	if manifest["kind"] != "Secret" {
		t.Errorf("kind = %v, want Secret", manifest["kind"])
	}

	// Check metadata
	meta, ok := manifest["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("metadata is not a map")
	}
	if meta["name"] != "tentacular-exoskeleton-myapp" {
		t.Errorf("name = %v, want tentacular-exoskeleton-myapp", meta["name"])
	}
	if meta["namespace"] != "tent-dev" {
		t.Errorf("namespace = %v, want tent-dev", meta["namespace"])
	}

	labels, ok := meta["labels"].(map[string]interface{})
	if !ok {
		t.Fatal("labels is not a map")
	}
	if labels["tentacular.io/release"] != "myapp" {
		t.Errorf("release label = %v, want myapp", labels["tentacular.io/release"])
	}
	if labels["tentacular.io/exoskeleton"] != "true" {
		t.Errorf("exoskeleton label = %v, want true", labels["tentacular.io/exoskeleton"])
	}

	// Check stringData keys
	sd, ok := manifest["stringData"].(map[string]interface{})
	if !ok {
		t.Fatal("stringData is not a map")
	}

	expectedKeys := []string{
		"tentacular-postgres.host",
		"tentacular-postgres.port",
		"tentacular-postgres.database",
		"tentacular-postgres.user",
		"tentacular-postgres.password",
		"tentacular-postgres.schema",
		"tentacular-postgres.protocol",
		"tentacular-nats.url",
		"tentacular-nats.token",
		"tentacular-nats.subject_prefix",
		"tentacular-nats.protocol",
		"tentacular-identity.principal",
		"tentacular-identity.namespace",
		"tentacular-identity.workflow",
	}
	for _, k := range expectedKeys {
		if _, found := sd[k]; !found {
			t.Errorf("missing key %q in stringData", k)
		}
	}

	// Verify specific values
	if sd["tentacular-postgres.host"] != "pg.local" {
		t.Errorf("postgres host = %v, want pg.local", sd["tentacular-postgres.host"])
	}
	if sd["tentacular-identity.principal"] != "spiffe://tentacular/ns/tent-dev/tentacles/myapp" {
		t.Errorf("principal = %v", sd["tentacular-identity.principal"])
	}
}

func TestBuildSecretManifestRustFS(t *testing.T) {
	creds := map[string]interface{}{
		"tentacular-rustfs": &RustFSCreds{
			Endpoint:  "http://minio:9000",
			AccessKey: "ak123",
			SecretKey: "sk456",
			Bucket:    "tentacular",
			Prefix:    "ns/tent-dev/tentacles/myapp/",
			Region:    "us-east-1",
			Protocol:  "s3",
		},
	}

	manifest, err := BuildSecretManifest("tent-dev", "myapp", creds)
	if err != nil {
		t.Fatalf("BuildSecretManifest returned error: %v", err)
	}
	sd := manifest["stringData"].(map[string]interface{})

	if sd["tentacular-rustfs.endpoint"] != "http://minio:9000" {
		t.Errorf("endpoint = %v", sd["tentacular-rustfs.endpoint"])
	}
	if sd["tentacular-rustfs.bucket"] != "tentacular" {
		t.Errorf("bucket = %v", sd["tentacular-rustfs.bucket"])
	}
	if sd["tentacular-rustfs.prefix"] != "ns/tent-dev/tentacles/myapp/" {
		t.Errorf("prefix = %v", sd["tentacular-rustfs.prefix"])
	}
}

func TestBuildSecretManifestEmpty(t *testing.T) {
	manifest, err := BuildSecretManifest("tent-dev", "myapp", map[string]interface{}{})
	if err != nil {
		t.Fatalf("BuildSecretManifest returned error: %v", err)
	}
	sd := manifest["stringData"].(map[string]interface{})

	// Should still have identity fields
	if sd["tentacular-identity.namespace"] != "tent-dev" {
		t.Errorf("namespace = %v, want tent-dev", sd["tentacular-identity.namespace"])
	}
}
