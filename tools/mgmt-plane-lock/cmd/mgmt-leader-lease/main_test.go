package main

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type fakeLeaseManager struct {
	preferredCluster string
	acquireLeaseID   string
	acquireErr       error
	renewErr         error
	releaseErr       error
	releasedLeaseIDs []string
	renewedLeaseIDs  []string
	acquireCalls     int
}

func (f *fakeLeaseManager) Acquire(_ context.Context, _ time.Duration) (string, error) {
	f.acquireCalls++
	return f.acquireLeaseID, f.acquireErr
}

func (f *fakeLeaseManager) Renew(_ context.Context, leaseID string) error {
	f.renewedLeaseIDs = append(f.renewedLeaseIDs, leaseID)
	return f.renewErr
}

func (f *fakeLeaseManager) Release(_ context.Context, leaseID string) error {
	f.releasedLeaseIDs = append(f.releasedLeaseIDs, leaseID)
	return f.releaseErr
}

func (f *fakeLeaseManager) PreferredCluster(context.Context) (string, error) {
	return f.preferredCluster, nil
}

func testControllerConfig() config.Controller {
	return config.Controller{
		ClusterName:      "mgmt-we",
		LeaseBlobURL:     "https://example.blob.core.windows.net/leases/mgmt-active",
		StatusNamespace:  "kube-system",
		StatusConfigMap:  "mgmt-leader-status",
		MetricsAddr:      ":8080",
		LeaseDuration:    60 * time.Second,
		RenewInterval:    15 * time.Second,
		PreferredMetaKey: "preferred-cluster",
	}
}

func TestReconcileAcquiresLeaseAndWritesActiveStatus(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	manager := &fakeLeaseManager{acquireLeaseID: "lease-123"}
	state := &leaderState{}

	if err := reconcile(context.Background(), testControllerConfig(), manager, clientset, state); err != nil {
		t.Fatalf("reconcile() error = %v", err)
	}

	if !state.isLeader {
		t.Fatal("state.isLeader = false, want true")
	}
	if state.leaseID != "lease-123" {
		t.Fatalf("state.leaseID = %q, want lease-123", state.leaseID)
	}

	status, err := clientset.CoreV1().ConfigMaps("kube-system").Get(context.Background(), "mgmt-leader-status", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get status configmap: %v", err)
	}

	if got := status.Data["leadershipStatus"]; got != "active" {
		t.Fatalf("leadershipStatus = %q, want active", got)
	}
	if got := status.Data["leaseID"]; got != "lease-123" {
		t.Fatalf("leaseID = %q, want lease-123", got)
	}
}

func TestReconcileWritesStandbyWhenPreferredClusterDiffers(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	manager := &fakeLeaseManager{preferredCluster: "mgmt-ne"}
	state := &leaderState{}

	if err := reconcile(context.Background(), testControllerConfig(), manager, clientset, state); err != nil {
		t.Fatalf("reconcile() error = %v", err)
	}

	if manager.acquireCalls != 0 {
		t.Fatalf("Acquire() calls = %d, want 0", manager.acquireCalls)
	}

	status, err := clientset.CoreV1().ConfigMaps("kube-system").Get(context.Background(), "mgmt-leader-status", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get status configmap: %v", err)
	}

	if got := status.Data["leadershipStatus"]; got != "standby" {
		t.Fatalf("leadershipStatus = %q, want standby", got)
	}
	if got := status.Data["preferredCluster"]; got != "mgmt-ne" {
		t.Fatalf("preferredCluster = %q, want mgmt-ne", got)
	}
}

func TestReconcileReleasesLeadershipWhenPreferenceChanges(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mgmt-leader-status",
			Namespace: "kube-system",
		},
	})
	manager := &fakeLeaseManager{preferredCluster: "mgmt-ne"}
	state := &leaderState{
		leaseID:       "lease-123",
		isLeader:      true,
		lastRenewedAt: time.Now().UTC(),
	}

	if err := reconcile(context.Background(), testControllerConfig(), manager, clientset, state); err != nil {
		t.Fatalf("reconcile() error = %v", err)
	}

	if state.isLeader {
		t.Fatal("state.isLeader = true, want false")
	}
	if state.leaseID != "" {
		t.Fatalf("state.leaseID = %q, want empty", state.leaseID)
	}
	if len(manager.releasedLeaseIDs) != 1 || manager.releasedLeaseIDs[0] != "lease-123" {
		t.Fatalf("releasedLeaseIDs = %#v, want [lease-123]", manager.releasedLeaseIDs)
	}

	status, err := clientset.CoreV1().ConfigMaps("kube-system").Get(context.Background(), "mgmt-leader-status", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get status configmap: %v", err)
	}
	if got := status.Data["leadershipStatus"]; got != "standby" {
		t.Fatalf("leadershipStatus = %q, want standby", got)
	}
}

func TestReconcileWritesStandbyAfterRenewConflict(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mgmt-leader-status",
			Namespace: "kube-system",
		},
	})
	manager := &fakeLeaseManager{
		renewErr: &azcore.ResponseError{StatusCode: 409, ErrorCode: "LeaseAlreadyPresent"},
	}
	state := &leaderState{
		leaseID:       "lease-123",
		isLeader:      true,
		lastRenewedAt: time.Now().Add(-20 * time.Second).UTC(),
	}

	if err := reconcile(context.Background(), testControllerConfig(), manager, clientset, state); err != nil {
		t.Fatalf("reconcile() error = %v", err)
	}

	if state.isLeader {
		t.Fatal("state.isLeader = true, want false")
	}
	if state.leaseID != "" {
		t.Fatalf("state.leaseID = %q, want empty", state.leaseID)
	}
	if len(manager.renewedLeaseIDs) != 1 || manager.renewedLeaseIDs[0] != "lease-123" {
		t.Fatalf("renewedLeaseIDs = %#v, want [lease-123]", manager.renewedLeaseIDs)
	}

	status, err := clientset.CoreV1().ConfigMaps("kube-system").Get(context.Background(), "mgmt-leader-status", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get status configmap: %v", err)
	}
	if got := status.Data["leadershipStatus"]; got != "standby" {
		t.Fatalf("leadershipStatus = %q, want standby", got)
	}
}
