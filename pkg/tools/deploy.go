package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

const releaseLabelKey = "tentacular.io/release"

// allowedKinds is the set of Kubernetes resource kinds wf_apply will
// accept. Cluster-scoped and sensitive kinds are not permitted.
var allowedKinds = map[string]bool{
	"Deployment":            true,
	"Service":               true,
	"PersistentVolumeClaim": true,
	"NetworkPolicy":         true,
	"ConfigMap":             true,
	"Secret":                true,
	"Job":                   true,
	"CronJob":               true,
	"Ingress":               true,
}

// WorkflowApplyParams are the parameters for wf_apply.
type WorkflowApplyParams struct {
	Namespace string                   `json:"namespace" jsonschema:"Target namespace for the workflow"`
	Name      string                   `json:"name" jsonschema:"Deployment name for tracking resources"`
	Manifests []map[string]interface{} `json:"manifests" jsonschema:"List of Kubernetes manifest objects to apply"`
}

// WorkflowApplyResult is the result of wf_apply.
type WorkflowApplyResult struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Created   int    `json:"created"`
	Updated   int    `json:"updated"`
	Deleted   int    `json:"deleted"`
}

// WorkflowRemoveParams are the parameters for wf_remove.
type WorkflowRemoveParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace containing the workflow resources"`
	Name      string `json:"name" jsonschema:"Deployment name to remove"`
}

// WorkflowRemoveResult is the result of wf_remove.
type WorkflowRemoveResult struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Deleted   int    `json:"deleted"`
}

// WorkflowStatusParams are the parameters for wf_status.
type WorkflowStatusParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace containing the workflow resources"`
	Name      string `json:"name" jsonschema:"Deployment name to check status for"`
}

// WorkflowResourceStatus is the status of a single resource in a workflow deployment.
type WorkflowResourceStatus struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Ready  bool   `json:"ready"`
	Reason string `json:"reason,omitempty"`
}

// WorkflowStatusResult is the result of wf_status.
type WorkflowStatusResult struct {
	Name      string                   `json:"name"`
	Namespace string                   `json:"namespace"`
	Resources []WorkflowResourceStatus `json:"resources"`
}

func registerDeployTools(srv *mcp.Server, client *k8s.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_apply",
		Description: "Apply a set of Kubernetes manifests as a named deployment in a namespace. Uses release labels for tracking and garbage collection.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WorkflowApplyParams) (*mcp.CallToolResult, WorkflowApplyResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, WorkflowApplyResult{}, err
		}
		result, err := handleWorkflowApply(ctx, client, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_remove",
		Description: "Remove all resources belonging to a named deployment in a namespace.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WorkflowRemoveParams) (*mcp.CallToolResult, WorkflowRemoveResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, WorkflowRemoveResult{}, err
		}
		result, err := handleWorkflowRemove(ctx, client, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_status",
		Description: "Get status of all resources belonging to a named deployment in a namespace.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WorkflowStatusParams) (*mcp.CallToolResult, WorkflowStatusResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, WorkflowStatusResult{}, err
		}
		result, err := handleWorkflowStatus(ctx, client, params)
		return nil, result, err
	})
}

// resolveGVR derives the GroupVersionResource from apiVersion and kind using the discovery client.
func resolveGVR(ctx context.Context, client *k8s.Client, apiVersion, kind string) (schema.GroupVersionResource, error) {
	_, resourceLists, err := client.Clientset.Discovery().ServerGroupsAndResources()
	if err != nil && resourceLists == nil {
		return schema.GroupVersionResource{}, fmt.Errorf("discovery failed: %w", err)
	}

	for _, rl := range resourceLists {
		if rl.GroupVersion != apiVersion {
			continue
		}
		for _, r := range rl.APIResources {
			if r.Kind == kind {
				gv, err := schema.ParseGroupVersion(apiVersion)
				if err != nil {
					return schema.GroupVersionResource{}, fmt.Errorf("parse group version %q: %w", apiVersion, err)
				}
				return schema.GroupVersionResource{
					Group:    gv.Group,
					Version:  gv.Version,
					Resource: r.Name,
				}, nil
			}
		}
	}

	return schema.GroupVersionResource{}, fmt.Errorf("no resource found for apiVersion=%q kind=%q", apiVersion, kind)
}

