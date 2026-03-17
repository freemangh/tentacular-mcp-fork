package exoskeleton

import (
	"encoding/json"
	"fmt"
)

// Label and naming constants used across the exoskeleton subsystem.
const (
	// ExoskeletonLabel is the label key marking a Secret as exoskeleton-managed.
	ExoskeletonLabel = "tentacular.io/exoskeleton"

	// ReleaseLabel is the label key holding the workflow name.
	ReleaseLabel = "tentacular.io/release"

	// ExoskeletonSecretPrefix is the naming prefix for exoskeleton credential Secrets.
	ExoskeletonSecretPrefix = "tentacular-exoskeleton-"
)

// BuildSecretManifest constructs a Kubernetes Secret manifest containing
// exoskeleton credentials for a workflow deployment. Each enabled service
// gets a JSON-encoded entry keyed by service name.
//
// The returned manifest is an unstructured map suitable for inclusion in
// the wf_apply manifest list.
func BuildSecretManifest(namespace, workflow string, creds map[string]any) (map[string]any, error) {
	secretName := ExoskeletonSecretPrefix + workflow

	stringData := make(map[string]any)
	for svcName, svcCreds := range creds {
		// Marshal per-service creds to a flat key-value map.
		switch c := svcCreds.(type) {
		case *PostgresCreds:
			stringData[svcName+".host"] = c.Host
			stringData[svcName+".port"] = c.Port
			stringData[svcName+".database"] = c.Database
			stringData[svcName+".user"] = c.User
			stringData[svcName+".password"] = c.Password
			stringData[svcName+".schema"] = c.Schema
			stringData[svcName+".protocol"] = c.Protocol
		case *NATSCreds:
			stringData[svcName+".url"] = c.URL
			if c.Token != "" {
				stringData[svcName+".token"] = c.Token
			}
			stringData[svcName+".subject_prefix"] = c.SubjectPrefix
			stringData[svcName+".protocol"] = c.Protocol
			stringData[svcName+".auth_method"] = c.AuthMethod
		case *RustFSCreds:
			stringData[svcName+".endpoint"] = c.Endpoint
			stringData[svcName+".access_key"] = c.AccessKey
			stringData[svcName+".secret_key"] = c.SecretKey
			stringData[svcName+".bucket"] = c.Bucket
			stringData[svcName+".prefix"] = c.Prefix
			stringData[svcName+".region"] = c.Region
			stringData[svcName+".protocol"] = c.Protocol
		default:
			// Fallback: JSON-encode the entire value under a single key.
			b, err := json.Marshal(svcCreds)
			if err == nil {
				stringData[svcName] = string(b)
			}
		}
	}

	// Always include identity fields.
	id, err := CompileIdentity(namespace, workflow)
	if err != nil {
		return nil, fmt.Errorf("compile identity for secret: %w", err)
	}
	stringData["tentacular-identity.principal"] = id.Principal
	stringData["tentacular-identity.namespace"] = namespace
	stringData["tentacular-identity.workflow"] = workflow

	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      secretName,
			"namespace": namespace,
			"labels": map[string]any{
				ReleaseLabel:     workflow,
				ExoskeletonLabel: "true",
			},
		},
		"type":       "Opaque",
		"stringData": stringData,
	}, nil
}
