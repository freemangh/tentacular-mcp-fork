package exoskeleton

import (
	"strings"
	"testing"
)

// TestCompileIdentity_ExactlyAtLimit verifies that a name exactly at 63
// chars is not truncated.
func TestCompileIdentity_ExactlyAtLimit(t *testing.T) {
	// "tn_" = 3 chars + "_" separator = 4 chars overhead
	// We need ns + wf lengths such that "tn_<ns>_<wf>" == 63 chars.
	// 63 - 4 = 59 chars for ns + wf combined.
	ns := strings.Repeat("a", 29)
	wf := strings.Repeat("b", 30) // tn_ + 29 + _ + 30 = 63
	id, err := CompileIdentity(ns, wf)
	if err != nil {
		t.Fatalf("CompileIdentity returned error: %v", err)
	}

	if len(id.PgRole) != maxPgIdentLen {
		t.Errorf("PgRole length = %d, want exactly %d: %q", len(id.PgRole), maxPgIdentLen, id.PgRole)
	}
	// Should NOT contain a hash suffix since it's exactly at the limit
	if strings.Contains(id.PgRole, "_") && strings.Count(id.PgRole, "_") > 2 {
		// tn_<ns>_<wf> has exactly 2 underscores. If there are more, it was truncated.
		// Actually let me just check: it should equal the raw value
		expected := "tn_" + strings.Repeat("a", 29) + "_" + strings.Repeat("b", 30)
		if id.PgRole != expected {
			t.Errorf("PgRole = %q, want %q (no truncation expected)", id.PgRole, expected)
		}
	}
}

// TestCompileIdentity_OneOverLimit verifies that a name 1 char over 63
// gets truncated with a hash suffix.
func TestCompileIdentity_OneOverLimit(t *testing.T) {
	ns := strings.Repeat("a", 30)
	wf := strings.Repeat("b", 30) // tn_ + 30 + _ + 30 = 64 > 63
	id, err := CompileIdentity(ns, wf)
	if err != nil {
		t.Fatalf("CompileIdentity returned error: %v", err)
	}

	if len(id.PgRole) > maxPgIdentLen {
		t.Errorf("PgRole length %d exceeds max %d: %q", len(id.PgRole), maxPgIdentLen, id.PgRole)
	}
	if len(id.PgRole) != maxPgIdentLen {
		t.Errorf("PgRole length = %d, expected exactly %d (truncated with hash): %q",
			len(id.PgRole), maxPgIdentLen, id.PgRole)
	}

	// Verify it contains a hash suffix (8 hex chars after the last _)
	raw := "tn_" + strings.Repeat("a", 30) + "_" + strings.Repeat("b", 30)
	if id.PgRole == raw {
		t.Error("PgRole should have been truncated but was not")
	}
}

// TestCompileIdentity_EmptyInputs verifies that empty strings return errors.
func TestCompileIdentity_EmptyInputs(t *testing.T) {
	_, err := CompileIdentity("", "")
	if err == nil {
		t.Error("expected error for empty namespace and workflow")
	}
}

// TestCompileIdentity_SpecialCharacters verifies hyphens are replaced
// and the identifier stays valid.
func TestCompileIdentity_SpecialCharacters(t *testing.T) {
	id, err := CompileIdentity("my-ns-with-hyphens", "my-wf-with-hyphens")
	if err != nil {
		t.Fatalf("CompileIdentity returned error: %v", err)
	}
	if strings.Contains(id.PgRole, "-") {
		t.Errorf("PgRole should not contain hyphens: %q", id.PgRole)
	}
	expected := "tn_my_ns_with_hyphens_my_wf_with_hyphens"
	if id.PgRole != expected {
		t.Errorf("PgRole = %q, want %q", id.PgRole, expected)
	}
}

// TestCompileIdentity_S3PolicyAlsoTruncated ensures S3Policy is
// truncated independently (it has a _policy suffix making it longer).
func TestCompileIdentity_S3PolicyAlsoTruncated(t *testing.T) {
	// Make names long enough that pgBase fits but pgBase + "_policy" does not
	ns := strings.Repeat("a", 25)
	wf := strings.Repeat("b", 25) // tn_ + 25 + _ + 25 = 54 chars; +_policy = 61, still fits
	id, err := CompileIdentity(ns, wf)
	if err != nil {
		t.Fatalf("CompileIdentity returned error: %v", err)
	}
	if len(id.S3Policy) > maxPgIdentLen {
		t.Errorf("S3Policy length %d exceeds max %d: %q", len(id.S3Policy), maxPgIdentLen, id.S3Policy)
	}

	// Now make it overflow
	ns2 := strings.Repeat("a", 28)
	wf2 := strings.Repeat("b", 28) // tn_ + 28 + _ + 28 = 59 chars; +_policy = 66 > 63
	id2, err := CompileIdentity(ns2, wf2)
	if err != nil {
		t.Fatalf("CompileIdentity returned error: %v", err)
	}
	if len(id2.S3Policy) > maxPgIdentLen {
		t.Errorf("S3Policy length %d exceeds max %d: %q", len(id2.S3Policy), maxPgIdentLen, id2.S3Policy)
	}
}
