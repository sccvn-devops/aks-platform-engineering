package bloblease

import (
	"bytes"
	"context"
	"errors"
	"log"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"k8s.io/client-go/kubernetes/fake"
)

// TestRunnerRenewsAfterRenewIntervalElapsed exercises the
// state.isLeader && time.Since(lastRenewedAt) >= RenewInterval branch.
func TestRunnerRenewsAfterRenewIntervalElapsed(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	manager := &fakeLeaseManager{}
	metrics := &fakeMetricsSink{}
	r := &LeaseRunner{
		Config:    testControllerConfig(),
		Manager:   manager,
		Clientset: clientset,
		Metrics:   metrics,
	}
	r.state = leaderState{leaseID: "lease-abc", isLeader: true, lastRenewedAt: time.Now().Add(-1 * time.Hour).UTC()}

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if len(manager.renewedLeaseIDs) != 1 || manager.renewedLeaseIDs[0] != "lease-abc" {
		t.Fatalf("renewedLeaseIDs = %#v, want [lease-abc]", manager.renewedLeaseIDs)
	}
	if !r.HoldsLease() {
		t.Fatal("HoldsLease() = false after successful renew")
	}
}

func TestRunnerReturnsErrorOnNonConflictRenewFailure(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	manager := &fakeLeaseManager{renewErr: errors.New("boom — non-conflict")}
	r := &LeaseRunner{
		Config: testControllerConfig(), Manager: manager, Clientset: clientset,
	}
	r.state = leaderState{leaseID: "lease-abc", isLeader: true, lastRenewedAt: time.Now().Add(-1 * time.Hour).UTC()}

	err := r.Reconcile(context.Background())
	if err == nil {
		t.Fatal("Reconcile() returned nil, want non-conflict error")
	}
	if r.HoldsLease() {
		t.Fatal("HoldsLease() = true after non-conflict renew failure, want false")
	}
}

func TestRunnerWritesStandbyOnAcquireConflict(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	manager := &fakeLeaseManager{
		acquireErr: &azcore.ResponseError{StatusCode: 409, ErrorCode: "LeaseAlreadyPresent"},
	}
	r := &LeaseRunner{Config: testControllerConfig(), Manager: manager, Clientset: clientset}

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if r.HoldsLease() {
		t.Fatal("HoldsLease() = true after acquire conflict")
	}
}

func TestRunnerSurfacesNonConflictAcquireError(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	manager := &fakeLeaseManager{acquireErr: errors.New("non-conflict acquire failure")}
	r := &LeaseRunner{Config: testControllerConfig(), Manager: manager, Clientset: clientset}

	err := r.Reconcile(context.Background())
	if err == nil {
		t.Fatal("Reconcile() returned nil, want non-conflict acquire error")
	}
}

func TestRunnerDrainOnShutdownSkipsWhenNotLeader(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	manager := &fakeLeaseManager{}
	r := &LeaseRunner{Config: testControllerConfig(), Manager: manager, Clientset: clientset}
	r.drainOnShutdown()

	if len(manager.releasedLeaseIDs) != 0 {
		t.Fatalf("releasedLeaseIDs = %#v, want [] when not leader", manager.releasedLeaseIDs)
	}
}

func TestRunnerDrainOnShutdownLogsReleaseError(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	manager := &fakeLeaseManager{releaseErr: errors.New("release failed")}
	var buf bytes.Buffer
	r := &LeaseRunner{
		Config:    testControllerConfig(),
		Manager:   manager,
		Clientset: clientset,
		Logger:    log.New(&buf, "", 0),
	}
	r.state = leaderState{leaseID: "x", isLeader: true}
	r.drainOnShutdown()

	if !contains(buf.String(), "release lease during shutdown") {
		t.Fatalf("log output %q missing release-error line", buf.String())
	}
}

func TestRunnerLogfWritesToCustomLogger(t *testing.T) {
	var buf bytes.Buffer
	r := &LeaseRunner{Logger: log.New(&buf, "", 0)}
	r.logf("ping %d", 1)
	if buf.String() != "ping 1\n" {
		t.Fatalf("logf wrote %q, want %q", buf.String(), "ping 1\n")
	}
}

func TestRunnerLogfDefaultsToStdLogger(t *testing.T) {
	r := &LeaseRunner{}
	r.logf("noop") // exercises default path; must not panic
}

func TestRunnerTickIntervalDefault(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	manager := &fakeLeaseManager{acquireLeaseID: "lease-tick"}
	r := &LeaseRunner{
		Config:    testControllerConfig(),
		Manager:   manager,
		Clientset: clientset,
		// TickInterval intentionally unset → exercises the default-5s branch.
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	time.Sleep(20 * time.Millisecond) // Run executes the initial reconcile, then blocks on the 5s tick.
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not exit on cancel within 2s")
	}
}

func TestIsLeaseConflictForGeneric409(t *testing.T) {
	if !IsLeaseConflict(&azcore.ResponseError{StatusCode: 409, ErrorCode: "Unknown"}) {
		t.Fatal("IsLeaseConflict(generic 409) = false, want true")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && bytes.Contains([]byte(s), []byte(substr))
}

