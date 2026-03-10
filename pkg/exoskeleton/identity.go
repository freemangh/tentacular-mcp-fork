package exoskeleton

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Identity contains all deterministic identifiers derived from a
// (namespace, workflow) tuple. It is the canonical mapping used by all
// exoskeleton registrars.
type Identity struct {
	Namespace  string
	Workflow   string
	Principal  string // spiffe://tentacular/ns/<ns>/tentacles/<wf>
	PgRole     string // tn_<ns>_<wf> (hyphens -> underscores, max 63 chars)
	PgSchema   string // same as PgRole
	NATSUser   string // <ns>.<wf>
	NATSPrefix string // tentacular.<ns>.<wf>.>
	S3Prefix   string // ns/<ns>/tentacles/<wf>/
	S3User     string // same as PgRole
	S3Policy   string // tn_<ns>_<wf>_policy
}

// maxPgIdentLen is the maximum length of a Postgres identifier.
const maxPgIdentLen = 63

// CompileIdentity deterministically computes all service-specific
// identifiers from a namespace and workflow name.
func CompileIdentity(namespace, workflow string) Identity {
	pgBase := sanitizePg(namespace, workflow)
	return Identity{
		Namespace:  namespace,
		Workflow:   workflow,
		Principal:  fmt.Sprintf("spiffe://tentacular/ns/%s/tentacles/%s", namespace, workflow),
		PgRole:     pgBase,
		PgSchema:   pgBase,
		NATSUser:   fmt.Sprintf("%s.%s", namespace, workflow),
		NATSPrefix: fmt.Sprintf("tentacular.%s.%s.>", namespace, workflow),
		S3Prefix:   fmt.Sprintf("ns/%s/tentacles/%s/", namespace, workflow),
		S3User:     pgBase,
		S3Policy:   truncatePg(fmt.Sprintf("tn_%s_%s_policy", replacePg(namespace), replacePg(workflow))),
	}
}

// replacePg replaces hyphens with underscores and lowercases the input.
func replacePg(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "-", "_"))
}

// sanitizePg builds the "tn_<ns>_<wf>" Postgres identifier with proper
// sanitization and length limiting.
func sanitizePg(namespace, workflow string) string {
	raw := fmt.Sprintf("tn_%s_%s", replacePg(namespace), replacePg(workflow))
	return truncatePg(raw)
}

// truncatePg ensures a Postgres identifier fits within 63 characters.
// If the raw identifier exceeds the limit, it is truncated and a short
// hash suffix is appended to maintain uniqueness.
func truncatePg(raw string) string {
	if len(raw) <= maxPgIdentLen {
		return raw
	}
	h := sha256.Sum256([]byte(raw))
	suffix := fmt.Sprintf("_%x", h[:4]) // 9 chars: _ + 8 hex
	return raw[:maxPgIdentLen-len(suffix)] + suffix
}
