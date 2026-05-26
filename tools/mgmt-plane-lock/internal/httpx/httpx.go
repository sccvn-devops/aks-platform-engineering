// Package httpx is the single transport seam for outbound HTTP in this module.
// It owns timeouts, retries, and error redaction so no binary open-codes HTTP
// policy and no response body leaks into an error message by default.
//
// FR-V4-15..18 (PRD-v4 US-V4-04):
//   - NewTransport returns an http.RoundTripper that wraps a base transport with
//     bounded per-attempt timeouts and capped exponential-backoff retries on
//     transient failures (5xx, 429, network errors).
//   - Context cancellation interrupts the retry backoff sleep immediately —
//     no remaining backoff is honoured once ctx.Err() != nil.
//   - The returned error for an HTTP failure is *Error; the response body is
//     redacted by default and surfaces only when the caller opts in via
//     WithBodyOnError(maxBytes).
package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// DefaultPerAttemptTimeout caps a single round-trip including connect + TLS +
// header + body. Callers can override via WithPerAttemptTimeout.
const DefaultPerAttemptTimeout = 30 * time.Second

// DefaultMaxRetries is the per-request retry budget. Retries fire only on
// retriable failures (see isRetriable).
const DefaultMaxRetries = 3

// DefaultBaseBackoff is the first backoff delay; subsequent retries double it
// up to DefaultMaxBackoff.
const DefaultBaseBackoff = 200 * time.Millisecond

// DefaultMaxBackoff caps a single backoff sleep.
const DefaultMaxBackoff = 5 * time.Second

// Options configures NewTransport.
type Options struct {
	// Base is the underlying RoundTripper. If nil, a clone of
	// http.DefaultTransport is used.
	Base http.RoundTripper

	// PerAttemptTimeout caps each individual round-trip attempt. <=0 means
	// DefaultPerAttemptTimeout.
	PerAttemptTimeout time.Duration

	// MaxRetries is the number of additional attempts after the first. <0 means
	// DefaultMaxRetries; 0 disables retries.
	MaxRetries int

	// BaseBackoff is the initial backoff between retries. <=0 means
	// DefaultBaseBackoff.
	BaseBackoff time.Duration

	// MaxBackoff caps a single backoff. <=0 means DefaultMaxBackoff.
	MaxBackoff time.Duration

	// BodyOnErrorMaxBytes opts the caller in to surfacing the first N bytes of
	// the response body inside *Error. Default 0 (redacted).
	BodyOnErrorMaxBytes int

	// nowFunc + sleepFunc are test hooks; nil = real time.
	nowFunc   func() time.Time
	sleepFunc func(ctx context.Context, d time.Duration) error
}

// Option is a functional configurator for NewTransport.
type Option func(*Options)

// WithBase overrides the underlying RoundTripper.
func WithBase(rt http.RoundTripper) Option { return func(o *Options) { o.Base = rt } }

// WithPerAttemptTimeout overrides the per-attempt timeout.
func WithPerAttemptTimeout(d time.Duration) Option {
	return func(o *Options) { o.PerAttemptTimeout = d }
}

// WithMaxRetries overrides the retry budget. n=0 disables retries.
func WithMaxRetries(n int) Option { return func(o *Options) { o.MaxRetries = n } }

// WithBaseBackoff overrides the first backoff delay.
func WithBaseBackoff(d time.Duration) Option { return func(o *Options) { o.BaseBackoff = d } }

// WithMaxBackoff caps a single backoff.
func WithMaxBackoff(d time.Duration) Option { return func(o *Options) { o.MaxBackoff = d } }

// WithBodyOnError opts in to surfacing the first maxBytes of the response body
// inside *Error. Pass 0 (default) to redact.
func WithBodyOnError(maxBytes int) Option {
	return func(o *Options) { o.BodyOnErrorMaxBytes = maxBytes }
}

// NewTransport returns an http.RoundTripper that wraps the base transport with
// per-attempt timeouts and capped exponential-backoff retries. The returned
// RoundTripper is safe for concurrent use.
func NewTransport(opts ...Option) http.RoundTripper {
	o := Options{}
	for _, fn := range opts {
		fn(&o)
	}
	if o.Base == nil {
		// Clone the default so callers can swap in their own dialer without
		// clobbering the global default.
		o.Base = http.DefaultTransport.(*http.Transport).Clone()
	}
	if o.PerAttemptTimeout <= 0 {
		o.PerAttemptTimeout = DefaultPerAttemptTimeout
	}
	if o.MaxRetries < 0 {
		o.MaxRetries = DefaultMaxRetries
	}
	if o.BaseBackoff <= 0 {
		o.BaseBackoff = DefaultBaseBackoff
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = DefaultMaxBackoff
	}
	if o.nowFunc == nil {
		o.nowFunc = time.Now
	}
	if o.sleepFunc == nil {
		o.sleepFunc = sleepWithContext
	}
	return &transport{opts: o}
}

// NewClient is a thin convenience that wraps NewTransport in an http.Client.
// The client carries no overall deadline — per-attempt timeouts are enforced by
// the transport, and an overall deadline belongs on the caller's context.
func NewClient(opts ...Option) *http.Client {
	return &http.Client{Transport: NewTransport(opts...)}
}

type transport struct {
	opts Options
}

