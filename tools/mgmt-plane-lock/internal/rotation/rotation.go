// Package rotation owns the saas-token-rotator's run-once pipeline:
// mint a new SaaS token, write it to both regional Key Vaults, sleep
// the grace period, then disable old versions (FR-V4-24).
//
// The split from cmd/saas-token-rotator/main.go is what makes the
// rotator unit-testable: tests inject fake SecretStores + a fake
// Minter and assert the sequence of calls, including the SIGTERM-
// interruptable grace-period sleep that AC3 demands.
package rotation

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/akvwriter"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/bootstrap"
)

// Minter mints a new SaaS token. cmd/saas-token-rotator wires this to
// the Bitbucket OAuth or Jira API-token endpoint per the --token-type
// flag; tests use the fake here.
type Minter interface {
	Mint(ctx context.Context) (string, error)
}

// MinterFunc adapts a plain function to Minter.
type MinterFunc func(ctx context.Context) (string, error)

// Mint implements Minter.
func (f MinterFunc) Mint(ctx context.Context) (string, error) { return f(ctx) }

// LeaseChecker returns whether the local cluster is currently the
// active management plane. The rotator MUST skip when standby
// (mgmt-leader-status configmap leadershipStatus != "active") so the
// passive region does not double-rotate while the active region is
// down. Plumbed through to keep the runner testable without a live
// Kubernetes client.
type LeaseChecker interface {
	IsActive(ctx context.Context) bool
}

// LeaseCheckerFunc adapts a plain function to LeaseChecker.
type LeaseCheckerFunc func(ctx context.Context) bool

// IsActive implements LeaseChecker.
func (f LeaseCheckerFunc) IsActive(ctx context.Context) bool { return f(ctx) }

// AgeReporter receives the post-rotation age signal (~0 days) so the
// rotator's Prometheus gauge can be updated. Optional.
type AgeReporter interface {
	ReportTokenAge(tokenType, secretName string, days float64)
}

// AgeReporterFunc adapts a plain function to AgeReporter.
type AgeReporterFunc func(tokenType, secretName string, days float64)

// ReportTokenAge implements AgeReporter.
func (f AgeReporterFunc) ReportTokenAge(tokenType, secretName string, days float64) {
	f(tokenType, secretName, days)
}

// Runner is the per-binary runner for saas-token-rotator (FR-V4-24).
// One Runner = one rotation pipeline; cmd's main.go constructs one
// and invokes Run(ctx).
type Runner struct {
	TokenType   string
	SecretName  string
	GracePeriod time.Duration
	Minter      Minter
	WEStore     akvwriter.SecretStore
	NEStore     akvwriter.SecretStore
	LeaseCheck  LeaseChecker
	AgeReport   AgeReporter
	Logger      *log.Logger

	// WEAger reads the WE-vault's UpdatedAt for the secret so the
	// rotator can emit the post-rotation age gauge. Optional: when
	// nil, the age signal is skipped (no metric emitted).
	WEAger SecretAger
}

// SecretAger exposes the AKV secret's UpdatedAt timestamp without
// pulling the whole Secret payload — the rotator only needs the
// timestamp for the age metric.
type SecretAger interface {
	GetSecretUpdatedAt(ctx context.Context, name string) (time.Time, error)
}

func (r *Runner) logf(format string, args ...any) {
	if r.Logger != nil {
		r.Logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// ErrSkippedNotActive is returned (and is non-fatal) when the lease
// check reports the local cluster is standby. The cmd surfaces this
// as a log line + exit 0, not a fatal error.
var ErrSkippedNotActive = errors.New("rotation: not the active management cluster")

// Run executes the rotation pipeline: lease check → mint → write to
// both regional vaults → grace-period sleep (ctx-interruptable) →
// disable old versions → emit age metric. Returns ErrSkippedNotActive
// if standby; ctx.Err() if cancelled mid-rotation; any other error if
// a stage fails.
func (r *Runner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}

	if !r.LeaseCheck.IsActive(ctx) {
		r.logf("not the active management cluster, skipping rotation")
		return ErrSkippedNotActive
	}

	newToken, err := r.Minter.Mint(ctx)
	if err != nil {
		return fmt.Errorf("mint %s token: %w", r.TokenType, err)
	}

	// RecoverIfSoftDeleted lets the rotator complete even if a prior
	// aborted run left the secret soft-deleted (e.g., manual operator
	// delete during an incident).
	if err := r.WEStore.Put(ctx, r.SecretName, newToken, akvwriter.RecoverIfSoftDeleted); err != nil {
		return fmt.Errorf("write WE vault secret %s: %w", r.SecretName, err)
	}
	if err := r.NEStore.Put(ctx, r.SecretName, newToken, akvwriter.RecoverIfSoftDeleted); err != nil {
		return fmt.Errorf("write NE vault secret %s: %w", r.SecretName, err)
	}
	r.logf("wrote rotated %s token to both regional vaults as %s", r.TokenType, r.SecretName)

	// AC3: SleepWithContext returns ctx.Err() within 1s of SIGTERM so
	// the process exits cleanly during the grace period. The previous
	// implementation used time.Sleep(graceDuration), which blocked
	// for the full grace window (24h default).
	if err := bootstrap.SleepWithContext(ctx, r.GracePeriod); err != nil {
		return err
	}

	for _, store := range []akvwriter.SecretStore{r.WEStore, r.NEStore} {
		if disabler, ok := store.(versionDisabler); ok {
			if err := disabler.DisableOldVersions(ctx, r.SecretName); err != nil {
				r.logf("disable old versions %s: %v", r.SecretName, err)
			}
		}
	}
	r.logf("disabled old versions of %s after grace period", r.SecretName)

	if r.AgeReport != nil && r.WEAger != nil {
		updatedAt, ageErr := r.WEAger.GetSecretUpdatedAt(ctx, r.SecretName)
		if ageErr == nil {
			ageDays := time.Since(updatedAt).Hours() / 24
			r.AgeReport.ReportTokenAge(r.TokenType, r.SecretName, ageDays)
		}
	}

	return nil
}

// versionDisabler is satisfied by *akvwriter.Client but not by the
// in-test fakes the unit suite uses; the type-assertion in Run lets
// production wire the disable-old-versions step in without forcing
// every fake to implement it.
type versionDisabler interface {
	DisableOldVersions(ctx context.Context, name string) error
}

func (r *Runner) validate() error {
	if r.TokenType == "" {
		return errors.New("rotation: TokenType is required")
	}
	if r.SecretName == "" {
		return errors.New("rotation: SecretName is required")
	}
	if r.Minter == nil {
		return errors.New("rotation: Minter is required")
	}
	if r.WEStore == nil || r.NEStore == nil {
		return errors.New("rotation: WEStore and NEStore are required")
	}
	if r.LeaseCheck == nil {
		return errors.New("rotation: LeaseCheck is required")
	}
	return nil
}
