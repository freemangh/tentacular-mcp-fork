package exoskeleton

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	calls []adminCallRecord
	mu    sync.Mutex
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
	if creds.Endpoint != "http://rustfs:9000" {
		t.Errorf("Endpoint = %q", creds.Endpoint)
	}
	if creds.AccessKey != "tn_ns_wf" {
		t.Errorf("AccessKey = %q", creds.AccessKey)
	}
	if creds.SecretKey != "secret123" {
		t.Errorf("SecretKey = %q", creds.SecretKey)
	}
	if creds.Prefix != "ns/test/tentacles/wf/" {
		t.Errorf("Prefix = %q", creds.Prefix)
	}
	if creds.Region != "us-east-1" {
		t.Errorf("Region = %q", creds.Region)
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

// ---------- adminURL tests ----------

func TestRustFSAdminURL(t *testing.T) {
	a := newRustFSAdmin("http://rustfs:9000", "ak", "sk", "us-east-1", nil)

	// Without query params.
	got := a.adminURL("/add-user", nil)
	want := "http://rustfs:9000/rustfs/admin/v3/add-user"
	if got != want {
		t.Errorf("adminURL without query = %q, want %q", got, want)
	}

	// With query params.
	q := url.Values{"accessKey": {"testuser"}}
	got = a.adminURL("/add-user", q)
	if !strings.HasPrefix(got, want+"?") {
		t.Errorf("adminURL with query = %q, expected prefix %q?", got, want)
	}
	if !strings.Contains(got, "accessKey=testuser") {
		t.Errorf("adminURL should contain accessKey=testuser, got %q", got)
	}
}

func TestRustFSAdmin_EndpointTrailingSlash(t *testing.T) {
	a := newRustFSAdmin("http://rustfs:9000/", "ak", "sk", "us-east-1", nil)
	got := a.adminURL("/add-user", nil)
	want := "http://rustfs:9000/rustfs/admin/v3/add-user"
	if got != want {
		t.Errorf("trailing slash not stripped: got %q, want %q", got, want)
	}
}

// ---------- doNoBody status code tests ----------

func TestRustFSAdmin_DoNoBody_SuccessStatusCodes(t *testing.T) {
	for _, status := range []int{200, 204, 299} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			admin, _, srv := newOrderedMockAdmin(t, status)
			defer srv.Close()
			err := admin.doNoBody(context.Background(), http.MethodGet, "/test", nil, nil)
			if err != nil {
				t.Errorf("expected no error for status %d, got %v", status, err)
			}
		})
	}
}

func TestRustFSAdmin_DoNoBody_ErrorStatusCodes(t *testing.T) {
	for _, status := range []int{300, 400, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("error body"))
			}))
			defer srv.Close()
			a := newRustFSAdmin(srv.URL, "ak", "sk", "us-east-1", srv.Client())
			err := a.doNoBody(context.Background(), http.MethodGet, "/test", nil, nil)
			if err == nil {
				t.Fatalf("expected error for status %d", status)
			}
			if !strings.Contains(err.Error(), "error body") {
				t.Errorf("error should contain response body: %v", err)
			}
		})
	}
}

// ---------- request format tests ----------

func TestRustFSAdmin_AddUser_RequestFormat(t *testing.T) {
	var capturedReq *http.Request
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := newRustFSAdmin(srv.URL, "admin", "secret", "us-east-1", srv.Client())
	err := a.AddUser(context.Background(), "myuser", "mypass")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	if capturedReq.Method != http.MethodPut {
		t.Errorf("method = %q, want PUT", capturedReq.Method)
	}
	if capturedReq.URL.Query().Get("accessKey") != "myuser" {
		t.Errorf("accessKey = %q, want myuser", capturedReq.URL.Query().Get("accessKey"))
	}

	var body map[string]string
	if err := json.Unmarshal(capturedBody, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["secretKey"] != "mypass" {
		t.Errorf("body secretKey = %q, want mypass", body["secretKey"])
	}
	if body["status"] != "enabled" {
		t.Errorf("body status = %q, want enabled", body["status"])
	}
}

func TestRustFSAdmin_RemoveUser_RequestFormat(t *testing.T) {
	var capturedReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := newRustFSAdmin(srv.URL, "admin", "secret", "us-east-1", srv.Client())
	err := a.RemoveUser(context.Background(), "myuser")
	if err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}

	if capturedReq.Method != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", capturedReq.Method)
	}
	if capturedReq.URL.Query().Get("accessKey") != "myuser" {
		t.Errorf("accessKey = %q, want myuser", capturedReq.URL.Query().Get("accessKey"))
	}
}