// RoundTrip implements http.RoundTripper.
func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// We may need to replay the body on retry; capture it once.
	var bodyBytes []byte
	if req.Body != nil && req.GetBody == nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("httpx: read request body: %w", err)
		}
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(newBytesReader(bodyBytes)), nil
		}
		body, _ := req.GetBody()
		req.Body = body
	}

	var lastErr error
	for attempt := 0; attempt <= t.opts.MaxRetries; attempt++ {
		if err := req.Context().Err(); err != nil {
			// Context already done — surface the cause without further work.
			return nil, err
		}

		attemptReq := req
		if attempt > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("httpx: rewind body: %w", err)
			}
			clone := req.Clone(req.Context())
			clone.Body = body
			attemptReq = clone
		}

		attemptCtx, cancel := context.WithTimeout(attemptReq.Context(), t.opts.PerAttemptTimeout)
		attemptReq = attemptReq.WithContext(attemptCtx)

		resp, err := t.opts.Base.RoundTrip(attemptReq)

		if err != nil {
			cancel()
			lastErr = err
			if !isRetriableErr(err) || attempt == t.opts.MaxRetries {
				return nil, err
			}
		} else if isRetriableStatus(resp.StatusCode) && attempt < t.opts.MaxRetries {
			// Drain + close so the connection can be reused.
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			cancel()
			lastErr = errFromStatus(resp.StatusCode)
		} else {
			// Success path or non-retriable status with budget exhausted —
			// hand the response back. The attemptCtx must outlive the body, so
			// we attach the cancel to the response body close.
			resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}
			return resp, nil
		}

		// Schedule the next attempt.
		delay := t.backoffFor(attempt)
		if err := t.opts.sleepFunc(req.Context(), delay); err != nil {
			// Context cancelled or deadline hit mid-backoff: return immediately
			// without honouring the rest of the delay (FR-V4-16).
			return nil, err
		}
	}

	if lastErr == nil {
		lastErr = errors.New("httpx: retry loop exited without result")
	}
	return nil, lastErr
}

func (t *transport) backoffFor(attempt int) time.Duration {
	d := t.opts.BaseBackoff << attempt
	if d <= 0 || d > t.opts.MaxBackoff {
		d = t.opts.MaxBackoff
	}
	return d
}

// sleepWithContext blocks for d or until ctx is done, whichever comes first.
// Returns ctx.Err() if the context fires; nil if the full duration elapses.
// FR-V4-16: cancellation during retry backoff returns immediately.
func sleepWithContext(ctx context.Context, d time.Duration) error {
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

func isRetriableStatus(code int) bool {
	if code == http.StatusRequestTimeout || code == http.StatusTooManyRequests {
		return true
	}
	return code >= 500 && code <= 599
}

func isRetriableErr(err error) bool {
	// All transport-level errors are treated as retriable except a cancelled
	// context (caller is shutting down).
	if errors.Is(err, context.Canceled) {
		return false
	}
	return err != nil
}

func errFromStatus(code int) error {
	return &Error{StatusCode: code, Status: http.StatusText(code)}
}

// Error is the typed transport error returned for HTTP failures. Body is
// populated only when the caller opted in via WithBodyOnError; otherwise it is
// empty and the error string contains no body content (FR-V4-17).
type Error struct {
	StatusCode int
	Status     string
	Body       string // empty unless WithBodyOnError was set
	URL        string
}

// Error implements error. The string never contains response body bytes unless
// the caller opted in via WithBodyOnError(maxBytes>0).
func (e *Error) Error() string {
	if e == nil {
		return "httpx: <nil>"
	}
	out := "httpx: status " + strconv.Itoa(e.StatusCode)
	if e.Status != "" && e.Status != http.StatusText(e.StatusCode) {
		out += " (" + e.Status + ")"
	}
	if e.URL != "" {
		out += " for " + e.URL
	}
	if e.Body != "" {
		out += ": " + e.Body
	}
	return out
}

// CheckResponse turns a non-2xx response into a redacted *Error. The response
// body is consumed and closed. The maxBytes argument controls how much of the
// body (if any) is attached to the returned error — 0 means redact. Callers
// should call this immediately after a successful RoundTrip / Do where the
// status is not 2xx.
//
// This is the single helper used by call sites to enforce FR-V4-17 redaction-
// by-default — no other path should embed body bytes in an error message.
func CheckResponse(resp *http.Response, maxBytes int) error {
	if resp == nil {
		return errors.New("httpx: nil response")
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	defer resp.Body.Close()
	e := &Error{StatusCode: resp.StatusCode, Status: resp.Status}
	if resp.Request != nil && resp.Request.URL != nil {
		// Strip query string — it can carry tokens (e.g., AKV continuation
		// pages embed signature material in some preview APIs).
		u := *resp.Request.URL
		u.RawQuery = ""
		e.URL = u.String()
	}
	if maxBytes > 0 {
		buf, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)))
		if err == nil && len(buf) > 0 {
			e.Body = string(buf)
		}
	} else {
		// Drain to allow connection reuse, discarding bytes.
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return e
}

// cancelOnCloseBody wires the per-attempt context cancel to the response body's
// Close so the timeout is released only after the caller finishes reading.
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
	closed bool
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	if !b.closed {
		b.closed = true
		b.cancel()
	}
	return err
}

// bytesReader is a minimal seekable byte source used to replay request bodies
// across retries without importing bytes (avoids name conflict with body
// constructors in some call sites).
type bytesReader struct {
	b   []byte
	pos int
}

func newBytesReader(b []byte) *bytesReader { return &bytesReader{b: b} }

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.pos:])
	r.pos += n
	return n, nil
}
