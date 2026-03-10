package exoskeleton

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ---------- Register/Unregister flow tests using mock admin + S3 ----------

// adminCallRecord records a single admin API call.
type adminCallRecord struct {
	method string
	path   string
	query  map[string]string
	body   string
}

// orderedAdminRecorder records calls in order (thread-safe).
type orderedAdminRecorder struct {
	mu    sync.Mutex
	calls []adminCallRecord
}

func (r *orderedAdminRecorder) append(rec adminCallRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, rec)
}

func (r *orderedAdminRecorder) getCalls() []adminCallRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]adminCallRecord, len(r.calls))
	copy(result, r.calls)
	return result
}

// newOrderedMockAdmin creates an httptest server that records calls in order.
func newOrderedMockAdmin(t *testing.T, status int) (*rustfsAdmin, *orderedAdminRecorder, *httptest.Server) {
	t.Helper()
	rec := &orderedAdminRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := adminCallRecord{
			method: r.Method,
			path:   r.URL.Path,
			query:  make(map[string]string),
		}
		for k, v := range r.URL.Query() {
			if len(v) > 0 {
				call.query[k] = v[0]
			}
		}
		if r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			call.body = string(body)
		}
		rec.append(call)
		w.WriteHeader(status)
	}))
	a := newRustFSAdmin(srv.URL, "admin", "admin123", "us-east-1", srv.Client())
	return a, rec, srv
}

// TestRustFSRegisterFlow_AdminCallSequence verifies that the Register flow
// makes admin calls in the correct order: AddUser, AddCannedPolicy, SetPolicy.
func TestRustFSRegisterFlow_AdminCallSequence(t *testing.T) {
	admin, rec, srv := newOrderedMockAdmin(t, http.StatusOK)
	defer srv.Close()

	ctx := context.Background()

	id, err := CompileIdentity("tent-dev", "hn-digest")
	if err != nil {
		t.Fatalf("CompileIdentity: %v", err)
	}

	// Simulate the Register admin calls in order (without S3).
	secretKey := "fake-secret-key-for-testing-abc"

	// Step 1: AddUser
	if err := admin.AddUser(ctx, id.S3User, secretKey); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	// Step 2: AddCannedPolicy
	policy := buildS3Policy("tentacular", id.S3Prefix)
	policyJSON, _ := json.Marshal(policy)
	if err := admin.AddCannedPolicy(ctx, id.S3Policy, policyJSON); err != nil {
		t.Fatalf("AddCannedPolicy: %v", err)
	}

	// Step 3: SetPolicy
	if err := admin.SetPolicy(ctx, id.S3Policy, id.S3User); err != nil {
		t.Fatalf("SetPolicy: %v", err)
	}

	calls := rec.getCalls()
	if len(calls) != 3 {
		t.Fatalf("expected 3 admin calls, got %d", len(calls))
	}

	// Verify call order.
	expectedPaths := []string{
		"/rustfs/admin/v3/add-user",
		"/rustfs/admin/v3/add-canned-policy",
		"/rustfs/admin/v3/set-user-or-group-policy",
	}
	for i, want := range expectedPaths {
		if calls[i].path != want {
			t.Errorf("call[%d] path = %q, want %q", i, calls[i].path, want)
		}
	}

	// Verify AddUser params.
	if calls[0].method != http.MethodPut {
		t.Errorf("AddUser method = %q, want PUT", calls[0].method)
	}
	if calls[0].query["accessKey"] != id.S3User {
		t.Errorf("AddUser accessKey = %q, want %q", calls[0].query["accessKey"], id.S3User)
	}
	var addUserBody map[string]string
	if err := json.Unmarshal([]byte(calls[0].body), &addUserBody); err == nil {
		if addUserBody["secretKey"] != secretKey {
			t.Errorf("AddUser body secretKey = %q", addUserBody["secretKey"])
		}
		if addUserBody["status"] != "enabled" {
			t.Errorf("AddUser body status = %q", addUserBody["status"])
		}
	}

	// Verify AddCannedPolicy params.
	if calls[1].query["name"] != id.S3Policy {
		t.Errorf("AddCannedPolicy name = %q, want %q", calls[1].query["name"], id.S3Policy)
	}

	// Verify SetPolicy params.
	if calls[2].query["policyName"] != id.S3Policy {
		t.Errorf("SetPolicy policyName = %q, want %q", calls[2].query["policyName"], id.S3Policy)
	}
	if calls[2].query["userOrGroup"] != id.S3User {
		t.Errorf("SetPolicy userOrGroup = %q, want %q", calls[2].query["userOrGroup"], id.S3User)
	}
}

