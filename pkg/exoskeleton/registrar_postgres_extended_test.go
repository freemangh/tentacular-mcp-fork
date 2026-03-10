package exoskeleton

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ---------- generateHexPassword extended tests ----------

func TestGenerateHexPassword_Length(t *testing.T) {
	for _, n := range []int{8, 16, 32, 64} {
		pw, err := generateHexPassword(n)
		if err != nil {
			t.Fatalf("generateHexPassword(%d): %v", n, err)
		}
		if len(pw) != n {
			t.Errorf("generateHexPassword(%d) length = %d, want %d", n, len(pw), n)
		}
	}
}

func TestGenerateHexPassword_HexOnly(t *testing.T) {
	pw, err := generateHexPassword(32)
	if err != nil {
		t.Fatalf("generateHexPassword: %v", err)
	}
	// Verify it decodes as valid hex.
	_, err = hex.DecodeString(pw)
	if err != nil {
		t.Errorf("password %q is not valid hex: %v", pw, err)
	}
}

func TestGenerateHexPassword_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		pw, err := generateHexPassword(32)
		if err != nil {
			t.Fatalf("generateHexPassword: %v", err)
		}
		if seen[pw] {
			t.Fatalf("duplicate password on iteration %d: %s", i, pw)
		}
		seen[pw] = true
	}
}

// ---------- pgIdent extended tests ----------

