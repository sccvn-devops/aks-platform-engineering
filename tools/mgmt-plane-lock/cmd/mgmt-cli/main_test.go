package main

import (
	"context"
	"testing"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
)

type fakeFailbackManager struct {
	preferredCluster string
	breakCalled      bool
}

func (f *fakeFailbackManager) SetPreferredCluster(_ context.Context, cluster string) error {
	f.preferredCluster = cluster
	return nil
}

func (f *fakeFailbackManager) Break(context.Context) error {
	f.breakCalled = true
	return nil
}

func TestRunFailbackSetsTargetAndBreaksLease(t *testing.T) {
	originalFactory := newFailbackManager
	fakeManager := &fakeFailbackManager{}
	newFailbackManager = func(context.Context, config.CLI) (failbackManager, error) {
		return fakeManager, nil
	}
	defer func() {
		newFailbackManager = originalFactory
	}()

	runFailback(config.CLI{
		LeaseBlobURL:     "https://example.blob.core.windows.net/leases/mgmt-active",
		PreferredMetaKey: "preferred-cluster",
	}, []string{"--to", "mgmt-ne", "--confirm"})

	if fakeManager.preferredCluster != "mgmt-ne" {
		t.Fatalf("preferredCluster = %q, want mgmt-ne", fakeManager.preferredCluster)
	}
	if !fakeManager.breakCalled {
		t.Fatal("breakCalled = false, want true")
	}
}

func TestRunBreakLeaseBreaksActiveLease(t *testing.T) {
	originalFactory := newFailbackManager
	fakeManager := &fakeFailbackManager{}
	newFailbackManager = func(context.Context, config.CLI) (failbackManager, error) {
		return fakeManager, nil
	}
	defer func() {
		newFailbackManager = originalFactory
	}()

	runBreakLease(config.CLI{
		LeaseBlobURL:     "https://example.blob.core.windows.net/leases/mgmt-active",
		PreferredMetaKey: "preferred-cluster",
	}, []string{"--confirm"})

	if !fakeManager.breakCalled {
		t.Fatal("breakCalled = false, want true")
	}
	if fakeManager.preferredCluster != "" {
		t.Fatalf("preferredCluster = %q, want empty", fakeManager.preferredCluster)
	}
}
