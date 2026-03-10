package exoskeleton

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// enrichContractDeps updates the workflow.yaml inside the ConfigMap manifest
// with resolved host/port/database/user/subject/container/auth fields from
// the registered credentials. It preserves all other fields in the workflow
// by using a generic map[string]interface{} for round-trip parse/serialize.
func enrichContractDeps(manifests []map[string]interface{}, creds map[string]interface{}) error {
	for _, m := range manifests {
		obj := &unstructured.Unstructured{Object: m}
		if obj.GetKind() != "ConfigMap" {
			continue
		}

		data, ok, _ := unstructured.NestedStringMap(obj.Object, "data")
		if !ok {
			continue
		}
		wfYAML, ok := data["workflow.yaml"]
		if !ok {
			continue
		}

		// Parse into generic map to preserve all fields.
		var workflow map[string]interface{}
		if err := yaml.Unmarshal([]byte(wfYAML), &workflow); err != nil {
			continue
		}

		contractRaw, ok := workflow["contract"]
		if !ok {
			continue
		}
		contract, ok := contractRaw.(map[string]interface{})
		if !ok {
			continue
		}
		depsRaw, ok := contract["dependencies"]
		if !ok {
			continue
		}
		deps, ok := depsRaw.(map[string]interface{})
		if !ok {
			continue
		}

		modified := false
		for depName, depVal := range deps {
			if !strings.HasPrefix(depName, "tentacular-") {
				continue
			}
			credVal, hasCred := creds[depName]
			if !hasCred {
				continue
			}

			depMap, ok := depVal.(map[string]interface{})
			if !ok {
				continue
			}

			switch c := credVal.(type) {
			case *PostgresCreds:
				depMap["host"] = c.Host
				depMap["port"] = c.Port
				depMap["database"] = c.Database
				depMap["user"] = c.User
				depMap["schema"] = c.Schema
				modified = true

			case *NATSCreds:
				host, port := parseHostPort(c.URL)
				depMap["host"] = host
				depMap["port"] = port
				depMap["subject"] = c.SubjectPrefix
				modified = true

			case *RustFSCreds:
				host, port := parseHostPort(c.Endpoint)
				depMap["host"] = host
				depMap["port"] = port
				depMap["container"] = c.Bucket
				depMap["prefix"] = c.Prefix
				modified = true
			}
		}

		if !modified {
			return nil
		}

		// Re-serialize workflow.yaml and update the ConfigMap in place.
		enriched, err := yaml.Marshal(workflow)
		if err != nil {
			return fmt.Errorf("re-serialize workflow.yaml: %w", err)
		}

		// Update the data map in the manifest directly.
		dataMap, _, _ := unstructured.NestedMap(obj.Object, "data")
		if dataMap == nil {
			dataMap = make(map[string]interface{})
		}
		dataMap["workflow.yaml"] = string(enriched)
		if err := unstructured.SetNestedField(obj.Object, dataMap, "data"); err != nil {
			return fmt.Errorf("update ConfigMap data: %w", err)
		}

		slog.Info("exoskeleton: enriched contract dependencies in ConfigMap")
		return nil
	}
	return nil
}

// patchDeploymentAllowNet scans manifests for a Deployment and appends
// exoskeleton service hosts to any --allow-net=... flag in the first
// container's args or command. Only adds hosts for services that have creds.
func patchDeploymentAllowNet(manifests []map[string]interface{}, creds map[string]interface{}) {
	hosts := collectExoHosts(creds)
	if len(hosts) == 0 {
		return
	}

	for _, m := range manifests {
		obj := &unstructured.Unstructured{Object: m}
		if obj.GetKind() != "Deployment" {
			continue
		}

		// Walk into spec.template.spec.containers[0]
		containers, found, _ := unstructured.NestedSlice(obj.Object,
			"spec", "template", "spec", "containers")
		if !found || len(containers) == 0 {
			continue
		}

		container, ok := containers[0].(map[string]interface{})
		if !ok {
			continue
		}

		// Try patching args first, then command.
		if patchAllowNetInSlice(container, "args", hosts) {
			containers[0] = container
			_ = unstructured.SetNestedSlice(obj.Object, containers,
				"spec", "template", "spec", "containers")
			slog.Info("exoskeleton: patched Deployment --allow-net in args")
			return
		}
		if patchAllowNetInSlice(container, "command", hosts) {
			containers[0] = container
			_ = unstructured.SetNestedSlice(obj.Object, containers,
				"spec", "template", "spec", "containers")
			slog.Info("exoskeleton: patched Deployment --allow-net in command")
			return
		}
	}
}

