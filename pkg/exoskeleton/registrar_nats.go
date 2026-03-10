package exoskeleton

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go"
)

// NATSCreds holds the connection details returned after registering
// a tentacle with NATS.
type NATSCreds struct {
	URL           string `json:"url"`
	Token         string `json:"token"`
	SubjectPrefix string `json:"subject_prefix"`
	Protocol      string `json:"protocol"`
}

// NATSRegistrar manages per-tentacle NATS access. In Phase 1 this uses
// a shared token model: all tentacles share the admin token and are
// differentiated by subject prefix convention only. A future phase will
// add per-tentacle NATS accounts or JWTs.
type NATSRegistrar struct {
	cfg NATSConfig
}

// NewNATSRegistrar creates a new NATS registrar and validates connectivity.
func NewNATSRegistrar(ctx context.Context, cfg NATSConfig) (*NATSRegistrar, error) {
	opts := []nats.Option{nats.Name("tentacular-mcp-exoskeleton")}
	if cfg.Token != "" {
		opts = append(opts, nats.Token(cfg.Token))
	}
	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	nc.Close()
	slog.Info("nats: connectivity validated", "url", cfg.URL)
	return &NATSRegistrar{cfg: cfg}, nil
}

// Register returns NATS credentials scoped by subject prefix for the
// given identity. This is idempotent: repeated calls return the same
// logical mapping.
func (r *NATSRegistrar) Register(_ context.Context, id Identity) (*NATSCreds, error) {
	slog.Info("nats: registered tentacle", "user", id.NATSUser, "prefix", id.NATSPrefix)
	return &NATSCreds{
		URL:           r.cfg.URL,
		Token:         r.cfg.Token,
		SubjectPrefix: id.NATSPrefix,
		Protocol:      "nats",
	}, nil
}

// Unregister is a no-op in the shared-token model. A future phase with
// per-tentacle accounts would revoke credentials here.
func (r *NATSRegistrar) Unregister(_ context.Context, id Identity) error {
	slog.Info("nats: unregistered tentacle (no-op in shared token mode)", "user", id.NATSUser)
	return nil
}

// Close is a no-op since we don't hold a persistent connection.
func (r *NATSRegistrar) Close() {}
