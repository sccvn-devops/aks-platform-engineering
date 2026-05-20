package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/bloblease"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
)

type failbackManager interface {
	SetPreferredCluster(ctx context.Context, cluster string) error
	Break(ctx context.Context) error
}

var newFailbackManager = func(ctx context.Context, cfg config.CLI) (failbackManager, error) {
	return bloblease.New(ctx, cfg.LeaseBlobURL, cfg.PreferredMetaKey)
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("usage: mgmt-cli failback --to <cluster> --confirm")
	}

	cfg, err := config.LoadCLI()
	if err != nil {
		log.Fatalf("load cli config: %v", err)
	}

	switch os.Args[1] {
	case "failback":
		runFailback(cfg, os.Args[2:])
	default:
		log.Fatalf("unknown command %q", os.Args[1])
	}
}

func runFailback(cfg config.CLI, args []string) {
	fs := flag.NewFlagSet("failback", flag.ExitOnError)
	target := fs.String("to", "", "target management cluster name")
	confirm := fs.Bool("confirm", false, "confirm the failback request")
	fs.Parse(args)

	if *target == "" {
		log.Fatal("--to is required")
	}
	if !*confirm {
		log.Fatal("--confirm is required")
	}

	ctx := context.Background()
	manager, err := newFailbackManager(ctx, cfg)
	if err != nil {
		log.Fatalf("create blob lease manager: %v", err)
	}

	if err := manager.SetPreferredCluster(ctx, *target); err != nil {
		log.Fatalf("set preferred cluster: %v", err)
	}
	if err := manager.Break(ctx); err != nil {
		log.Fatalf("break active lease: %v", err)
	}

	fmt.Printf("Failback requested for %s via %s\n", *target, cfg.LeaseBlobURL)
}
