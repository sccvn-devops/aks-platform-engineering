package kube

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestUpsertStatusCreatesConfigMap(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	err := UpsertStatus(context.Background(), clientset, Status{
		Namespace:        "kube-system",
		ConfigMapName:    "mgmt-leader-status",
		ClusterName:      "mgmt-we",
		LeadershipStatus: "active",
		LeaseID:          "lease-123",
		PreferredCluster: "",
		LastRenewedAt:    time.Unix(100, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertStatus() error = %v", err)
	}

	cm, err := clientset.CoreV1().ConfigMaps("kube-system").Get(context.Background(), "mgmt-leader-status", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get configmap: %v", err)
	}

	if cm.Labels["app.kubernetes.io/name"] != "mgmt-leader-lease" {
		t.Fatalf("app label = %q, want mgmt-leader-lease", cm.Labels["app.kubernetes.io/name"])
	}
	if cm.Data["leadershipStatus"] != "active" {
		t.Fatalf("leadershipStatus = %q, want active", cm.Data["leadershipStatus"])
	}
}

func TestUpsertStatusUpdatesExistingConfigMap(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mgmt-leader-status",
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"leadershipStatus": "standby",
		},
	})

	err := UpsertStatus(context.Background(), clientset, Status{
		Namespace:        "kube-system",
		ConfigMapName:    "mgmt-leader-status",
		ClusterName:      "mgmt-ne",
		LeadershipStatus: "standby",
		LeaseID:          "",
		PreferredCluster: "mgmt-we",
		LastRenewedAt:    time.Unix(200, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertStatus() error = %v", err)
	}

	cm, err := clientset.CoreV1().ConfigMaps("kube-system").Get(context.Background(), "mgmt-leader-status", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get configmap: %v", err)
	}

	if cm.Data["clusterName"] != "mgmt-ne" {
		t.Fatalf("clusterName = %q, want mgmt-ne", cm.Data["clusterName"])
	}
	if cm.Data["preferredCluster"] != "mgmt-we" {
		t.Fatalf("preferredCluster = %q, want mgmt-we", cm.Data["preferredCluster"])
	}
	if cm.Labels["app.kubernetes.io/managed-by"] != "argocd" {
		t.Fatalf("managed-by label = %q, want argocd", cm.Labels["app.kubernetes.io/managed-by"])
	}
}
