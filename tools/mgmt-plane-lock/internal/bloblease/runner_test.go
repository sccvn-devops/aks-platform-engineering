package bloblease

import (
	"context"
	"errors"
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

type fakeMetricsSink struct {
	last float64
}

func (f *fakeMetricsSink) SetLeaseRenewedSeconds(v float64) { f.last = v }

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

func TestRunnerAcquiresLeaseAndWritesActiveStatus(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	metrics := &fakeMetricsSink{}
	r := &LeaseRunner{
		Config:    testControllerConfig(),
		Manager:   &fakeLeaseManager{acquireLeaseID: "lease-123"},
		Clientset: clientset,
		Metrics:   metrics,
	}

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if !r.HoldsLease() {
		t.Fatal("HoldsLease() = false, want true")
	}
	if r.LeaseID() != "lease-123" {
		t.Fatalf("LeaseID() = %q, want lease-123", r.LeaseID())
	}
	if metrics.last == 0 {
		t.Fatal("Metrics.SetLeaseRenewedSeconds was not called")
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

func TestRunnerWritesStandbyWhenPreferredClusterDiffers(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	manager := &fakeLeaseManager{preferredCluster: "mgmt-ne"}
	r := &LeaseRunner{
		Config: testControllerConfig(), Manager: manager, Clientset: clientset,
	}

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if manager.acquireCalls != 0 {
		t.Fatalf("Acquire() calls = %d, want 0", manager.acquireCalls)
	}
	status, _ := clientset.CoreV1().ConfigMaps("kube-system").Get(context.Background(), "mgmt-leader-status", metav1.GetOptions{})
	if got := status.Data["leadershipStatus"]; got != "standby" {
		t.Fatalf("leadershipStatus = %q, want standby", got)
	}
}

func TestRunnerReleasesLeadershipWhenPreferenceChanges(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "mgmt-leader-status", Namespace: "kube-system"},
	})
	manager := &fakeLeaseManager{preferredCluster: "mgmt-ne"}
	r := &LeaseRunner{
		Config: testControllerConfig(), Manager: manager, Clientset: clientset,
	}
	r.state = leaderState{leaseID: "lease-123", isLeader: true, lastRenewedAt: time.Now().UTC()}

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if r.HoldsLease() {
		t.Fatal("HoldsLease() = true, want false")
	}
	if len(manager.releasedLeaseIDs) != 1 || manager.releasedLeaseIDs[0] != "lease-123" {
		t.Fatalf("releasedLeaseIDs = %#v, want [lease-123]", manager.releasedLeaseIDs)
	}
}

func TestRunnerWritesStandbyAfterRenewConflict(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "mgmt-leader-status", Namespace: "kube-system"},
	})
	manager := &fakeLeaseManager{
		renewErr: &azcore.ResponseError{StatusCode: 409, ErrorCode: "LeaseAlreadyPresent"},
	}
	r := &LeaseRunner{
		Config: testControllerConfig(), Manager: manager, Clientset: clientset,
	}
	r.state = leaderState{leaseID: "lease-123", isLeader: true, lastRenewedAt: time.Now().Add(-20 * time.Second).UTC()}

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if r.HoldsLease() {
		t.Fatal("HoldsLease() = true, want false")
	}
	if len(manager.renewedLeaseIDs) != 1 || manager.renewedLeaseIDs[0] != "lease-123" {
		t.Fatalf("renewedLeaseIDs = %#v", manager.renewedLeaseIDs)
	}
	status, _ := clientset.CoreV1().ConfigMaps("kube-system").Get(context.Background(), "mgmt-leader-status", metav1.GetOptions{})
	if got := status.Data["leadershipStatus"]; got != "standby" {
		t.Fatalf("leadershipStatus = %q, want standby", got)
	}
}

func TestRunnerRunReleasesLeaseOnShutdown(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	manager := &fakeLeaseManager{acquireLeaseID: "lease-shutdown"}
	r := &LeaseRunner{
		Config:       testControllerConfig(),
		Manager:      manager,
		Clientset:    clientset,
		TickInterval: 5 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	// Allow the initial reconcile + at least one tick to acquire the lease.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if r.HoldsLease() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !r.HoldsLease() {
		cancel()
		t.Fatal("runner never acquired lease within 1s")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not exit after ctx cancel within 2s")
	}

	if len(manager.releasedLeaseIDs) == 0 {
		t.Fatal("Release was not called on shutdown")
	}
}

func TestRunnerRunWithNilDependencyReturnsError(t *testing.T) {
	r := &LeaseRunner{Config: testControllerConfig()}
	if err := r.Run(context.Background()); !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("Run() with nil deps = %v, want ErrMissingDependency", err)
	}
}

func TestRunnerReconcileSurfacesPreferredClusterError(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	manager := &errPreferredClusterManager{}
	r := &LeaseRunner{Config: testControllerConfig(), Manager: manager, Clientset: clientset}

	err := r.Reconcile(context.Background())
	if err == nil {
		t.Fatal("Reconcile() returned nil, want error from PreferredCluster")
	}
}

type errPreferredClusterManager struct{}

func (errPreferredClusterManager) Acquire(context.Context, time.Duration) (string, error) {
	return "", nil
}
func (errPreferredClusterManager) Renew(context.Context, string) error   { return nil }
func (errPreferredClusterManager) Release(context.Context, string) error { return nil }
func (errPreferredClusterManager) PreferredCluster(context.Context) (string, error) {
	return "", errors.New("preferred-cluster lookup failed")
}