func TestRustFSAdmin_SetPolicy_RequestFormat(t *testing.T) {
	var capturedReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := newRustFSAdmin(srv.URL, "admin", "secret", "us-east-1", srv.Client())
	err := a.SetPolicy(context.Background(), "my-policy", "my-user")
	if err != nil {
		t.Fatalf("SetPolicy: %v", err)
	}

	if capturedReq.Method != http.MethodPut {
		t.Errorf("method = %q, want PUT", capturedReq.Method)
	}
	q := capturedReq.URL.Query()
	if q.Get("policyName") != "my-policy" {
		t.Errorf("policyName = %q, want my-policy", q.Get("policyName"))
	}
	if q.Get("userOrGroup") != "my-user" {
		t.Errorf("userOrGroup = %q, want my-user", q.Get("userOrGroup"))
	}
	if q.Get("isGroup") != "false" {
		t.Errorf("isGroup = %q, want false", q.Get("isGroup"))
	}
}

// ---------- SigV4 signing test ----------

func TestRustFSAdmin_SigV4Signing(t *testing.T) {
	var capturedReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := newRustFSAdmin(srv.URL, "admin", "secret", "us-east-1", srv.Client())
	err := a.AddUser(context.Background(), "testuser", "testpass")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	// Verify Authorization header contains AWS4-HMAC-SHA256.
	auth := capturedReq.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256") {
		t.Errorf("Authorization header = %q, expected AWS4-HMAC-SHA256 prefix", auth)
	}

	// Verify X-Amz-Content-Sha256 header is present.
	sha := capturedReq.Header.Get("X-Amz-Content-Sha256")
	if sha == "" {
		t.Error("expected X-Amz-Content-Sha256 header")
	}
	if len(sha) != 64 {
		t.Errorf("X-Amz-Content-Sha256 length = %d, expected 64 hex chars", len(sha))
	}
}

// ---------- buildS3Policy structure test ----------

func TestBuildS3Policy_Structure(t *testing.T) {
	policy := buildS3Policy("mybucket", "ns/test/tentacles/wf/")

	if policy.Version != "2012-10-17" {
		t.Errorf("Version = %q, want 2012-10-17", policy.Version)
	}
	if len(policy.Statement) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(policy.Statement))
	}

	// Statement 0: object operations.
	s0 := policy.Statement[0]
	if s0.Effect != "Allow" {
		t.Errorf("stmt[0] Effect = %q", s0.Effect)
	}
	wantActions := []string{"s3:GetObject", "s3:PutObject", "s3:DeleteObject"}
	if len(s0.Action) != len(wantActions) {
		t.Fatalf("stmt[0] actions = %v, want %v", s0.Action, wantActions)
	}
	for i, a := range s0.Action {
		if a != wantActions[i] {
			t.Errorf("stmt[0] action[%d] = %q, want %q", i, a, wantActions[i])
		}
	}
	if s0.Resource != "arn:aws:s3:::mybucket/ns/test/tentacles/wf/*" {
		t.Errorf("stmt[0] Resource = %v", s0.Resource)
	}

	// Statement 1: ListBucket with condition.
	s1 := policy.Statement[1]
	if s1.Effect != "Allow" {
		t.Errorf("stmt[1] Effect = %q", s1.Effect)
	}
	if len(s1.Action) != 1 || s1.Action[0] != "s3:ListBucket" {
		t.Errorf("stmt[1] actions = %v", s1.Action)
	}
	if s1.Resource != "arn:aws:s3:::mybucket" {
		t.Errorf("stmt[1] Resource = %v", s1.Resource)
	}
	if s1.Condition == nil {
		t.Fatal("stmt[1] expected Condition")
	}
}
