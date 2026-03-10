package exoskeleton

import (
	"encoding/json"
	"fmt"
)

// BuildSecretManifest constructs a Kubernetes Secret manifest containing
// exoskeleton credentials for a workflow deployment. Each enabled service
// gets a JSON-encoded entry keyed by service name.
//
// The returned manifest is an unstructured map suitable for inclusion in
// the wf_apply manifest list.
func BuildSecretManifest(namespace, workflow string, creds map[string]interface{}) (map[string]interface{}, error) {
	secretName := "tentacular-exoskeleton-" + workflow

	stringData := make(map[string]interface{})
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
			stringData[svcName+".token"] = c.Token
			stringData[svcName+".subject_prefix"] = c.SubjectPrefix
			stringData[svcName+".protocol"] = c.Protocol
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

	return map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]interface{}{
			"name":      secretName,
			"namespace": namespace,
			"labels": map[string]interface{}{
				"tentacular.io/release":     workflow,
				"tentacular.io/exoskeleton": "true",
			},
		},
		"type":       "Opaque",
		"stringData": stringData,
	}, nil
}
