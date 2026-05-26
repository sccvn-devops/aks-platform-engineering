// Command argocd-jira-bridge watches ArgoCD Applications and creates
// Jira issues on sync/rollout failures + drift corrections. The
// reconcile loop lives in internal/jirabridge.Runner — main is the
// wiring.
package main

import (
	"log"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/bootstrap"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
)

func main() {
	ctx, stop := bootstrap.SignalContext()
	defer stop()

	cfg, err := config.LoadJiraBridge()
	if err != nil {
		log.Fatalf("load jira bridge config: %v", err)
	}
	runner := buildRunner(cfg)
	if err := runner.Run(ctx); err != nil {
		log.Fatalf("argocd-jira-bridge run: %v", err)
	}
}
