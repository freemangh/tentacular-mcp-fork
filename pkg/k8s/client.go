package k8s

import (
	"fmt"
	"net/http"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// Timeout for K8s API calls.
	Timeout = 30 * time.Second

	// ManagedByLabel is applied to all resources created by tentacular.
	ManagedByLabel = "app.kubernetes.io/managed-by"
	ManagedByValue = "tentacular"

	// NameLabel identifies the workflow name.
	NameLabel = "app.kubernetes.io/name"

	// VersionLabel identifies the workflow version.
	VersionLabel = "app.kubernetes.io/version"
)

// Client wraps Kubernetes API clients for in-cluster operations.
type Client struct {
	Clientset kubernetes.Interface
	Dynamic   dynamic.Interface
	Config    *rest.Config
	HTTP      *http.Client
}

// NewInClusterClient creates a Client using in-cluster service account credentials.
func NewInClusterClient() (*Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("load in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes clientset: %w", err)
	}

	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	return &Client{
		Clientset: clientset,
		Dynamic:   dyn,
		Config:    config,
		HTTP:      &http.Client{Timeout: Timeout},
	}, nil
}

// NewClientFromConfig creates a Client from explicit clients and config.
// Used in tests with fake clients. Pass nil for dynamic if not needed.
// Pass nil for httpClient to get a default client with Timeout.
func NewClientFromConfig(clientset kubernetes.Interface, dyn dynamic.Interface, config *rest.Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: Timeout}
	}
	return &Client{
		Clientset: clientset,
		Dynamic:   dyn,
		Config:    config,
		HTTP:      httpClient,
	}
}
