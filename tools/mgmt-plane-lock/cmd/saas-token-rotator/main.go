// Command saas-token-rotator rotates Bitbucket workspace tokens and
// Jira service-account tokens, writing the new credentials to both
// regional Azure Key Vaults. The rotation pipeline lives in
// internal/rotation.Runner — main is the wiring.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"time"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/bootstrap"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/rotation"
)

func main() {
	ctx, stop := bootstrap.SignalContext()
	defer stop()

	tokenType := flag.String("token-type", "", "Token type to rotate: bitbucket or jira")
	gracePeriodHours := flag.Int("grace-period-hours", 24, "Hours before old token versions are disabled")
	metricsAddr := flag.String("metrics-addr", ":8080", "Prometheus metrics address")
	flag.Parse()
	if *tokenType == "" {
		log.Fatal("--token-type is required (bitbucket or jira)")
	}
	serveMetricsInBackground(ctx, *metricsAddr)
	runner := buildRunner(*tokenType, time.Duration(*gracePeriodHours)*time.Hour)
	if err := runner.Run(ctx); err != nil {
		if errors.Is(err, rotation.ErrSkippedNotActive) || errors.Is(err, context.Canceled) {
			return
		}
		log.Fatalf("rotate %s token: %v", *tokenType, err)
	}
}