// patchAllowNetInSlice finds a --allow-net=... entry in the named string
// slice field and appends the given hosts. Returns true if patching occurred.
func patchAllowNetInSlice(container map[string]interface{}, field string, hosts []string) bool {
	rawSlice, ok := container[field]
	if !ok {
		return false
	}
	slice, ok := toStringSlice(rawSlice)
	if !ok {
		return false
	}

	for i, arg := range slice {
		if !strings.HasPrefix(arg, "--allow-net=") {
			continue
		}
		// Append hosts to the existing value.
		existing := strings.TrimPrefix(arg, "--allow-net=")
		if existing == "" {
			slice[i] = "--allow-net=" + strings.Join(hosts, ",")
		} else {
			slice[i] = arg + "," + strings.Join(hosts, ",")
		}
		// Convert back to []interface{} for unstructured.
		result := make([]interface{}, len(slice))
		for j, s := range slice {
			result[j] = s
		}
		container[field] = result
		return true
	}
	return false
}

// toStringSlice converts an interface{} (expected []interface{} of strings)
// to a []string.
func toStringSlice(v interface{}) ([]string, bool) {
	raw, ok := v.([]interface{})
	if !ok {
		return nil, false
	}
	result := make([]string, len(raw))
	for i, item := range raw {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		result[i] = s
	}
	return result, true
}

// collectExoHosts returns host:port strings for each registered service.
func collectExoHosts(creds map[string]interface{}) []string {
	var hosts []string
	for name, c := range creds {
		switch v := c.(type) {
		case *PostgresCreds:
			hosts = append(hosts, v.Host+":"+v.Port)
		case *NATSCreds:
			h, p := parseHostPort(v.URL)
			if h != "" && p != "" {
				hosts = append(hosts, h+":"+p)
			}
		case *RustFSCreds:
			h, p := parseHostPort(v.Endpoint)
			if h != "" && p != "" {
				hosts = append(hosts, h+":"+p)
			}
		default:
			slog.Warn("exoskeleton: unknown cred type for allow-net", "dep", name)
		}
	}
	return hosts
}

// parseHostPort extracts host and port from a URL string.
// For "nats://host:4222" returns ("host", "4222").
// For "http://host:9000" returns ("host", "9000").
// For "host:5432" returns ("host", "5432").
func parseHostPort(rawURL string) (string, string) {
	// Try parsing as a full URL first.
	u, err := url.Parse(rawURL)
	if err == nil && u.Host != "" {
		host := u.Hostname()
		port := u.Port()
		return host, port
	}
	// Fallback: treat as host:port.
	if idx := strings.LastIndex(rawURL, ":"); idx > 0 {
		return rawURL[:idx], rawURL[idx+1:]
	}
	return rawURL, ""
}

// AnnotateDeployer adds provenance annotations to each Deployment manifest.
func (c *Controller) AnnotateDeployer(manifests []map[string]interface{}, deployer DeployerInfo) []map[string]interface{} {
	now := time.Now().UTC().Format(time.RFC3339)
	deployedBy := deployer.Email
	if deployedBy == "" {
		deployedBy = "bearer-token"
	}
	deployedVia := deployer.AgentType
	if deployedVia == "" {
		deployedVia = "unknown"
	}

	for _, m := range manifests {
		obj := &unstructured.Unstructured{Object: m}
		if obj.GetKind() != "Deployment" {
			continue
		}
		ann := obj.GetAnnotations()
		if ann == nil {
			ann = make(map[string]string)
		}
		ann["tentacular.io/deployed-by"] = deployedBy
		ann["tentacular.io/deployed-via"] = deployedVia
		ann["tentacular.io/deployed-at"] = now
		obj.SetAnnotations(ann)
	}
	return manifests
}
