package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/scaling"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadScaler()
	if err != nil {
		log.Fatalf("load scaler config: %v", err)
	}

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

	workloads := scaling.DefaultWorkloads()
	if err := reconcile(ctx, cfg, clientset, workloads); err != nil {
		log.Printf("initial reconcile failed: %v", err)
	}

	runWatchLoop(ctx, cfg, clientset, workloads)
}

func runWatchLoop(ctx context.Context, cfg config.Scaler, clientset kubernetes.Interface, workloads []scaling.Workload) {
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	var watcher watch.Interface

	for {
		if watcher == nil {
			var err error
			watcher, err = clientset.CoreV1().ConfigMaps(cfg.StatusNamespace).Watch(ctx, metav1.ListOptions{
				FieldSelector: fields.OneTermEqualSelector("metadata.name", cfg.StatusConfigMap).String(),
			})
			if err != nil {
				log.Printf("watch leadership status configmap failed: %v", err)
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := reconcile(ctx, cfg, clientset, workloads); err != nil {
						log.Printf("reconcile failed: %v", err)
					}
				}
				continue
			}
		}

		select {
		case <-ctx.Done():
			if watcher != nil {
				watcher.Stop()
			}
			return
		case event, ok := <-watcher.ResultChan():
			if !ok {
				watcher.Stop()
				watcher = nil
				continue
			}
			switch event.Type {
			case watch.Added, watch.Modified, watch.Deleted:
				if err := reconcile(ctx, cfg, clientset, workloads); err != nil {
					log.Printf("reconcile failed: %v", err)
				}
			}
		case <-ticker.C:
			if err := reconcile(ctx, cfg, clientset, workloads); err != nil {
				log.Printf("reconcile failed: %v", err)
			}
		}
	}
}

func reconcile(ctx context.Context, cfg config.Scaler, clientset kubernetes.Interface, workloads []scaling.Workload) error {
	leaseStatus, err := scaling.ReadLeaseStatus(ctx, clientset, cfg.StatusNamespace, cfg.StatusConfigMap)
	if err != nil {
		return err
	}

	if err := scaling.ReconcileWorkloads(ctx, clientset, workloads, leaseStatus == scaling.LeaseStatusActive); err != nil {
		return err
	}

	if err := scaling.UpdateClusterSecretLeaseStatus(ctx, clientset, cfg.ArgoCDNamespace, cfg.ClusterName, leaseStatus); err != nil {
		return err
	}

	return nil
}
