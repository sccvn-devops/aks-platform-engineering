package scaling

import (
	"context"
	"log"
	"time"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

// Runner owns the controller-scaler's reconcile + watch loop
// (FR-V4-24). cmd/controller-scaler/main.go wires up config + a
// kubernetes client and then delegates to Run(ctx).
type Runner struct {
	Config    config.Scaler
	Clientset kubernetes.Interface
	Workloads []Workload

	// Logger is optional — defaults to the package-level logger. The
	// field exists so tests can capture log output.
	Logger *log.Logger
}

func (r *Runner) logf(format string, args ...any) {
	if r.Logger != nil {
		r.Logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Run reconciles workloads against the leadership status ConfigMap on
// every Watch event and on every PollInterval tick, until ctx is
// cancelled. Returns nil on graceful shutdown.
func (r *Runner) Run(ctx context.Context) error {
	if r.Clientset == nil {
		return errNilClientset
	}
	workloads := r.Workloads
	if workloads == nil {
		workloads = DefaultWorkloads()
	}

	if err := r.reconcile(ctx, workloads); err != nil {
		r.logf("initial reconcile failed: %v", err)
	}

	ticker := time.NewTicker(r.Config.PollInterval)
	defer ticker.Stop()

	var watcher watch.Interface
	for {
		if watcher == nil {
			w, err := r.Clientset.CoreV1().ConfigMaps(r.Config.StatusNamespace).Watch(ctx, metav1.ListOptions{
				FieldSelector: fields.OneTermEqualSelector("metadata.name", r.Config.StatusConfigMap).String(),
			})
			if err != nil {
				r.logf("watch leadership status configmap failed: %v", err)
				select {
				case <-ctx.Done():
					return nil
				case <-ticker.C:
					if err := r.reconcile(ctx, workloads); err != nil {
						r.logf("reconcile failed: %v", err)
					}
				}
				continue
			}
			watcher = w
		}

		select {
		case <-ctx.Done():
			watcher.Stop()
			return nil
		case event, ok := <-watcher.ResultChan():
			if !ok {
				watcher.Stop()
				watcher = nil
				continue
			}
			switch event.Type {
			case watch.Added, watch.Modified, watch.Deleted:
				if err := r.reconcile(ctx, workloads); err != nil {
					r.logf("reconcile failed: %v", err)
				}
			}
		case <-ticker.C:
			if err := r.reconcile(ctx, workloads); err != nil {
				r.logf("reconcile failed: %v", err)
			}
		}
	}
}

// Reconcile runs the lease-status read + workload reconcile + cluster
// secret update pipeline once. Exported so unit tests (and the
// legacy main_test seam) can drive a single iteration without
// spinning the watch loop.
func (r *Runner) Reconcile(ctx context.Context) error {
	workloads := r.Workloads
	if workloads == nil {
		workloads = DefaultWorkloads()
	}
	return r.reconcile(ctx, workloads)
}

func (r *Runner) reconcile(ctx context.Context, workloads []Workload) error {
	leaseStatus, err := ReadLeaseStatus(ctx, r.Clientset, r.Config.StatusNamespace, r.Config.StatusConfigMap)
	if err != nil {
		return err
	}

	if err := ReconcileWorkloads(ctx, r.Clientset, workloads, leaseStatus == LeaseStatusActive); err != nil {
		return err
	}

	return UpdateClusterSecretLeaseStatus(ctx, r.Clientset, r.Config.ArgoCDNamespace, r.Config.ClusterName, leaseStatus)
}
