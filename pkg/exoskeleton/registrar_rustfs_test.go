package exoskeleton

import (
	"encoding/json"
	"testing"
)

func TestBuildS3Policy(t *testing.T) {
	policy := buildS3Policy("tentacular", "ns/tent-dev/tentacles/myapp/")

	if policy.Version != "2012-10-17" {
		t.Errorf("Version = %q, want 2012-10-17", policy.Version)
	}
	if len(policy.Statement) != 2 {
		t.Fatalf("got %d statements, want 2", len(policy.Statement))
	}

	// Statement 0: object operations
	stmt0 := policy.Statement[0]
	if stmt0.Effect != "Allow" {
		t.Errorf("stmt0 Effect = %q", stmt0.Effect)
	}
	if len(stmt0.Action) != 3 {
		t.Errorf("stmt0 actions = %d, want 3", len(stmt0.Action))
	}
	resource, ok := stmt0.Resource.(string)
	if !ok {
		t.Fatal("stmt0 Resource is not string")
	}
	if resource != "arn:aws:s3:::tentacular/ns/tent-dev/tentacles/myapp/*" {
		t.Errorf("stmt0 Resource = %q", resource)
	}

	// Statement 1: list bucket with condition
	stmt1 := policy.Statement[1]
	if stmt1.Effect != "Allow" {
		t.Errorf("stmt1 Effect = %q", stmt1.Effect)
	}
	listResource, ok := stmt1.Resource.(string)
	if !ok {
		t.Fatal("stmt1 Resource is not string")
	}
	if listResource != "arn:aws:s3:::tentacular" {
		t.Errorf("stmt1 Resource = %q", listResource)
	}
	if stmt1.Condition == nil {
		t.Fatal("stmt1 Condition is nil")
	}

	// Verify it marshals to valid JSON
	b, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if len(b) == 0 {
		t.Error("empty JSON")
	}
}

func TestRustFSIdentityMapping(t *testing.T) {
	id := CompileIdentity("tent-dev", "hn-digest")

	if id.S3Prefix != "ns/tent-dev/tentacles/hn-digest/" {
		t.Errorf("S3Prefix = %q", id.S3Prefix)
	}
	if id.S3User != "tn_tent_dev_hn_digest" {
		t.Errorf("S3User = %q", id.S3User)
	}
	if id.S3Policy != "tn_tent_dev_hn_digest_policy" {
		t.Errorf("S3Policy = %q", id.S3Policy)
	}
}