// resourceKey returns a unique identifier for a resource.
func resourceKey(gvr schema.GroupVersionResource, name string) string {
	return fmt.Sprintf("%s/%s/%s", gvr.Group, gvr.Resource, name)
}

// handleWorkflowApply applies a set of Kubernetes manifests as a named deployment.
//
// ConfigMap data integrity note: large string values in ConfigMap data are NOT
// truncated server-side. The manifest map[string]interface{} is wrapped directly
// in unstructured.Unstructured and passed to the dynamic client without any JSON
// round-trip or size limit in this function. If ConfigMap data appears truncated,
// the cause is client-side (e.g. the LLM generating incomplete manifests), not
// the MCP server. See TestWorkflowApplyConfigMapLargeDataIntegrity for verification.
func handleWorkflowApply(ctx context.Context, client *k8s.Client, params WorkflowApplyParams) (WorkflowApplyResult, error) {
	if err := k8s.CheckManagedNamespace(ctx, client, params.Namespace); err != nil {
		return WorkflowApplyResult{}, err
	}

	created, updated, deleted := 0, 0, 0
	appliedKeys := make(map[string]bool)

	for _, manifest := range params.Manifests {
		obj := &unstructured.Unstructured{Object: manifest}

		apiVersion := obj.GetAPIVersion()
		kind := obj.GetKind()
		if apiVersion == "" || kind == "" {
			return WorkflowApplyResult{}, fmt.Errorf("manifest missing apiVersion or kind")
		}

		if !allowedKinds[kind] {
			return WorkflowApplyResult{}, fmt.Errorf("kind %q is not permitted in workflow manifests; allowed kinds: Deployment, Service, PersistentVolumeClaim, NetworkPolicy, ConfigMap, Secret, Job, CronJob, Ingress", kind)
		}

		gvr, err := resolveGVR(ctx, client, apiVersion, kind)
		if err != nil {
			return WorkflowApplyResult{}, fmt.Errorf("resolve GVR for %s/%s: %w", apiVersion, kind, err)
		}

		// Set namespace and release label
		obj.SetNamespace(params.Namespace)
		labels := obj.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[releaseLabelKey] = params.Name
		obj.SetLabels(labels)

		name := obj.GetName()
		if name == "" {
			return WorkflowApplyResult{}, fmt.Errorf("manifest of kind %s is missing a name", kind)
		}

		key := resourceKey(gvr, name)
		appliedKeys[key] = true

		// Try to get existing resource
		existing, err := client.Dynamic.Resource(gvr).Namespace(params.Namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			// Create
			_, createErr := client.Dynamic.Resource(gvr).Namespace(params.Namespace).Create(ctx, obj, metav1.CreateOptions{})
			if createErr != nil {
				return WorkflowApplyResult{}, fmt.Errorf("create %s/%s: %w", kind, name, createErr)
			}
			created++
		} else if err != nil {
			return WorkflowApplyResult{}, fmt.Errorf("get %s/%s: %w", kind, name, err)
		} else {
			// Update: preserve resource version
			obj.SetResourceVersion(existing.GetResourceVersion())
			_, updateErr := client.Dynamic.Resource(gvr).Namespace(params.Namespace).Update(ctx, obj, metav1.UpdateOptions{})
			if updateErr != nil {
				return WorkflowApplyResult{}, fmt.Errorf("update %s/%s: %w", kind, name, updateErr)
			}
			updated++
		}
	}

	// Garbage collect: delete previously-labeled resources not in new manifest set
	// We need to list resources across known types that may carry the release label
	knownGVRs := []schema.GroupVersionResource{
		{Group: "apps", Version: "v1", Resource: "deployments"},
		{Group: "", Version: "v1", Resource: "services"},
		{Group: "", Version: "v1", Resource: "configmaps"},
		{Group: "", Version: "v1", Resource: "secrets"},
		{Group: "batch", Version: "v1", Resource: "jobs"},
		{Group: "batch", Version: "v1", Resource: "cronjobs"},
		{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"},
		{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
		{Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
	}

	labelSelector := fmt.Sprintf("%s=%s", releaseLabelKey, params.Name)
	for _, gvr := range knownGVRs {
		list, err := client.Dynamic.Resource(gvr).Namespace(params.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			continue // skip GVRs that don't exist or are not accessible
		}
		for _, item := range list.Items {
			key := resourceKey(gvr, item.GetName())
			if !appliedKeys[key] {
				err := client.Dynamic.Resource(gvr).Namespace(params.Namespace).Delete(ctx, item.GetName(), metav1.DeleteOptions{})
				if err != nil {
					continue // best-effort GC
				}
				deleted++
			}
		}
	}

	return WorkflowApplyResult{
		Name:      params.Name,
		Namespace: params.Namespace,
		Created:   created,
		Updated:   updated,
		Deleted:   deleted,
	}, nil
}

func handleWorkflowRemove(ctx context.Context, client *k8s.Client, params WorkflowRemoveParams) (WorkflowRemoveResult, error) {
	if err := k8s.CheckManagedNamespace(ctx, client, params.Namespace); err != nil {
		return WorkflowRemoveResult{}, err
	}
	knownGVRs := []schema.GroupVersionResource{
		{Group: "apps", Version: "v1", Resource: "deployments"},
		{Group: "", Version: "v1", Resource: "services"},
		{Group: "", Version: "v1", Resource: "configmaps"},
		{Group: "", Version: "v1", Resource: "secrets"},
		{Group: "batch", Version: "v1", Resource: "jobs"},
		{Group: "batch", Version: "v1", Resource: "cronjobs"},
		{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"},
		{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
		{Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
	}

	labelSelector := fmt.Sprintf("%s=%s", releaseLabelKey, params.Name)
	deleted := 0

	for _, gvr := range knownGVRs {
		list, err := client.Dynamic.Resource(gvr).Namespace(params.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			continue
		}
		for _, item := range list.Items {
			err := client.Dynamic.Resource(gvr).Namespace(params.Namespace).Delete(ctx, item.GetName(), metav1.DeleteOptions{})
			if err != nil {
				continue
			}
			deleted++
		}
	}

	return WorkflowRemoveResult{
		Name:      params.Name,
		Namespace: params.Namespace,
		Deleted:   deleted,
	}, nil
}

func handleWorkflowStatus(ctx context.Context, client *k8s.Client, params WorkflowStatusParams) (WorkflowStatusResult, error) {
	if err := k8s.CheckManagedNamespace(ctx, client, params.Namespace); err != nil {
		return WorkflowStatusResult{}, err
	}
	knownGVRs := []schema.GroupVersionResource{
		{Group: "apps", Version: "v1", Resource: "deployments"},
		{Group: "", Version: "v1", Resource: "services"},
		{Group: "", Version: "v1", Resource: "configmaps"},
		{Group: "", Version: "v1", Resource: "secrets"},
		{Group: "batch", Version: "v1", Resource: "jobs"},
		{Group: "batch", Version: "v1", Resource: "cronjobs"},
		{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"},
		{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
		{Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
	}

	labelSelector := fmt.Sprintf("%s=%s", releaseLabelKey, params.Name)
	resources := []WorkflowResourceStatus{}

	for _, gvr := range knownGVRs {
		list, err := client.Dynamic.Resource(gvr).Namespace(params.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			continue
		}
		for _, item := range list.Items {
			ready, reason := resourceReadiness(item, gvr.Resource)
			resources = append(resources, WorkflowResourceStatus{
				Kind:   strings.ToTitle(gvr.Resource[:1]) + gvr.Resource[1:],
				Name:   item.GetName(),
				Ready:  ready,
				Reason: reason,
			})
		}
	}

	return WorkflowStatusResult{
		Name:      params.Name,
		Namespace: params.Namespace,
		Resources: resources,
	}, nil
}

// resourceReadiness determines readiness from an unstructured resource.
func resourceReadiness(obj unstructured.Unstructured, resource string) (bool, string) {
	switch resource {
	case "deployments":
		readyReplicas, _, _ := unstructured.NestedInt64(obj.Object, "status", "readyReplicas")
		replicas, _, _ := unstructured.NestedInt64(obj.Object, "spec", "replicas")
		if replicas == 0 {
			replicas = 1
		}
		if readyReplicas >= replicas {
			return true, ""
		}
		return false, fmt.Sprintf("%d/%d replicas ready", readyReplicas, replicas)
	case "jobs":
		conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
		for _, c := range conditions {
			cond, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			if cond["type"] == "Complete" && cond["status"] == "True" {
				return true, ""
			}
			if cond["type"] == "Failed" && cond["status"] == "True" {
				return false, "job failed"
			}
		}
		return false, "job in progress"
	default:
		// Services, ConfigMaps, Secrets, NetworkPolicies, CronJobs: presence = ready
		return true, ""
	}
}
