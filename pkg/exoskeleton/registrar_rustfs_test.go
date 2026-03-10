package exoskeleton

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// ---------- adminURL construction tests ----------

func TestAdminURLBasic(t *testing.T) {
	a := newRustFSAdmin("http://localhost:9000", "ak", "sk", "us-east-1", nil)
	got := a.adminURL("/add-user", url.Values{"accessKey": {"bob"}})
	want := "http://localhost:9000/rustfs/admin/v3/add-user?accessKey=bob"
	if got != want {
		t.Errorf("adminURL = %q, want %q", got, want)
	}
}

func TestAdminURLTrailingSlash(t *testing.T) {
	a := newRustFSAdmin("http://localhost:9000/", "ak", "sk", "us-east-1", nil)
	got := a.adminURL("/list-users", nil)
	want := "http://localhost:9000/rustfs/admin/v3/list-users"
	if got != want {
		t.Errorf("adminURL = %q, want %q", got, want)
	}
}

func TestAdminURLNoQuery(t *testing.T) {
	a := newRustFSAdmin("https://s3.example.com", "ak", "sk", "us-east-1", nil)
	got := a.adminURL("/info", nil)
	want := "https://s3.example.com/rustfs/admin/v3/info"
	if got != want {
		t.Errorf("adminURL = %q, want %q", got, want)
	}
}

func TestAdminURLMultiQuery(t *testing.T) {
	a := newRustFSAdmin("http://localhost:9000", "ak", "sk", "us-east-1", nil)
	q := url.Values{
		"policyName":  {"mypol"},
		"userOrGroup": {"bob"},
		"isGroup":     {"false"},
	}
	got := a.adminURL("/set-user-or-group-policy", q)
	// Verify the base path is correct; query order may vary.
	if !strings.HasPrefix(got, "http://localhost:9000/rustfs/admin/v3/set-user-or-group-policy?") {
		t.Errorf("adminURL prefix mismatch: %q", got)
	}
	for _, key := range []string{"policyName=mypol", "userOrGroup=bob", "isGroup=false"} {
		if !strings.Contains(got, key) {
			t.Errorf("adminURL missing %q in %q", key, got)
		}
	}
}

// ---------- Request body format tests ----------

func TestAddUserRequestBody(t *testing.T) {
	type addUserReq struct {
		SecretKey string `json:"secretKey"`
		Status    string `json:"status"`
	}
	body, err := json.Marshal(addUserReq{SecretKey: "s3cret", Status: "enabled"})
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]string
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["secretKey"] != "s3cret" {
		t.Errorf("secretKey = %q", parsed["secretKey"])
	}
	if parsed["status"] != "enabled" {
		t.Errorf("status = %q", parsed["status"])
	}
}

// ---------- Mock HTTP server tests ----------

// adminRecorder captures request details from the mock admin server.
type adminRecorder struct {
	method string
	path   string
	query  url.Values
	body   []byte
}

// newMockAdmin starts an httptest.Server that records each request and
// returns the given status code. It returns the admin client wired to
// the test server and a pointer to the most recent recorded request.
func newMockAdmin(t *testing.T, status int) (*rustfsAdmin, *adminRecorder, *httptest.Server) {
	t.Helper()
	rec := &adminRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.query = r.URL.Query()
		if r.Body != nil {
			rec.body, _ = io.ReadAll(r.Body)
		}
		w.WriteHeader(status)
	}))
	a := newRustFSAdmin(srv.URL, "admin", "admin123", "us-east-1", srv.Client())
	return a, rec, srv
}

func TestAddUser(t *testing.T) {
	a, rec, srv := newMockAdmin(t, http.StatusOK)
	defer srv.Close()

	err := a.AddUser(context.Background(), "bob", "bobsecret")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if rec.method != http.MethodPut {
		t.Errorf("method = %q, want PUT", rec.method)
	}
	if rec.path != "/rustfs/admin/v3/add-user" {
		t.Errorf("path = %q", rec.path)
	}
	if rec.query.Get("accessKey") != "bob" {
		t.Errorf("accessKey = %q", rec.query.Get("accessKey"))
	}
	var body map[string]string
	if err := json.Unmarshal(rec.body, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["secretKey"] != "bobsecret" {
		t.Errorf("body secretKey = %q", body["secretKey"])
	}
	if body["status"] != "enabled" {
		t.Errorf("body status = %q", body["status"])
	}
}

func TestRemoveUser(t *testing.T) {
	a, rec, srv := newMockAdmin(t, http.StatusOK)
	defer srv.Close()

	err := a.RemoveUser(context.Background(), "bob")
	if err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}
	if rec.method != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", rec.method)
	}
	if rec.path != "/rustfs/admin/v3/remove-user" {
		t.Errorf("path = %q", rec.path)
	}
	if rec.query.Get("accessKey") != "bob" {
		t.Errorf("accessKey = %q", rec.query.Get("accessKey"))
	}
}

func TestAddCannedPolicy(t *testing.T) {
	a, rec, srv := newMockAdmin(t, http.StatusOK)
	defer srv.Close()

	policyDoc := []byte(`{"Version":"2012-10-17","Statement":[]}`)
	err := a.AddCannedPolicy(context.Background(), "mypol", policyDoc)
	if err != nil {
		t.Fatalf("AddCannedPolicy: %v", err)
	}
	if rec.method != http.MethodPut {
		t.Errorf("method = %q, want PUT", rec.method)
	}
	if rec.path != "/rustfs/admin/v3/add-canned-policy" {
		t.Errorf("path = %q", rec.path)
	}
	if rec.query.Get("name") != "mypol" {
		t.Errorf("name = %q", rec.query.Get("name"))
	}
	if string(rec.body) != string(policyDoc) {
		t.Errorf("body = %q", string(rec.body))
	}
}

