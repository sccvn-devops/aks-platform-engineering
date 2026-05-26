// Command controller-scaler watches the management-plane leadership
// ConfigMap and scales the active/standby workloads to match. The
// run-loop lives in internal/scaling.Runner — main is the wiring.
package main

import (
	"log"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/bootstrap"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/scaling"
	"k8s.io/client-go/kubernetes"
)

func main() {
	ctx, stop := bootstrap.SignalContext()
	defer stop()

	cfg, err := config.LoadScaler()
	if err != nil {
		log.Fatalf("load scaler config: %v", err)
	}
	restConfig, err := bootstrap.KubeRestConfig()
	if err != nil {
		log.Fatalf("%v", err)
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Fatalf("create kubernetes client: %v", err)
	}

	runner := &scaling.Runner{Config: cfg, Clientset: clientset, Workloads: scaling.DefaultWorkloads()}
	if err := runner.Run(ctx); err != nil {
		log.Fatalf("controller-scaler run: %v", err)
	}
}
