package testutil

import (
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// NewFakeClient returns a k8s.Client backed by a fake Kubernetes clientset.
// Use this in unit tests to avoid hitting a real cluster.
func NewFakeClient(objects ...interface{}) *k8s.Client {
	cs := fake.NewClientset()
	_ = objects // objects passed as runtime.Object in typed helpers below
	return k8s.NewClientFromConfig(cs, nil, &rest.Config{Host: "https://fake-cluster:6443"}, nil)
}

// NewFakeClientset returns a raw fake.Clientset for tests that need direct
// access to the fake tracker (e.g. to pre-seed objects).
func NewFakeClientset() *fake.Clientset {
	return fake.NewClientset()
}

// FakeClientWithK8sClient returns both the raw fake clientset and a k8s.Client
// wrapping it, so tests can seed objects via the clientset and call pkg/k8s
// functions via the client.
func FakeClientWithK8sClient() (*fake.Clientset, *k8s.Client) {
	cs := fake.NewClientset()
	client := k8s.NewClientFromConfig(cs, nil, &rest.Config{Host: "https://fake-cluster:6443"}, nil)
	return cs, client
}
