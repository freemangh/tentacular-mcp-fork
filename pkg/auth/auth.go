package auth

import (
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

// LoadToken reads the bearer token from the given file path.
func LoadToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read token file %s: %w", path, err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("token file %s is empty", path)
	}
	return token, nil
}

// Middleware returns an HTTP middleware that validates Bearer token authentication.
// The /healthz endpoint bypasses authentication.
// Error responses are JSON-formatted per the MCP Streamable HTTP transport spec.
func Middleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			slog.Warn("auth rejected: missing authorization header",
				"method", r.Method,
				"path", r.URL.Path,
				"remote", r.RemoteAddr,
				"headers", fmt.Sprintf("%v", r.Header))
			writeAuthError(w, "missing authorization header")
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeAuthError(w, "invalid authorization header format")
			return
		}

		provided := strings.TrimPrefix(authHeader, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			writeAuthError(w, "invalid token")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// writeAuthError sends a JSON-formatted 401 response. MCP Streamable HTTP
// clients expect JSON error bodies; plain text causes parse failures.
func writeAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	fmt.Fprintf(w, `{"error":"unauthorized","error_description":%q}`, msg)
}
