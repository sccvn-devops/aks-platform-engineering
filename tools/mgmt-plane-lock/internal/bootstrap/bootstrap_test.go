package bootstrap

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// findFreePort returns a port the OS-allocated for a listener that
// has been immediately closed — i.e., a port that is very likely free
// when the caller binds it a moment later.
func findFreePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for port allocation: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func TestServeMetricsShutsDownOnContextCancel(t *testing.T) {
	addr := findFreePort(t)
	registry := prometheus.NewRegistry()
	registry.MustRegister(prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_gauge", Help: "test"}))

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- ServeMetrics(ctx, addr, registry)
	}()

	deadline := time.Now().Add(2 * time.Second)
	var resp *http.Response
	var err error
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://" + addr + "/metrics")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("metrics server never came up: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "test_gauge") {
		t.Fatalf("metrics output missing test_gauge: %q", string(body))
	}

	start := time.Now()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ServeMetrics returned %v, want nil on cancel", err)
		}
		if elapsed := time.Since(start); elapsed > shutdownGrace+200*time.Millisecond {
			t.Fatalf("ServeMetrics shutdown took %v, want <= %v", elapsed, shutdownGrace+200*time.Millisecond)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ServeMetrics did not return after ctx cancel within 2s")
	}
}

func TestServeMetricsReturnsBindError(t *testing.T) {
	// Bind a listener so the address is in use.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = ServeMetrics(ctx, l.Addr().String(), prometheus.NewRegistry())
	if err == nil {
		t.Fatal("ServeMetrics on busy port returned nil, want bind error")
	}
}

func TestServeMetricsUsesDefaultGathererWhenNil(t *testing.T) {
	addr := findFreePort(t)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- ServeMetrics(ctx, addr, nil)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/metrics")
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServeMetrics(nil registry) did not shut down")
	}
}

func TestSleepWithContextHonoursCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	start := time.Now()
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := SleepWithContext(ctx, time.Hour)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("SleepWithContext returned nil, want ctx error")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("SleepWithContext took %v, want <500ms on cancel", elapsed)
	}
}

func TestSleepWithContextCompletesNormally(t *testing.T) {
	err := SleepWithContext(context.Background(), 10*time.Millisecond)
	if err != nil {
		t.Fatalf("SleepWithContext returned %v, want nil", err)
	}
}

func TestSleepWithContextZeroDurationReturnsImmediately(t *testing.T) {
	start := time.Now()
	err := SleepWithContext(context.Background(), 0)
	if err != nil {
		t.Fatalf("SleepWithContext(0) returned %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Millisecond {
		t.Fatalf("SleepWithContext(0) took %v, want immediate", elapsed)
	}
}

func TestSleepWithContextZeroDurationWithCancelledCtxReturnsErr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := SleepWithContext(ctx, 0)
	if err == nil {
		t.Fatal("SleepWithContext(0, cancelled) returned nil, want ctx error")
	}
}

func TestSignalContextReturnsCancellableContext(t *testing.T) {
	ctx, stop := SignalContext()
	defer stop()

	if ctx.Err() != nil {
		t.Fatalf("SignalContext ctx.Err() = %v, want nil before stop", ctx.Err())
	}

	stop()

	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("SignalContext ctx did not cancel after stop()")
	}
}