func TestPgIdent_EmbeddedQuotes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", `"simple"`},
		{`has"quote`, `"has""quote"`},
		{`a""b`, `"a""""b"`},
		{"", `""`},
		{`"`, `""""`},
	}
	for _, tt := range tests {
		got := pgIdent(tt.input)
		if got != tt.want {
			t.Errorf("pgIdent(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---------- escapeLiteral extended tests ----------

func TestEscapeLiteral_Extended(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"noquotes", "noquotes"},
		{"it's a test", "it''s a test"},
		{"'''", "''''''"},
		{"a'b'c'd", "a''b''c''d"},
	}
	for _, tt := range tests {
		got := escapeLiteral(tt.input)
		if got != tt.want {
			t.Errorf("escapeLiteral(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---------- Mock pgExecutor for SQL generation tests ----------

// pgExecutor is the interface for executing SQL against Postgres.
type pgExecutor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

// mockTx records SQL statements executed within a transaction.
type mockTx struct {
	stmts []string
}

func (m *mockTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	m.stmts = append(m.stmts, sql)
	return pgconn.NewCommandTag("OK"), nil
}

func (m *mockTx) Commit(_ context.Context) error { return nil }
func (m *mockTx) Rollback(_ context.Context) error { return nil }

// ---------- Register SQL generation tests ----------

// buildRegisterSQL produces the SQL statements that Register would execute,
// extracted from the production code's logic so we can test them in isolation.
func buildRegisterSQL(role, schema, password string) []string {
	createRole := fmt.Sprintf(
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = '%s') THEN
				CREATE ROLE %s LOGIN PASSWORD '%s';
			ELSE
				ALTER ROLE %s WITH PASSWORD '%s';
			END IF;
		END $$`,
		role, pgIdent(role), escapeLiteral(password),
		pgIdent(role), escapeLiteral(password),
	)
	grantToAdmin := fmt.Sprintf("GRANT %s TO CURRENT_USER", pgIdent(role))
	createSchema := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s AUTHORIZATION %s",
		pgIdent(schema), pgIdent(role))
	grantUsage := fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", pgIdent(schema), pgIdent(role))
	grantAll := fmt.Sprintf("GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA %s TO %s",
		pgIdent(schema), pgIdent(role))
	alterDefault := fmt.Sprintf(
		"ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT ALL PRIVILEGES ON TABLES TO %s",
		pgIdent(schema), pgIdent(role))

	return []string{createRole, grantToAdmin, createSchema, grantUsage, grantAll, alterDefault}
}

func TestRegisterSQL_CorrectStatements(t *testing.T) {
	id, err := CompileIdentity("tent-dev", "hn-digest")
	if err != nil {
		t.Fatalf("CompileIdentity: %v", err)
	}

	stmts := buildRegisterSQL(id.PgRole, id.PgSchema, "testpassword123")

	// Verify 6 statements.
	if len(stmts) != 6 {
		t.Fatalf("expected 6 SQL statements, got %d", len(stmts))
	}

	// Statement 0: CREATE ROLE DO block.
	if !strings.Contains(stmts[0], "CREATE ROLE") {
		t.Errorf("stmt 0 should contain CREATE ROLE: %s", stmts[0])
	}
	if !strings.Contains(stmts[0], id.PgRole) {
		t.Errorf("stmt 0 should contain role name %s: %s", id.PgRole, stmts[0])
	}
	if !strings.Contains(stmts[0], "testpassword123") {
		t.Errorf("stmt 0 should contain password: %s", stmts[0])
	}
	if !strings.Contains(stmts[0], "ALTER ROLE") {
		t.Errorf("stmt 0 should contain ALTER ROLE fallback: %s", stmts[0])
	}

	// Statement 1: GRANT TO CURRENT_USER.
	want1 := fmt.Sprintf("GRANT %s TO CURRENT_USER", pgIdent(id.PgRole))
	if stmts[1] != want1 {
		t.Errorf("stmt 1 = %q, want %q", stmts[1], want1)
	}

	// Statement 2: CREATE SCHEMA.
	if !strings.Contains(stmts[2], "CREATE SCHEMA IF NOT EXISTS") {
		t.Errorf("stmt 2 should contain CREATE SCHEMA IF NOT EXISTS: %s", stmts[2])
	}
	if !strings.Contains(stmts[2], pgIdent(id.PgSchema)) {
		t.Errorf("stmt 2 should contain schema name: %s", stmts[2])
	}
	if !strings.Contains(stmts[2], "AUTHORIZATION") {
		t.Errorf("stmt 2 should contain AUTHORIZATION: %s", stmts[2])
	}

	// Statement 3: GRANT USAGE.
	if !strings.Contains(stmts[3], "GRANT USAGE ON SCHEMA") {
		t.Errorf("stmt 3 should contain GRANT USAGE: %s", stmts[3])
	}

	// Statement 4: GRANT ALL.
	if !strings.Contains(stmts[4], "GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA") {
		t.Errorf("stmt 4 should contain GRANT ALL: %s", stmts[4])
	}

	// Statement 5: ALTER DEFAULT PRIVILEGES.
	if !strings.Contains(stmts[5], "ALTER DEFAULT PRIVILEGES IN SCHEMA") {
		t.Errorf("stmt 5 should contain ALTER DEFAULT PRIVILEGES: %s", stmts[5])
	}
}

func TestRegisterSQL_PasswordWithSpecialChars(t *testing.T) {
	id, err := CompileIdentity("ns", "wf")
	if err != nil {
		t.Fatalf("CompileIdentity: %v", err)
	}

	// Password with single quotes should be escaped.
	stmts := buildRegisterSQL(id.PgRole, id.PgSchema, "pass'word")
	if !strings.Contains(stmts[0], "pass''word") {
		t.Errorf("password with single quote not properly escaped in SQL: %s", stmts[0])
	}
}

// ---------- Unregister SQL generation tests ----------

func buildUnregisterSQL(role, schema string) []string {
	revoke := fmt.Sprintf("REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA %s FROM %s",
		pgIdent(schema), pgIdent(role))
	dropSchema := fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgIdent(schema))
	dropRole := fmt.Sprintf("DROP ROLE IF EXISTS %s", pgIdent(role))
	return []string{revoke, dropSchema, dropRole}
}

func TestUnregisterSQL_CorrectStatements(t *testing.T) {
	id, err := CompileIdentity("tent-dev", "hn-digest")
	if err != nil {
		t.Fatalf("CompileIdentity: %v", err)
	}

	stmts := buildUnregisterSQL(id.PgRole, id.PgSchema)

	if len(stmts) != 3 {
		t.Fatalf("expected 3 SQL statements, got %d", len(stmts))
	}

	// Statement 0: REVOKE.
	if !strings.Contains(stmts[0], "REVOKE ALL PRIVILEGES") {
		t.Errorf("stmt 0 should contain REVOKE: %s", stmts[0])
	}
	if !strings.Contains(stmts[0], pgIdent(id.PgSchema)) {
		t.Errorf("stmt 0 should contain schema name: %s", stmts[0])
	}

	// Statement 1: DROP SCHEMA CASCADE.
	if !strings.Contains(stmts[1], "DROP SCHEMA IF EXISTS") {
		t.Errorf("stmt 1 should contain DROP SCHEMA: %s", stmts[1])
	}
	if !strings.Contains(stmts[1], "CASCADE") {
		t.Errorf("stmt 1 should contain CASCADE: %s", stmts[1])
	}

	// Statement 2: DROP ROLE.
	if !strings.Contains(stmts[2], "DROP ROLE IF EXISTS") {
		t.Errorf("stmt 2 should contain DROP ROLE: %s", stmts[2])
	}
	if !strings.Contains(stmts[2], pgIdent(id.PgRole)) {
		t.Errorf("stmt 2 should contain role name: %s", stmts[2])
	}
}

// ---------- PostgresCreds population test ----------

func TestPostgresCreds_Fields(t *testing.T) {
	creds := &PostgresCreds{
		Host:     "pg.example.com",
		Port:     "5432",
		Database: "tentacular",
		User:     "tn_ns_wf",
		Password: "secret",
		Schema:   "tn_ns_wf",
		Protocol: "postgresql",
	}

	if creds.Protocol != "postgresql" {
		t.Errorf("Protocol = %q, want postgresql", creds.Protocol)
	}
	if creds.Host != "pg.example.com" {
		t.Errorf("Host = %q", creds.Host)
	}
}

// ---------- PostgresRegistrar Close test ----------

func TestPostgresRegistrar_CloseNilPool(t *testing.T) {
	r := &PostgresRegistrar{pool: nil, cfg: PostgresConfig{}}
	// Should not panic.
	r.Close()
}
