package exoskeleton

import (
	"testing"
)

func TestPostgresCreds(t *testing.T) {
	id := CompileIdentity("tent-dev", "hn-digest")

	// Verify the expected role and schema names.
	if id.PgRole != "tn_tent_dev_hn_digest" {
		t.Errorf("PgRole = %q, want tn_tent_dev_hn_digest", id.PgRole)
	}
	if id.PgSchema != "tn_tent_dev_hn_digest" {
		t.Errorf("PgSchema = %q, want tn_tent_dev_hn_digest", id.PgSchema)
	}
}

func TestPgIdent(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"tn_test", `"tn_test"`},
		{"role_with_underscores", `"role_with_underscores"`},
	}
	for _, tt := range tests {
		got := pgIdent(tt.input)
		if got != tt.want {
			t.Errorf("pgIdent(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEscapeLiteral(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"it's", "it''s"},
		{"a'b'c", "a''b''c"},
	}
	for _, tt := range tests {
		got := escapeLiteral(tt.input)
		if got != tt.want {
			t.Errorf("escapeLiteral(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGenerateHexPassword(t *testing.T) {
	pw, err := generateHexPassword(32)
	if err != nil {
		t.Fatalf("generateHexPassword: %v", err)
	}
	if len(pw) != 32 {
		t.Errorf("password length = %d, want 32", len(pw))
	}

	// Should be unique (probabilistic)
	pw2, _ := generateHexPassword(32)
	if pw == pw2 {
		t.Error("two generated passwords are identical")
	}
}
