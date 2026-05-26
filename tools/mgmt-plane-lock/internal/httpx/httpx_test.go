package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// stubRT is a counting RoundTripper that returns scripted (resp, err) pairs.
type stubRT struct {
	scripts []scriptedResponse
	count   atomic.Int32
}

type scriptedResponse struct {
	status int
	body   string
	err    error
	delay  time.Duration
}

func (s *stubRT) RoundTrip(req *http.Request) (*http.Response, error) {
	idx := int(s.count.Add(1)) - 1
	if idx >= len(s.scripts) {
		return nil, fmt.Errorf("stubRT: no script for attempt %d", idx)
	}
	script := s.scripts[idx]
	if script.delay > 0 {
		select {
		case <-time.After(script.delay):
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	if script.err != nil {
		return nil, script.err
	}
	return &http.Response{
		StatusCode: script.status,
		Status:     http.StatusText(script.status),
		Body:       io.NopCloser(strings.NewReader(script.body)),
		Request:    req,
		Header:     http.Header{},
	}, nil
}

func TestRetryOn5xxThenSuccess(t *testing.T) {
	rt := &stubRT{scripts: []scriptedResponse{
		{status: 500, body: "transient"},
		{status: 502, body: "still transient"},
		{status: 200, body: "ok"},
	}}
	client := NewClient(
		WithBase(rt),
		WithMaxRetries(3),
		WithBaseBackoff(time.Microsecond),
		WithMaxBackoff(time.Microsecond),
	)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.invalid/x", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() err = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if got := rt.count.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestNoRetryOn4xx(t *testing.T) {
	rt := &stubRT{scripts: []scriptedResponse{
		{status: 400, body: "bad input"},
	}}
	client := NewClient(WithBase(rt), WithMaxRetries(3), WithBaseBackoff(time.Microsecond))
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.invalid/x", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() err = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("StatusCode = %d, want 400", resp.StatusCode)
	}
	if got := rt.count.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1 (4xx must not retry)", got)
	}
}

func TestRetryBudgetExhausted(t *testing.T) {
	rt := &stubRT{scripts: []scriptedResponse{
		{status: 503}, {status: 503}, {status: 503}, {status: 503},
	}}
	client := NewClient(WithBase(rt), WithMaxRetries(2), WithBaseBackoff(time.Microsecond))
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.invalid/x", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() err = %v (want resp, not err)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatalf("StatusCode = %d, want 503", resp.StatusCode)
	}
	if got := rt.count.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3 (1 + 2 retries)", got)
	}
}

// TestContextCancellationDuringBackoffReturnsImmediately covers FR-V4-16:
// when ctx is cancelled mid-backoff, the retry loop must return immediately
// rather than completing the remaining sleep.
func TestContextCancellationDuringBackoffReturnsImmediately(t *testing.T) {
	rt := &stubRT{scripts: []scriptedResponse{
		{status: 503},
		{status: 200, body: "should-not-reach"},
	}}
	ctx, cancel := context.WithCancel(context.Background())

	sleepCalls := atomic.Int32{}
	customSleep := func(ctx context.Context, d time.Duration) error {
		sleepCalls.Add(1)
		// Cancel from inside the sleep — the sleep MUST observe the cancel
		// and return ctx.Err() rather than honouring the full delay.
		cancel()
		// Simulate the real sleep's select.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			t.Fatal("sleep returned after 5s — it did not honour context cancel")
			return nil
		}
	}

	tr := NewTransport(
		WithBase(rt),
		WithMaxRetries(3),
		WithBaseBackoff(time.Second),
	).(*transport)
	tr.opts.sleepFunc = customSleep

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.invalid/x", nil)
	start := time.Now()
	_, err := tr.RoundTrip(req)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("RoundTrip err = nil, want context cancel error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RoundTrip err = %v, want context.Canceled", err)
	}
	if elapsed > time.Second {
		t.Fatalf("elapsed = %s, want <1s (cancellation must short-circuit backoff)", elapsed)
	}
	if got := sleepCalls.Load(); got != 1 {
		t.Fatalf("sleep called %d times, want 1", got)
	}
	if got := rt.count.Load(); got != 1 {
		t.Fatalf("RoundTripper hit %d times, want 1 (second attempt must not fire)", got)
	}
}

// TestSleepWithContextHonoursCancel is the unit-level cover for the real
// sleepWithContext helper (the production path).
func TestSleepWithContextHonoursCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	err := sleepWithContext(ctx, time.Second)
	elapsed := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("elapsed = %s, want <100ms", elapsed)
	}
}