func TestSetPolicy(t *testing.T) {
	a, rec, srv := newMockAdmin(t, http.StatusOK)
	defer srv.Close()

	err := a.SetPolicy(context.Background(), "mypol", "bob")
	if err != nil {
		t.Fatalf("SetPolicy: %v", err)
	}
	if rec.method != http.MethodPut {
		t.Errorf("method = %q, want PUT", rec.method)
	}
	if rec.path != "/rustfs/admin/v3/set-user-or-group-policy" {
		t.Errorf("path = %q", rec.path)
	}
	if rec.query.Get("policyName") != "mypol" {
		t.Errorf("policyName = %q", rec.query.Get("policyName"))
	}
	if rec.query.Get("userOrGroup") != "bob" {
		t.Errorf("userOrGroup = %q", rec.query.Get("userOrGroup"))
	}
	if rec.query.Get("isGroup") != "false" {
		t.Errorf("isGroup = %q", rec.query.Get("isGroup"))
	}
}

func TestRemoveCannedPolicy(t *testing.T) {
	a, rec, srv := newMockAdmin(t, http.StatusOK)
	defer srv.Close()

	err := a.RemoveCannedPolicy(context.Background(), "mypol")
	if err != nil {
		t.Fatalf("RemoveCannedPolicy: %v", err)
	}
	if rec.method != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", rec.method)
	}
	if rec.path != "/rustfs/admin/v3/remove-canned-policy" {
		t.Errorf("path = %q", rec.path)
	}
	if rec.query.Get("name") != "mypol" {
		t.Errorf("name = %q", rec.query.Get("name"))
	}
}

// ---------- Error handling tests ----------

func TestAdminHTTPError(t *testing.T) {
	a, _, srv := newMockAdmin(t, http.StatusForbidden)
	defer srv.Close()

	err := a.AddUser(context.Background(), "bob", "secret")
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention 403: %v", err)
	}
}

func TestAdminServerError(t *testing.T) {
	a, _, srv := newMockAdmin(t, http.StatusInternalServerError)
	defer srv.Close()

	err := a.RemoveUser(context.Background(), "bob")
	if err == nil {
		t.Fatal("expected error for 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention 500: %v", err)
	}
}

// ---------- SigV4 signing test ----------

func TestRequestsAreSigned(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := newRustFSAdmin(srv.URL, "AKID", "SECRET", "us-east-1", srv.Client())
	_ = a.AddUser(context.Background(), "bob", "bobsecret")

	if authHeader == "" {
		t.Fatal("Authorization header not set")
	}
	if !strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256") {
		t.Errorf("Authorization header = %q, want AWS4-HMAC-SHA256 prefix", authHeader)
	}
}

// ---------- Full Register/Unregister flow with mock ----------

func TestRegisterUnregisterFlow(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		// For bucket-exists, S3 ListBuckets, etc. just return 200.
		w.WriteHeader(http.StatusOK)
		// If it's a HEAD on the bucket, return 200 (bucket exists).
	}))
	defer srv.Close()

	// We can't easily test the full Register flow because minio.Client
	// needs a real S3 endpoint. Instead, test the admin calls directly
	// in sequence.
	a := newRustFSAdmin(srv.URL, "admin", "admin123", "us-east-1", srv.Client())

	ctx := context.Background()

	// Simulate Register sequence.
	if err := a.AddUser(ctx, "tn_ns_myapp", "secret123"); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	policy := buildS3Policy("tentacular", "ns/default/tentacles/myapp/")
	policyJSON, _ := json.Marshal(policy)
	if err := a.AddCannedPolicy(ctx, "tn_ns_myapp_policy", policyJSON); err != nil {
		t.Fatalf("AddCannedPolicy: %v", err)
	}

	if err := a.SetPolicy(ctx, "tn_ns_myapp_policy", "tn_ns_myapp"); err != nil {
		t.Fatalf("SetPolicy: %v", err)
	}

	// Simulate Unregister sequence.
	if err := a.RemoveCannedPolicy(ctx, "tn_ns_myapp_policy"); err != nil {
		t.Fatalf("RemoveCannedPolicy: %v", err)
	}

	if err := a.RemoveUser(ctx, "tn_ns_myapp"); err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}

	// Verify the expected call sequence.
	expected := []string{
		"PUT /rustfs/admin/v3/add-user",
		"PUT /rustfs/admin/v3/add-canned-policy",
		"PUT /rustfs/admin/v3/set-user-or-group-policy",
		"DELETE /rustfs/admin/v3/remove-canned-policy",
		"DELETE /rustfs/admin/v3/remove-user",
	}
	if len(calls) != len(expected) {
		t.Fatalf("got %d calls, want %d: %v", len(calls), len(expected), calls)
	}
	for i, want := range expected {
		if calls[i] != want {
			t.Errorf("call[%d] = %q, want %q", i, calls[i], want)
		}
	}
}

// ---------- buildS3Policy tests (preserved from original) ----------

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
	id, err := CompileIdentity("tent-dev", "hn-digest")
	if err != nil {
		t.Fatalf("CompileIdentity returned error: %v", err)
	}

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

// ---------- newRustFSAdmin defaults ----------

func TestNewRustFSAdminDefaults(t *testing.T) {
	a := newRustFSAdmin("http://localhost:9000", "ak", "sk", "us-east-1", nil)
	if a.httpClient == nil {
		t.Error("httpClient should default to http.DefaultClient")
	}
	if a.endpoint != "http://localhost:9000" {
		t.Errorf("endpoint = %q", a.endpoint)
	}
}
