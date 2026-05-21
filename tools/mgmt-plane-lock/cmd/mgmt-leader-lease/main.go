package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/bloblease"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/kube"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type leaseManager interface {
	Acquire(ctx context.Context, duration time.Duration) (string, error)
	Renew(ctx context.Context, leaseID string) error
	Release(ctx context.Context, leaseID string) error
	PreferredCluster(ctx context.Context) (string, error)
}

type leaderState struct {
	leaseID         string
	isLeader        bool
	lastRenewedAt   time.Time
}

var leaseRenewedSeconds = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "mgmt_leader_lease_renewed_seconds",
	Help: "Unix timestamp of the last successful management-plane lease acquire or renew.",
})

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadController()
	if err != nil {
		log.Fatalf("load controller config: %v", err)
	}

	go serveMetrics(cfg.MetricsAddr)

	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		kubeConfig, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			log.Fatalf("build kube config: %v", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		log.Fatalf("create kubernetes client: %v", err)
	}

	leaseManager, err := bloblease.New(ctx, cfg.LeaseBlobURL, cfg.PreferredMetaKey)
	if err != nil {
		log.Fatalf("create blob lease manager: %v", err)
	}

	state := leaderState{}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	if err := reconcile(ctx, cfg, leaseManager, clientset, &state); err != nil {
		log.Printf("initial reconcile failed: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			if state.isLeader {
				if err := leaseManager.Release(context.Background(), state.leaseID); err != nil {
					log.Printf("release lease during shutdown: %v", err)
				} else if err := writeStandbyStatus(context.Background(), cfg, clientset, ""); err != nil {
					log.Printf("write standby status during shutdown: %v", err)
				}
			}
			return
		case <-ticker.C:
			if err := reconcile(ctx, cfg, leaseManager, clientset, &state); err != nil {
				log.Printf("reconcile failed: %v", err)
			}
		}
	}
}

func reconcile(
	ctx context.Context,
	cfg config.Controller,
	leaseManager leaseManager,
	clientset kubernetes.Interface,
	state *leaderState,
) error {
	preferredCluster, err := leaseManager.PreferredCluster(ctx)
	if err != nil {
		return err
	}

	if state.isLeader {
		if preferredCluster != "" && preferredCluster != cfg.ClusterName {
			if err := leaseManager.Release(ctx, state.leaseID); err != nil {
				return err
			}
			*state = leaderState{}
			return writeStandbyStatus(ctx, cfg, clientset, preferredCluster)
		}

		if time.Since(state.lastRenewedAt) >= cfg.RenewInterval {
			if err := leaseManager.Renew(ctx, state.leaseID); err != nil {
				*state = leaderState{}
				if !bloblease.IsLeaseConflict(err) && !bloblease.IsMissingLeaseError(err) {
					return err
				}
				return writeStandbyStatus(ctx, cfg, clientset, preferredCluster)
			}
			state.lastRenewedAt = time.Now().UTC()
			leaseRenewedSeconds.Set(float64(state.lastRenewedAt.Unix()))
		}

		return kube.UpsertStatus(ctx, clientset, kube.Status{
			Namespace:        cfg.StatusNamespace,
			ConfigMapName:    cfg.StatusConfigMap,
			ClusterName:      cfg.ClusterName,
			LeaseBlobURL:     cfg.LeaseBlobURL,
			LeadershipStatus: "active",
			LeaseID:          state.leaseID,
			PreferredCluster: preferredCluster,
			LastRenewedAt:    state.lastRenewedAt,
		})
	}

	if preferredCluster != "" && preferredCluster != cfg.ClusterName {
		return writeStandbyStatus(ctx, cfg, clientset, preferredCluster)
	}

	leaseID, err := leaseManager.Acquire(ctx, cfg.LeaseDuration)
	if err != nil {
		if !bloblease.IsLeaseConflict(err) {
			return err
		}
		return writeStandbyStatus(ctx, cfg, clientset, preferredCluster)
	}

	state.leaseID = leaseID
	state.isLeader = true
	state.lastRenewedAt = time.Now().UTC()
	leaseRenewedSeconds.Set(float64(state.lastRenewedAt.Unix()))

	return kube.UpsertStatus(ctx, clientset, kube.Status{
		Namespace:        cfg.StatusNamespace,
		ConfigMapName:    cfg.StatusConfigMap,
		ClusterName:      cfg.ClusterName,
		LeaseBlobURL:     cfg.LeaseBlobURL,
		LeadershipStatus: "active",
		LeaseID:          state.leaseID,
		PreferredCluster: preferredCluster,
		LastRenewedAt:    state.lastRenewedAt,
	})
}

func writeStandbyStatus(ctx context.Context, cfg config.Controller, clientset kubernetes.Interface, preferredCluster string) error {
	return kube.UpsertStatus(ctx, clientset, kube.Status{
		Namespace:        cfg.StatusNamespace,
		ConfigMapName:    cfg.StatusConfigMap,
		ClusterName:      cfg.ClusterName,
		LeaseBlobURL:     cfg.LeaseBlobURL,
		LeadershipStatus: "standby",
		LeaseID:          "",
		PreferredCluster: preferredCluster,
		LastRenewedAt:    time.Now().UTC(),
	})
}

func serveMetrics(addr string) {
	server := &http.Server{
		Addr:              addr,
		Handler:           promhttp.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("metrics server failed: %v", err)
	}
}
