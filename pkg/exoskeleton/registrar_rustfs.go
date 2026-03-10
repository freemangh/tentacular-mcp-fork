package exoskeleton

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// RustFSCreds holds the connection details returned after registering
// a tentacle with RustFS (MinIO-compatible object storage).
type RustFSCreds struct {
	Endpoint  string `json:"endpoint"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	Region    string `json:"region"`
	Protocol  string `json:"protocol"`
}

// RustFSRegistrar manages per-tentacle RustFS IAM users, policies, and
// prefix-scoped access.
type RustFSRegistrar struct {
	admin  *madmin.AdminClient
	s3     *minio.Client
	cfg    RustFSConfig
}

// NewRustFSRegistrar creates a new RustFS registrar with admin and S3 clients.
func NewRustFSRegistrar(_ context.Context, cfg RustFSConfig) (*RustFSRegistrar, error) {
	endpoint := strings.TrimPrefix(strings.TrimPrefix(cfg.Endpoint, "http://"), "https://")
	useSSL := strings.HasPrefix(cfg.Endpoint, "https://")

	admin, err := madmin.New(endpoint, cfg.AccessKey, cfg.SecretKey, useSSL)
	if err != nil {
		return nil, fmt.Errorf("rustfs admin client: %w", err)
	}

	s3Client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: useSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("rustfs s3 client: %w", err)
	}

	return &RustFSRegistrar{admin: admin, s3: s3Client, cfg: cfg}, nil
}

// Register creates (or updates) a scoped RustFS IAM user and policy for
// the given identity. It is idempotent.
func (r *RustFSRegistrar) Register(ctx context.Context, id Identity) (*RustFSCreds, error) {
	bucket := r.cfg.Bucket
	prefix := id.S3Prefix
	userName := id.S3User
	policyName := id.S3Policy

	// Ensure bucket exists.
	exists, err := r.s3.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket %s: %w", bucket, err)
	}
	if !exists {
		if err := r.s3.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: r.cfg.Region}); err != nil {
			// Ignore "already exists" errors from race conditions.
			errResp := minio.ToErrorResponse(err)
			if errResp.Code != "BucketAlreadyOwnedByYou" && errResp.Code != "BucketAlreadyExists" {
				return nil, fmt.Errorf("create bucket %s: %w", bucket, err)
			}
		}
	}

	// Generate secret key for the scoped IAM user.
	// Use the deterministic userName (id.S3User) as the MinIO access key so that
	// Unregister can find and remove the user by the same deterministic name.
	secretKey, err := generateHexPassword(32)
	if err != nil {
		return nil, fmt.Errorf("generate secret key: %w", err)
	}

	// Create or update the IAM user using the deterministic userName as access key.
	if err := r.admin.AddUser(ctx, userName, secretKey); err != nil {
		return nil, fmt.Errorf("add user %s: %w", userName, err)
	}

	// Build a canned policy scoped to the prefix.
	policy := buildS3Policy(bucket, prefix)
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return nil, fmt.Errorf("marshal policy: %w", err)
	}

	if err := r.admin.AddCannedPolicy(ctx, policyName, policyJSON); err != nil {
		return nil, fmt.Errorf("add policy %s: %w", policyName, err)
	}

	// Attach the policy to the user.
	if err := r.admin.SetPolicy(ctx, policyName, userName, false); err != nil {
		return nil, fmt.Errorf("set policy %s for user %s: %w", policyName, userName, err)
	}

	slog.Info("rustfs: registered tentacle",
		"user", userName, "policy", policyName,
		"bucket", bucket, "prefix", prefix)

	return &RustFSCreds{
		Endpoint:  r.cfg.Endpoint,
		AccessKey: userName,
		SecretKey: secretKey,
		Bucket:    bucket,
		Prefix:    prefix,
		Region:    r.cfg.Region,
		Protocol:  "s3",
	}, nil
}

// Unregister removes the tentacle's objects, policy, and IAM user.
func (r *RustFSRegistrar) Unregister(ctx context.Context, id Identity) error {
	bucket := r.cfg.Bucket
	prefix := id.S3Prefix
	userName := id.S3User
	policyName := id.S3Policy

	// Delete all objects under the prefix.
	objectCh := r.s3.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})
	for obj := range objectCh {
		if obj.Err != nil {
			slog.Warn("rustfs: error listing objects for cleanup", "prefix", prefix, "error", obj.Err)
			break
		}
		if err := r.s3.RemoveObject(ctx, bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
			slog.Warn("rustfs: failed to delete object", "key", obj.Key, "error", err)
		}
	}

	// Detach policy from user (best-effort; user may not have the old access key).
	// We remove the policy and user by name rather than tracking the access key.
	// The admin API may not support detach directly, so we just remove user + policy.

	// Remove the canned policy.
	if err := r.admin.RemoveCannedPolicy(ctx, policyName); err != nil {
		slog.Warn("rustfs: remove policy failed", "policy", policyName, "error", err)
	}

	// Remove the IAM user. In the shared model, we use userName as a proxy.
	// Since we generated a unique access key during Register, we don't have
	// it at Unregister time. We remove the user by the userName convention.
	if err := r.admin.RemoveUser(ctx, userName); err != nil {
		slog.Warn("rustfs: remove user failed", "user", userName, "error", err)
	}

	slog.Info("rustfs: unregistered tentacle",
		"user", userName, "policy", policyName,
		"bucket", bucket, "prefix", prefix)

	return nil
}

// Close is a no-op since the MinIO clients don't hold persistent connections.
func (r *RustFSRegistrar) Close() {}

// s3PolicyDoc represents a MinIO/S3 IAM policy document.
type s3PolicyDoc struct {
	Version   string            `json:"Version"`
	Statement []s3PolicyStmt    `json:"Statement"`
}

type s3PolicyStmt struct {
	Effect    string      `json:"Effect"`
	Action    []string    `json:"Action"`
	Resource  interface{} `json:"Resource,omitempty"`
	Condition interface{} `json:"Condition,omitempty"`
}

// buildS3Policy creates an IAM policy granting GetObject, PutObject,
// DeleteObject on objects under the prefix, and ListBucket with a
// prefix condition.
func buildS3Policy(bucket, prefix string) s3PolicyDoc {
	arnBucket := fmt.Sprintf("arn:aws:s3:::%s", bucket)
	arnPrefix := fmt.Sprintf("arn:aws:s3:::%s/%s*", bucket, prefix)

	return s3PolicyDoc{
		Version: "2012-10-17",
		Statement: []s3PolicyStmt{
			{
				Effect:   "Allow",
				Action:   []string{"s3:GetObject", "s3:PutObject", "s3:DeleteObject"},
				Resource: arnPrefix,
			},
			{
				Effect:   "Allow",
				Action:   []string{"s3:ListBucket"},
				Resource: arnBucket,
				Condition: map[string]interface{}{
					"StringLike": map[string]string{
						"s3:prefix": prefix + "*",
					},
				},
			},
		},
	}
}
