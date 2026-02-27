package proxy

import (
	"testing"
)

func TestBuildDeployment_DefaultImage(t *testing.T) {
	dep := BuildDeployment(Options{Namespace: "tentacular-support"})
	if dep.Name != DeploymentName {
		t.Errorf("name: got %q, want %q", dep.Name, DeploymentName)
	}
	if dep.Namespace != "tentacular-support" {
		t.Errorf("namespace: got %q, want %q", dep.Namespace, "tentacular-support")
	}
	if len(dep.Spec.Template.Spec.Containers) == 0 {
		t.Fatal("expected at least one container")
	}
	if dep.Spec.Template.Spec.Containers[0].Image != DefaultImage {
		t.Errorf("image: got %q, want %q", dep.Spec.Template.Spec.Containers[0].Image, DefaultImage)
	}
}

func TestBuildDeployment_CustomImage(t *testing.T) {
	dep := BuildDeployment(Options{Namespace: "ns", Image: "my-esm:v2"})
	if dep.Spec.Template.Spec.Containers[0].Image != "my-esm:v2" {
		t.Errorf("image: got %q, want my-esm:v2", dep.Spec.Template.Spec.Containers[0].Image)
	}
}

func TestBuildDeployment_EmptyDirVolume(t *testing.T) {
	dep := BuildDeployment(Options{Namespace: "ns"})
	if len(dep.Spec.Template.Spec.Volumes) == 0 {
		t.Fatal("expected volume for cache")
	}
	vol := dep.Spec.Template.Spec.Volumes[0]
	if vol.EmptyDir == nil {
		t.Error("expected emptyDir volume when StorageSize is empty")
	}
}

func TestBuildDeployment_PVCVolume(t *testing.T) {
	dep := BuildDeployment(Options{Namespace: "ns", StorageSize: "5Gi"})
	if len(dep.Spec.Template.Spec.Volumes) == 0 {
		t.Fatal("expected volume for cache")
	}
	vol := dep.Spec.Template.Spec.Volumes[0]
	if vol.PersistentVolumeClaim == nil {
		t.Error("expected PVC volume when StorageSize is set")
	}
}

func TestBuildDeployment_Labels(t *testing.T) {
	dep := BuildDeployment(Options{Namespace: "ns"})
	labels := dep.Labels
	if labels["app.kubernetes.io/managed-by"] != "tentacular" {
		t.Errorf("missing managed-by label: %v", labels)
	}
	if labels["app.kubernetes.io/name"] != "esm-sh" {
		t.Errorf("missing name label: %v", labels)
	}
}

func TestBuildService_NameAndNamespace(t *testing.T) {
	svc := BuildService(Options{Namespace: "my-ns"})
	if svc.Name != ServiceName {
		t.Errorf("name: got %q, want %q", svc.Name, ServiceName)
	}
	if svc.Namespace != "my-ns" {
		t.Errorf("namespace: got %q, want my-ns", svc.Namespace)
	}
}

func TestBuildService_Port(t *testing.T) {
	svc := BuildService(Options{Namespace: "ns"})
	if len(svc.Spec.Ports) == 0 {
		t.Fatal("expected at least one port")
	}
	if svc.Spec.Ports[0].Port != ContainerPort {
		t.Errorf("port: got %d, want %d", svc.Spec.Ports[0].Port, ContainerPort)
	}
}

func TestBuildDeployment_ReadinessProbe(t *testing.T) {
	dep := BuildDeployment(Options{Namespace: "ns"})
	c := dep.Spec.Template.Spec.Containers[0]
	if c.ReadinessProbe == nil {
		t.Fatal("expected readiness probe")
	}
	if c.ReadinessProbe.HTTPGet == nil {
		t.Fatal("expected HTTP GET readiness probe")
	}
	if c.ReadinessProbe.HTTPGet.Path != "/status" {
		t.Errorf("probe path: got %q, want /status", c.ReadinessProbe.HTTPGet.Path)
	}
}
