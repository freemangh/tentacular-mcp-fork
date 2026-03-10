package exoskeleton

import (
	"fmt"
	"strings"
	"testing"
)

func TestCompileIdentity(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		workflow  string
		wantPg    string
		wantPrinc string
		wantNATS  string
		wantS3    string
	}{
		{
			name:      "simple",
			namespace: "tent-dev",
			workflow:  "hn-digest",
			wantPg:    "tn_tent_dev_hn_digest",
			wantPrinc: "spiffe://tentacular/ns/tent-dev/tentacles/hn-digest",
			wantNATS:  "tentacle.tent-dev.hn-digest",
			wantS3:    "ns/tent-dev/tentacles/hn-digest/",
		},
		{
			name:      "no hyphens",
			namespace: "prod",
			workflow:  "myapp",
			wantPg:    "tn_prod_myapp",
			wantPrinc: "spiffe://tentacular/ns/prod/tentacles/myapp",
			wantNATS:  "tentacle.prod.myapp",
			wantS3:    "ns/prod/tentacles/myapp/",
		},
		{
			name:      "multiple hyphens",
			namespace: "my-long-ns",
			workflow:  "my-long-wf-name",
			wantPg:    "tn_my_long_ns_my_long_wf_name",
			wantPrinc: "spiffe://tentacular/ns/my-long-ns/tentacles/my-long-wf-name",
			wantNATS:  "tentacle.my-long-ns.my-long-wf-name",
			wantS3:    "ns/my-long-ns/tentacles/my-long-wf-name/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := CompileIdentity(tt.namespace, tt.workflow)
			if err != nil {
				t.Fatalf("CompileIdentity returned error: %v", err)
			}

			if id.PgRole != tt.wantPg {
				t.Errorf("PgRole = %q, want %q", id.PgRole, tt.wantPg)
			}
			if id.PgSchema != tt.wantPg {
				t.Errorf("PgSchema = %q, want %q", id.PgSchema, tt.wantPg)
			}
			if id.Principal != tt.wantPrinc {
				t.Errorf("Principal = %q, want %q", id.Principal, tt.wantPrinc)
			}
			if id.NATSUser != tt.wantNATS {
				t.Errorf("NATSUser = %q, want %q", id.NATSUser, tt.wantNATS)
			}
			if id.S3Prefix != tt.wantS3 {
				t.Errorf("S3Prefix = %q, want %q", id.S3Prefix, tt.wantS3)
			}
			if id.S3User != tt.wantPg {
				t.Errorf("S3User = %q, want PgRole %q", id.S3User, tt.wantPg)
			}
			wantPrefix := fmt.Sprintf("tentacular.%s.%s.>", tt.namespace, tt.workflow)
			if id.NATSPrefix != wantPrefix {
				t.Errorf("NATSPrefix = %q, want %q", id.NATSPrefix, wantPrefix)
			}
		})
	}
}

func TestCompileIdentityLongNames(t *testing.T) {
	// Generate names that would exceed 63 chars for the Postgres identifier
	longNS := strings.Repeat("a", 30)
	longWF := strings.Repeat("b", 30)
	id, err := CompileIdentity(longNS, longWF)
	if err != nil {
		t.Fatalf("CompileIdentity returned error: %v", err)
	}

	if len(id.PgRole) > maxPgIdentLen {
		t.Errorf("PgRole length %d exceeds max %d: %q", len(id.PgRole), maxPgIdentLen, id.PgRole)
	}
	if len(id.PgSchema) > maxPgIdentLen {
		t.Errorf("PgSchema length %d exceeds max %d", len(id.PgSchema), maxPgIdentLen)
	}
	if len(id.S3Policy) > maxPgIdentLen {
		t.Errorf("S3Policy length %d exceeds max %d", len(id.S3Policy), maxPgIdentLen)
	}

	// Verify determinism
	id2, err := CompileIdentity(longNS, longWF)
	if err != nil {
		t.Fatalf("CompileIdentity returned error: %v", err)
	}
	if id.PgRole != id2.PgRole {
		t.Errorf("PgRole not deterministic: %q != %q", id.PgRole, id2.PgRole)
	}
}

func TestCompileIdentityDeterminism(t *testing.T) {
	a, err := CompileIdentity("tent-dev", "hn-digest")
	if err != nil {
		t.Fatalf("CompileIdentity returned error: %v", err)
	}
	b, err := CompileIdentity("tent-dev", "hn-digest")
	if err != nil {
		t.Fatalf("CompileIdentity returned error: %v", err)
	}

	if a != b {
		t.Errorf("CompileIdentity is not deterministic")
	}
}

func TestCompileIdentityEmptyNamespace(t *testing.T) {
	_, err := CompileIdentity("", "workflow")
	if err != ErrEmptyNamespace {
		t.Errorf("expected ErrEmptyNamespace, got %v", err)
	}
}

func TestCompileIdentityEmptyWorkflow(t *testing.T) {
	_, err := CompileIdentity("namespace", "")
	if err != ErrEmptyWorkflow {
		t.Errorf("expected ErrEmptyWorkflow, got %v", err)
	}
}

func TestCompileIdentityBothEmpty(t *testing.T) {
	_, err := CompileIdentity("", "")
	if err == nil {
		t.Error("expected error for empty namespace and workflow")
	}
}
