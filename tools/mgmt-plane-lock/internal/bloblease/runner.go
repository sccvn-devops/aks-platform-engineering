package bloblease

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/kube"
	"k8s.io/client-go/kubernetes"
)

// LeaseManager is the contract the LeaseRunner depends on (FR-V4-24).
// Decoupling from *Manager lets unit tests inject a fake and lets the
// runner be tested without an Azure Storage account. The production
// implementation is *Manager from this same package.
type LeaseManager interface {
	Acquire(ctx context.Context, duration time.Duration) (string, error)
	Renew(ctx context.Context, leaseID string) error
	Release(ctx context.Context, leaseID string) error
	PreferredCluster(ctx context.Context) (string, error)
}

// MetricsSink is the optional hook the runner uses to publish the
// "last successful renew" timestamp. Production wires this to the
// promauto gauge in cmd/mgmt-leader-lease/main.go.
type MetricsSink interface {
	SetLeaseRenewedSeconds(unix float64)
}

type leaderState struct {
	leaseID       string
	isLeader      bool
	lastRenewedAt time.Time
}

// LeaseRunner owns the mgmt-leader-lease reconcile loop, lease state
// machine, and standby-status shutdown drain (FR-V4-24). cmd's main.go
// constructs config + Kubernetes/blob clients and delegates to
// Run(ctx).
type LeaseRunner struct {
	Config    config.Controller
	Manager   LeaseManager
	Clientset kubernetes.Interface
	Metrics   MetricsSink
	Logger    *log.Logger

	// TickInterval bounds the reconcile-loop cadence. Defaults to 5s
	// (the historical mgmt-leader-lease value). Tests override to
	// drive the loop quickly.
	TickInterval time.Duration

	state leaderState
}

func (r *LeaseRunner) logf(format string, args ...any) {
	if r.Logger != nil {
		r.Logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// ErrMissingDependency is returned from Run when required fields
// (Manager, Clientset) are nil — programmer error caught early.
var ErrMissingDependency = errors.New("bloblease: LeaseRunner missing required dependency")

// Run reconciles the management-plane lease state on every tick until
// ctx is cancelled. On cancel, if the runner currently holds the
// lease, it attempts a best-effort release + standby-status write
// using a fresh context (so the cancelled ctx does not block the
// release request).
func (r *LeaseRunner) Run(ctx context.Context) error {
	if r.Manager == nil || r.Clientset == nil {
		return ErrMissingDependency
	}
	tickInterval := r.TickInterval
	if tickInterval <= 0 {
		tickInterval = 5 * time.Second
	}

	r.state = leaderState{}

	if err := r.reconcile(ctx); err != nil {
		r.logf("initial reconcile failed: %v", err)
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.drainOnShutdown()
			return nil
		case <-ticker.C:
			if err := r.reconcile(ctx); err != nil {
				r.logf("reconcile failed: %v", err)
			}
		}
	}
}

// Reconcile runs one reconcile pass. Exported so unit tests (and the
// transitional main_test.go in cmd/mgmt-leader-lease) can drive the
// state machine without spinning the ticker loop.
func (r *LeaseRunner) Reconcile(ctx context.Context) error {
	return r.reconcile(ctx)
}

// HoldsLease reports whether the runner currently holds the lease.
// Exported so tests can assert state transitions across reconciles.
func (r *LeaseRunner) HoldsLease() bool { return r.state.isLeader }

// LeaseID returns the currently held lease ID, or "" if not the
// leader. Exported for tests.
func (r *LeaseRunner) LeaseID() string { return r.state.leaseID }

func (r *LeaseRunner) drainOnShutdown() {
	if !r.state.isLeader {
		return
	}
	// Use a fresh context: the parent ctx is cancelled, so any blob
	// SDK call against it would fail immediately and leave the lease
	// undrained, holding leadership for the full lease-duration TTL.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := r.Manager.Release(ctx, r.state.leaseID); err != nil {
		r.logf("release lease during shutdown: %v", err)
		return
	}
	if err := r.writeStandbyStatus(ctx, ""); err != nil {
		r.logf("write standby status during shutdown: %v", err)
	}
}

func (r *LeaseRunner) reconcile(ctx context.Context) error {
	preferredCluster, err := r.Manager.PreferredCluster(ctx)
	if err != nil {
		return err
	}

	if r.state.isLeader {
		if preferredCluster != "" && preferredCluster != r.Config.ClusterName {
			if err := r.Manager.Release(ctx, r.state.leaseID); err != nil {
				return err
			}
			r.state = leaderState{}
			return r.writeStandbyStatus(ctx, preferredCluster)
		}

		if time.Since(r.state.lastRenewedAt) >= r.Config.RenewInterval {
			if err := r.Manager.Renew(ctx, r.state.leaseID); err != nil {
				r.state = leaderState{}
				if !IsLeaseConflict(err) && !IsMissingLeaseError(err) {
					return err
				}
				return r.writeStandbyStatus(ctx, preferredCluster)
			}
			r.state.lastRenewedAt = time.Now().UTC()
			if r.Metrics != nil {
				r.Metrics.SetLeaseRenewedSeconds(float64(r.state.lastRenewedAt.Unix()))
			}
		}

		return kube.UpsertStatus(ctx, r.Clientset, kube.Status{
			Namespace:        r.Config.StatusNamespace,
			ConfigMapName:   r.Config.StatusConfigMap,
			ClusterName:      r.Config.ClusterName,
			LeaseBlobURL:     r.Config.LeaseBlobURL,
			LeadershipStatus: "active",
			LeaseID:          r.state.leaseID,
			PreferredCluster: preferredCluster,
			LastRenewedAt:    r.state.lastRenewedAt,
		})
	}

	if preferredCluster != "" && preferredCluster != r.Config.ClusterName {
		return r.writeStandbyStatus(ctx, preferredCluster)
	}

	leaseID, err := r.Manager.Acquire(ctx, r.Config.LeaseDuration)
	if err != nil {
		if !IsLeaseConflict(err) {
			return err
		}
		return r.writeStandbyStatus(ctx, preferredCluster)
	}

	r.state.leaseID = leaseID
	r.state.isLeader = true
	r.state.lastRenewedAt = time.Now().UTC()
	if r.Metrics != nil {
		r.Metrics.SetLeaseRenewedSeconds(float64(r.state.lastRenewedAt.Unix()))
	}

	return kube.UpsertStatus(ctx, r.Clientset, kube.Status{
		Namespace:        r.Config.StatusNamespace,
		ConfigMapName:   r.Config.StatusConfigMap,
		ClusterName:      r.Config.ClusterName,
		LeaseBlobURL:     r.Config.LeaseBlobURL,
		LeadershipStatus: "active",
		LeaseID:          r.state.leaseID,
		PreferredCluster: preferredCluster,
		LastRenewedAt:    r.state.lastRenewedAt,
	})
}

func (r *LeaseRunner) writeStandbyStatus(ctx context.Context, preferredCluster string) error {
	return kube.UpsertStatus(ctx, r.Clientset, kube.Status{
		Namespace:        r.Config.StatusNamespace,
		ConfigMapName:    r.Config.StatusConfigMap,
		ClusterName:      r.Config.ClusterName,
		LeaseBlobURL:     r.Config.LeaseBlobURL,
		LeadershipStatus: "standby",
		LeaseID:          "",
		PreferredCluster: preferredCluster,
		LastRenewedAt:    time.Now().UTC(),
	})
}