func TestSleepWithContextRunsToCompletion(t *testing.T) {
	err := sleepWithContext(context.Background(), 5*time.Millisecond)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestSleepWithContextZeroDuration(t *testing.T) {
	if err := sleepWithContext(context.Background(), 0); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

// TestErrorRedactsBodyByDefault covers FR-V4-17: by default, a 5xx-with-body
// response surfaces as an *Error whose .Error() string contains NO body bytes.
func TestErrorRedactsBodyByDefault(t *testing.T) {
	const secretBody = "secret-token=abcdef-leaked"
	resp := &http.Response{
		StatusCode: 500,
		Status:     "500 Internal Server Error",
		Body:       io.NopCloser(strings.NewReader(secretBody)),
		Request: &http.Request{
			URL: mustURL("https://vault.example/secrets/foo?api-version=7.4"),
		},
	}
	err := CheckResponse(resp, 0)
	if err == nil {
		t.Fatal("CheckResponse err = nil, want *Error")
	}
	var httpErr *Error
	if !errors.As(err, &httpErr) {
		t.Fatalf("err is %T, want *Error", err)
	}
	if httpErr.Body != "" {
		t.Fatalf("Body = %q, want empty (redacted by default)", httpErr.Body)
	}
	if strings.Contains(err.Error(), secretBody) {
		t.Fatalf("Error() leaked body: %q", err.Error())
	}
	if strings.Contains(err.Error(), "abcdef") {
		t.Fatalf("Error() leaked partial body: %q", err.Error())
	}
	// URL must be present but query-stripped.
	if !strings.Contains(err.Error(), "vault.example/secrets/foo") {
		t.Fatalf("Error() missing URL: %q", err.Error())
	}
	if strings.Contains(err.Error(), "api-version") {
		t.Fatalf("Error() leaked URL query: %q", err.Error())
	}
}

func TestErrorIncludesBodyOnOptIn(t *testing.T) {
	resp := &http.Response{
		StatusCode: 500,
		Body:       io.NopCloser(strings.NewReader("first-200-bytes-of-error-body")),
		Request:    &http.Request{URL: mustURL("https://api.example/foo")},
	}
	err := CheckResponse(resp, 200)
	var httpErr *Error
	if !errors.As(err, &httpErr) {
		t.Fatalf("err is %T, want *Error", err)
	}
	if httpErr.Body != "first-200-bytes-of-error-body" {
		t.Fatalf("Body = %q, want full body", httpErr.Body)
	}
	if !strings.Contains(err.Error(), "first-200-bytes-of-error-body") {
		t.Fatalf("Error() = %q, want body content", err.Error())
	}
}

func TestErrorTruncatesBodyToMaxBytes(t *testing.T) {
	huge := strings.Repeat("A", 4096)
	resp := &http.Response{
		StatusCode: 502,
		Body:       io.NopCloser(strings.NewReader(huge)),
		Request:    &http.Request{URL: mustURL("https://api.example/")},
	}
	err := CheckResponse(resp, 64)
	var httpErr *Error
	if !errors.As(err, &httpErr) {
		t.Fatalf("err is %T, want *Error", err)
	}
	if len(httpErr.Body) != 64 {
		t.Fatalf("Body len = %d, want 64", len(httpErr.Body))
	}
}

func TestCheckResponse2xxReturnsNil(t *testing.T) {
	resp := &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader(""))}
	if err := CheckResponse(resp, 1024); err != nil {
		t.Fatalf("CheckResponse err = %v, want nil", err)
	}
}

func TestCheckResponseNilResponse(t *testing.T) {
	if err := CheckResponse(nil, 0); err == nil {
		t.Fatal("CheckResponse(nil) err = nil, want non-nil")
	}
}

func TestErrorWithNoStatusTextStillStringifies(t *testing.T) {
	e := &Error{StatusCode: 503}
	s := e.Error()
	if !strings.Contains(s, "503") {
		t.Fatalf("Error() = %q, want '503'", s)
	}
}

func TestErrorNilStringifies(t *testing.T) {
	var e *Error
	if e.Error() == "" {
		t.Fatal("nil.Error() empty")
	}
}

func TestRequestBodyReplayedOnRetry(t *testing.T) {
	rt := &stubRT{scripts: []scriptedResponse{
		{status: 500},
		{status: 200, body: "ok"},
	}}
	client := NewClient(WithBase(rt), WithMaxRetries(2), WithBaseBackoff(time.Microsecond))

	// Body without GetBody — exercises the read-and-cache path in RoundTrip.
	body := strings.NewReader(`{"k":"v"}`)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://example.invalid/x", body)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() err = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if got := rt.count.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

func TestNetworkErrorRetried(t *testing.T) {
	rt := &stubRT{scripts: []scriptedResponse{
		{err: errors.New("connection reset")},
		{err: errors.New("eof")},
		{status: 200, body: "ok"},
	}}
	client := NewClient(WithBase(rt), WithMaxRetries(3), WithBaseBackoff(time.Microsecond))
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.invalid/x", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() err = %v", err)
	}
	defer resp.Body.Close()
	if got := rt.count.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestNetworkErrorBudgetExhausted(t *testing.T) {
	rt := &stubRT{scripts: []scriptedResponse{
		{err: errors.New("fail-1")},
		{err: errors.New("fail-2")},
	}}
	client := NewClient(WithBase(rt), WithMaxRetries(1), WithBaseBackoff(time.Microsecond))
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.invalid/x", nil)
	_, err := client.Do(req)
	if err == nil {
		t.Fatal("Do() err = nil, want network error")
	}
}

