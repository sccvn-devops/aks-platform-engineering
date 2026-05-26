// Command mgmt-leader-lease holds (or contends for) the Azure Storage
// blob lease that designates the active management cluster. The run
// loop lives in internal/bloblease.LeaseRunner — main is the wiring.
package main

import (
	"log"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/bootstrap"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
)

func main() {
	ctx, stop := bootstrap.SignalContext()
	defer stop()

	cfg, err := config.LoadController()
	if err != nil {
		log.Fatalf("load controller config: %v", err)
	}
	go func() {
		if err := bootstrap.ServeMetrics(ctx, cfg.MetricsAddr, nil); err != nil {
			log.Printf("metrics server: %v", err)
		}
	}()
	if err := buildRunner(ctx, cfg).Run(ctx); err != nil {
		log.Fatalf("mgmt-leader-lease run: %v", err)
	}
}
