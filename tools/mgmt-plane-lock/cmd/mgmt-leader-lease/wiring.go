package main

import (
	"context"
	"log"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/bloblease"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/bootstrap"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
	"k8s.io/client-go/kubernetes"
)

func buildRunner(ctx context.Context, cfg config.Controller) *bloblease.LeaseRunner {
	restConfig, err := bootstrap.KubeRestConfig()
	if err != nil {
		log.Fatalf("%v", err)
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Fatalf("create kubernetes client: %v", err)
	}
	manager, err := bloblease.New(ctx, cfg.LeaseBlobURL, cfg.PreferredMetaKey)
	if err != nil {
		log.Fatalf("create blob lease manager: %v", err)
	}
	return &bloblease.LeaseRunner{Config: cfg, Manager: manager, Clientset: clientset, Metrics: gaugeSink{}}
}