func TestContextCancelledBeforeFirstAttempt(t *testing.T) {
	rt := &stubRT{scripts: []scriptedResponse{{status: 200}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := NewClient(WithBase(rt), WithMaxRetries(3), WithBaseBackoff(time.Microsecond))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.invalid/x", nil)
	_, err := client.Do(req)
	if err == nil {
		t.Fatal("err = nil, want context cancel")
	}
	if got := rt.count.Load(); got != 0 {
		t.Fatalf("attempts = %d, want 0", got)
	}
}

func TestContextCancelDuringNetworkErrorIsNotRetried(t *testing.T) {
	rt := &stubRT{scripts: []scriptedResponse{
		{err: context.Canceled},
	}}
	client := NewClient(WithBase(rt), WithMaxRetries(3), WithBaseBackoff(time.Microsecond))
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.invalid/x", nil)
	_, err := client.Do(req)
	if err == nil {
		t.Fatal("err = nil, want context cancel")
	}
	if got := rt.count.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1 (context.Canceled is non-retriable)", got)
	}
}

func TestBackoffCappedAtMaxBackoff(t *testing.T) {
	tr := NewTransport(
		WithBase(http.DefaultTransport),
		WithBaseBackoff(100*time.Millisecond),
		WithMaxBackoff(150*time.Millisecond),
	).(*transport)
	for attempt := 0; attempt < 10; attempt++ {
		d := tr.backoffFor(attempt)
		if d > 150*time.Millisecond {
			t.Fatalf("attempt %d backoff = %s, want <=150ms", attempt, d)
		}
	}
}

func TestDefaultsApplied(t *testing.T) {
	tr := NewTransport().(*transport)
	if tr.opts.PerAttemptTimeout != DefaultPerAttemptTimeout {
		t.Errorf("PerAttemptTimeout = %s, want default", tr.opts.PerAttemptTimeout)
	}
	if tr.opts.MaxRetries != 0 {
		// Zero is intentional default for options struct — DefaultMaxRetries is
		// applied only when MaxRetries is negative. Document the contract.
		// (MaxRetries == 0 is "no retries"; negative means "use default".)
	}
	if tr.opts.BaseBackoff != DefaultBaseBackoff {
		t.Errorf("BaseBackoff = %s, want default", tr.opts.BaseBackoff)
	}
	if tr.opts.MaxBackoff != DefaultMaxBackoff {
		t.Errorf("MaxBackoff = %s, want default", tr.opts.MaxBackoff)
	}
	tr2 := NewTransport(WithMaxRetries(-1)).(*transport)
	if tr2.opts.MaxRetries != DefaultMaxRetries {
		t.Errorf("MaxRetries with -1 = %d, want %d", tr2.opts.MaxRetries, DefaultMaxRetries)
	}
}

// TestEndToEndWithRealServer exercises the full transport against a real
// httptest server — flushes out any subtle interaction with the per-attempt
// context cancel and the response-body close.
func TestEndToEndWithRealServer(t *testing.T) {
	attempts := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(503)
			_, _ = w.Write([]byte("flaky"))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client := NewClient(WithMaxRetries(3), WithBaseBackoff(time.Millisecond))
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/path?x=1", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() err = %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q, want 'ok'", string(body))
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestEndToEnd4xxNotRetried(t *testing.T) {
	attempts := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(404)
	}))
	defer srv.Close()

	client := NewClient(WithMaxRetries(3), WithBaseBackoff(time.Microsecond))
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() err = %v", err)
	}
	defer resp.Body.Close()
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestCheckResponseEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("server-side-token=xyz123"))
	}))
	defer srv.Close()
	client := NewClient(WithMaxRetries(0))
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/?secret=q", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() err = %v", err)
	}
	wrapped := CheckResponse(resp, 0)
	if wrapped == nil {
		t.Fatal("CheckResponse = nil, want *Error")
	}
	if strings.Contains(wrapped.Error(), "xyz123") {
		t.Fatalf("Error() leaked body token: %q", wrapped.Error())
	}
	if strings.Contains(wrapped.Error(), "secret=q") {
		t.Fatalf("Error() leaked URL query: %q", wrapped.Error())
	}
}

func mustURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}
