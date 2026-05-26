// Package bootstrap owns the cross-binary lifecycle concerns for the
// mgmt-plane-lock command suite: signal-to-context wiring and the
// /metrics HTTP server with graceful shutdown.
//
// Every binary's main() uses SignalContext() to obtain a context that
// fires on SIGINT/SIGTERM and ServeMetrics(ctx, addr, registry) to
// expose Prometheus metrics tied to the same context. This keeps signal
// handling and metrics-server lifecycle in exactly one place
// (FR-V4-23).
package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// shutdownGrace bounds the graceful-shutdown wait for the metrics
// server. AC3 requires the rotator to exit within 1 second of SIGTERM
// during a grace-period sleep, so the metrics-server drain must not
// outlast that bound.
const shutdownGrace = 500 * time.Millisecond

// SignalContext returns a context that cancels on SIGINT or SIGTERM
// (FR-V4-23). Callers MUST defer the returned cancel; otherwise the
// signal handler stays registered for the lifetime of the process.
func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

// ServeMetrics serves /metrics from registry on addr until ctx is
// cancelled, then gracefully shuts down within shutdownGrace.
//
// Returns nil on graceful shutdown (ctx cancel) or http.ErrServerClosed
// from the server. Non-nil only when ListenAndServe fails (bind error,
// fatal accept error). Designed to be called as `go ServeMetrics(...)`
// from main; binaries that need to surface the bind error wrap it in a
// goroutine that logs.
//
// Passing a nil registry uses prometheus.DefaultGatherer (consistent
// with the historical promauto-driven binaries).
func ServeMetrics(ctx context.Context, addr string, registry prometheus.Gatherer) error {
	if registry == nil {
		registry = prometheus.DefaultGatherer
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		<-serverErr
		return nil
	case err := <-serverErr:
		return err
	}
}

// SleepWithContext blocks for d or until ctx is cancelled, whichever
// fires first. Returns ctx.Err() on cancel, nil on full sleep
// completion. This is the building block runners use instead of
// time.Sleep so a SIGTERM during a grace period exits within the
// shutdownGrace budget (AC3).
func SleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