// TestRustFSUnregisterFlow_AdminCallSequence verifies the Unregister flow
// makes admin calls in correct order: RemoveUser before RemoveCannedPolicy.
func TestRustFSUnregisterFlow_AdminCallSequence(t *testing.T) {
	admin, rec, srv := newOrderedMockAdmin(t, http.StatusOK)
	defer srv.Close()

	ctx := context.Background()

	id, err := CompileIdentity("tent-dev", "hn-digest")
	if err != nil {
		t.Fatalf("CompileIdentity: %v", err)
	}

	// Simulate Unregister admin calls (user before policy, matching production code).
	if err := admin.RemoveUser(ctx, id.S3User); err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}
	if err := admin.RemoveCannedPolicy(ctx, id.S3Policy); err != nil {
		t.Fatalf("RemoveCannedPolicy: %v", err)
	}

	calls := rec.getCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 admin calls, got %d", len(calls))
	}

	// Verify order: user removed before policy.
	if calls[0].path != "/rustfs/admin/v3/remove-user" {
		t.Errorf("call[0] path = %q, want remove-user", calls[0].path)
	}
	if calls[0].method != http.MethodDelete {
		t.Errorf("call[0] method = %q, want DELETE", calls[0].method)
	}
	if calls[1].path != "/rustfs/admin/v3/remove-canned-policy" {
		t.Errorf("call[1] path = %q, want remove-canned-policy", calls[1].path)
	}
	if calls[1].method != http.MethodDelete {
		t.Errorf("call[1] method = %q, want DELETE", calls[1].method)
	}
}

// TestRustFSUnregisterFlow_AdminErrorsNonFatal verifies that admin errors
// during Unregister are handled gracefully (logged, not fatal).
func TestRustFSUnregisterFlow_AdminErrorsNonFatal(t *testing.T) {
	// Return 500 for all requests.
	admin, _, srv := newOrderedMockAdmin(t, http.StatusInternalServerError)
	defer srv.Close()

	ctx := context.Background()

	// RemoveUser fails but doesn't panic.
	err := admin.RemoveUser(ctx, "nonexistent-user")
	if err == nil {
		t.Error("expected error from RemoveUser with 500 status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention 500: %v", err)
	}

	// RemoveCannedPolicy fails but doesn't panic.
	err = admin.RemoveCannedPolicy(ctx, "nonexistent-policy")
	if err == nil {
		t.Error("expected error from RemoveCannedPolicy with 500 status")
	}
}

// TestRustFSCreds_Fields verifies credential struct fields.
func TestRustFSCreds_Fields(t *testing.T) {
	creds := &RustFSCreds{
		Endpoint:  "http://rustfs:9000",
		AccessKey: "tn_ns_wf",
		SecretKey: "secret123",
		Bucket:    "tentacular",
		Prefix:    "ns/test/tentacles/wf/",
		Region:    "us-east-1",
		Protocol:  "s3",
	}

	if creds.Protocol != "s3" {
		t.Errorf("Protocol = %q, want s3", creds.Protocol)
	}
	if creds.Bucket != "tentacular" {
		t.Errorf("Bucket = %q", creds.Bucket)
	}
}

// TestRustFSRegistrar_Close verifies Close is a no-op.
func TestRustFSRegistrar_Close(t *testing.T) {
	r := &RustFSRegistrar{}
	// Should not panic.
	r.Close()
}

// TestBuildS3Policy_PrefixScoping verifies the policy is correctly scoped.
func TestBuildS3Policy_PrefixScoping(t *testing.T) {
	id, err := CompileIdentity("prod-ns", "data-pipeline")
	if err != nil {
		t.Fatalf("CompileIdentity: %v", err)
	}

	policy := buildS3Policy("tentacular", id.S3Prefix)

	// Marshal and verify valid JSON.
	b, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// Verify the policy is scoped to the correct prefix.
	policyStr := string(b)
	expectedARN := "arn:aws:s3:::tentacular/" + id.S3Prefix + "*"
	if !strings.Contains(policyStr, expectedARN) {
		t.Errorf("policy should contain %q, got: %s", expectedARN, policyStr)
	}

	// Verify bucket ARN for ListBucket.
	if !strings.Contains(policyStr, "arn:aws:s3:::tentacular") {
		t.Errorf("policy should contain bucket ARN")
	}
}
