package akvwriter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/httpx"
)

// stubCred is an azcore.TokenCredential that returns a static token.
type stubCred struct {
	err error
}

func (s *stubCred) GetToken(ctx context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	if s.err != nil {
		return azcore.AccessToken{}, s.err
	}
	return azcore.AccessToken{Token: "fake-token", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

// stubTransport routes every request through a user-supplied handler. It also
// captures the requests for assertion.
type stubTransport struct {
	handler func(req *http.Request) (*http.Response, error)
	calls   []*http.Request
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.calls = append(s.calls, req.Clone(req.Context()))
	return s.handler(req)
}

func newResp(status int, body string, req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
		Request:    req,
	}
}

// newTestClient wires a Client to (a) stubCred and (b) a *http.Client backed by
// an httpx transport configured for fast tests (no retries, no backoff).
func newTestClient(t *testing.T, handler func(req *http.Request) (*http.Response, error)) (*Client, *stubTransport) {
	t.Helper()
	stub := &stubTransport{handler: handler}
	rt := httpx.NewTransport(
		httpx.WithBase(stub),
		httpx.WithMaxRetries(0),
		httpx.WithPerAttemptTimeout(2*time.Second),
	)
	hc := &http.Client{Transport: rt}
	c := newWithDeps("https://vault.example", &stubCred{}, hc)
	return c, stub
}

func TestPut_HappyPath(t *testing.T) {
	c, stub := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPut {
			t.Fatalf("want PUT, got %s", req.Method)
		}
		if got := req.URL.Path; got != "/secrets/tok" {
			t.Fatalf("path: %q", got)
		}
		if q := req.URL.Query().Get("api-version"); q != apiVersion {
			t.Fatalf("api-version: %q", q)
		}
		if auth := req.Header.Get("Authorization"); auth != "Bearer fake-token" {
			t.Fatalf("auth header: %q", auth)
		}
		return newResp(200, `{"id":"x","value":"v"}`, req), nil
	})
	if err := c.Put(context.Background(), "tok", "v", Overwrite); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if len(stub.calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(stub.calls))
	}
}

func TestPut_AuthFailed_401(t *testing.T) {
	c, _ := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		return newResp(401, `{"error":"unauthorized"}`, req), nil
	})
	err := c.Put(context.Background(), "tok", "v", Overwrite)
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("want ErrAuthFailed, got %v", err)
	}
}

func TestPut_AuthFailed_403(t *testing.T) {
	c, _ := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		return newResp(403, `{"error":"forbidden"}`, req), nil
	})
	err := c.Put(context.Background(), "tok", "v", Overwrite)
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("want ErrAuthFailed, got %v", err)
	}
}

func TestPut_SoftDeleted_Overwrite_ReturnsTypedError(t *testing.T) {
	c, stub := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case http.MethodPut:
			return newResp(409, `{"error":{"code":"Conflict","message":"deleted but recoverable"}}`, req), nil
		case http.MethodGet:
			if !strings.HasPrefix(req.URL.Path, "/deletedsecrets/") {
				t.Fatalf("unexpected GET path: %q", req.URL.Path)
			}
			return newResp(200, `{"recoveryId":"x"}`, req), nil
		}
		t.Fatalf("unexpected method: %s", req.Method)
		return nil, nil
	})
	err := c.Put(context.Background(), "tok", "v", Overwrite)
	if !errors.Is(err, ErrSoftDeletedSecretExists) {
		t.Fatalf("want ErrSoftDeletedSecretExists, got %v", err)
	}
	// PUT then GET-deleted; no recover, no second PUT.
	if got, want := len(stub.calls), 2; got != want {
		t.Fatalf("call count: got %d want %d", got, want)
	}
}

func TestPut_SoftDeleted_RecoverWritesNewValue(t *testing.T) {
	puts := 0
	recovers := 0
	probes := 0
	c, stub := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodPut:
			puts++
			if puts == 1 {
				return newResp(409, `{"error":"conflict"}`, req), nil
			}
			return newResp(200, `{"id":"x"}`, req), nil
		case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/deletedsecrets/"):
			probes++
			return newResp(200, `{"recoveryId":"x"}`, req), nil
		case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/recover"):
			recovers++
			return newResp(200, `{"recovered":true}`, req), nil
		}
		t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
		return nil, nil
	})
	if err := c.Put(context.Background(), "tok", "v", RecoverIfSoftDeleted); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if puts != 2 || probes != 1 || recovers != 1 {
		t.Fatalf("call shape: puts=%d probes=%d recovers=%d", puts, probes, recovers)
	}
	if got := len(stub.calls); got != 4 {
		t.Fatalf("total calls: %d", got)
	}
}

func TestPut_Conflict_NotSoftDeleted(t *testing.T) {
	c, _ := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodPut:
			return newResp(409, `{"error":"name in use"}`, req), nil
		case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/deletedsecrets/"):
			return newResp(404, `{"error":"not deleted"}`, req), nil
		}
		t.Fatalf("unexpected: %s %s", req.Method, req.URL.Path)
		return nil, nil
	})
	err := c.Put(context.Background(), "tok", "v", RecoverIfSoftDeleted)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	if errors.Is(err, ErrSoftDeletedSecretExists) {
		t.Fatalf("must not classify as soft-deleted")
	}
}

func TestPut_5xx_ReturnsRedactedError(t *testing.T) {
	const secretPayload = "TOPSECRETBODY"
	c, _ := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		return newResp(500, secretPayload, req), nil
	})
	err := c.Put(context.Background(), "tok", "v", Overwrite)
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), secretPayload) {
		t.Fatalf("error string leaked body bytes: %q", err.Error())
	}
	var he *httpx.Error
	if !errors.As(err, &he) {
		t.Fatalf("want *httpx.Error, got %T", err)
	}
	if he.StatusCode != 500 {
		t.Fatalf("status: %d", he.StatusCode)
	}
	if he.Body != "" {
		t.Fatalf("body must be empty by default, got %q", he.Body)
	}
	// URL must be query-stripped (FR-V4-17 — api-version etc. dropped).
	if u, perr := url.Parse(he.URL); perr != nil || u.RawQuery != "" {
		t.Fatalf("URL query not stripped: %q", he.URL)
	}
}

func TestGet_HappyPath(t *testing.T) {
	updated := time.Now().Truncate(time.Second).Unix()
	c, _ := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		body := fmt.Sprintf(`{"value":"hunter2","attributes":{"enabled":true,"updated":%d}}`, updated)
		return newResp(200, body, req), nil
	})
	got, err := c.Get(context.Background(), "tok")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Value != "hunter2" || !got.Enabled || got.UpdatedAt.Unix() != updated {
		t.Fatalf("got %+v", got)
	}
}

func TestGet_NotFound(t *testing.T) {
	c, _ := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		return newResp(404, `{"error":"not found"}`, req), nil
	})
	_, err := c.Get(context.Background(), "missing")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("want ErrSecretNotFound, got %v", err)
	}
}

func TestGet_AuthFailed(t *testing.T) {
	c, _ := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		return newResp(401, `{"error":"unauth"}`, req), nil
	})
	_, err := c.Get(context.Background(), "tok")
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("want ErrAuthFailed, got %v", err)
	}
}

func TestGet_TokenAcquisitionFailureSurfaces(t *testing.T) {
	stub := &stubTransport{handler: func(req *http.Request) (*http.Response, error) {
		t.Fatal("HTTP must not be reached when token acquisition fails")
		return nil, nil
	}}
	hc := &http.Client{Transport: httpx.NewTransport(httpx.WithBase(stub), httpx.WithMaxRetries(0))}
	c := newWithDeps("https://vault.example", &stubCred{err: errors.New("aad refused")}, hc)
	if _, err := c.Get(context.Background(), "tok"); err == nil {
		t.Fatal("want error")
	}
	if err := c.Put(context.Background(), "tok", "v", Overwrite); err == nil {
		t.Fatal("want error")
	}
}

func TestGetSecretUpdatedAt(t *testing.T) {
	updated := time.Now().Truncate(time.Second).Unix()
	c, _ := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		body := fmt.Sprintf(`{"value":"v","attributes":{"enabled":true,"updated":%d}}`, updated)
		return newResp(200, body, req), nil
	})
	got, err := c.GetSecretUpdatedAt(context.Background(), "tok")
	if err != nil {
		t.Fatalf("GetSecretUpdatedAt: %v", err)
	}
	if got.Unix() != updated {
		t.Fatalf("ts: got %d want %d", got.Unix(), updated)
	}
}

func TestGetSecretUpdatedAt_NotFound(t *testing.T) {
	c, _ := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		return newResp(404, `{}`, req), nil
	})
	_, err := c.GetSecretUpdatedAt(context.Background(), "tok")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("want ErrSecretNotFound, got %v", err)
	}
}

func TestDisableOldVersions_SkipsWhenLessThanTwo(t *testing.T) {
	c, stub := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", req.Method)
		}
		return newResp(200, `{"value":[{"id":"v1","attributes":{"enabled":true}}]}`, req), nil
	})
	if err := c.DisableOldVersions(context.Background(), "tok"); err != nil {
		t.Fatalf("DisableOldVersions: %v", err)
	}
	if len(stub.calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(stub.calls))
	}
}

func TestDisableOldVersions_PatchesAllButLatest(t *testing.T) {
	patched := []string{}
	c, _ := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case http.MethodGet:
			body := `{"value":[` +
				`{"id":"https://vault.example/secrets/tok/v1","attributes":{"enabled":true}},` +
				`{"id":"https://vault.example/secrets/tok/v2","attributes":{"enabled":true}},` +
				`{"id":"https://vault.example/secrets/tok/v3","attributes":{"enabled":true}}` +
				`]}`
			return newResp(200, body, req), nil
		case http.MethodPatch:
			var payload map[string]any
			b, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(b, &payload)
			patched = append(patched, req.URL.Path)
			return newResp(200, `{}`, req), nil
		}
		t.Fatalf("unexpected: %s", req.Method)
		return nil, nil
	})
	if err := c.DisableOldVersions(context.Background(), "tok"); err != nil {
		t.Fatalf("DisableOldVersions: %v", err)
	}
	// v3 is newest (last); v1 + v2 should be patched.
	if got, want := len(patched), 2; got != want {
		t.Fatalf("patch count: got %d want %d (%v)", got, want, patched)
	}
}

func TestDisableOldVersions_PatchAuthFails(t *testing.T) {
	c, _ := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case http.MethodGet:
			body := `{"value":[` +
				`{"id":"https://vault.example/secrets/tok/v1","attributes":{"enabled":true}},` +
				`{"id":"https://vault.example/secrets/tok/v2","attributes":{"enabled":true}}` +
				`]}`
			return newResp(200, body, req), nil
		case http.MethodPatch:
			return newResp(401, `{}`, req), nil
		}
		t.Fatalf("unexpected: %s", req.Method)
		return nil, nil
	})
	err := c.DisableOldVersions(context.Background(), "tok")
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("want ErrAuthFailed (wrapped), got %v", err)
	}
}

func TestDisableOldVersions_ListAuthFails(t *testing.T) {
	c, _ := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		return newResp(403, `{}`, req), nil
	})
	err := c.DisableOldVersions(context.Background(), "tok")
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("want ErrAuthFailed, got %v", err)
	}
}

func TestPut_ProbeFailureFallsBackToOriginalConflict(t *testing.T) {
	c, _ := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodPut:
			return newResp(409, `{}`, req), nil
		case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/deletedsecrets/"):
			// Unexpected non-200/404 status — probe should fail, Put surfaces
			// the original ErrConflict rather than the less-actionable probe error.
			return newResp(503, `{}`, req), nil
		}
		t.Fatalf("unexpected: %s %s", req.Method, req.URL.Path)
		return nil, nil
	})
	err := c.Put(context.Background(), "tok", "v", RecoverIfSoftDeleted)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestPut_RecoverFailurePropagates(t *testing.T) {
	c, _ := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodPut:
			return newResp(409, `{}`, req), nil
		case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/deletedsecrets/"):
			return newResp(200, `{}`, req), nil
		case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/recover"):
			return newResp(403, `{}`, req), nil
		}
		t.Fatalf("unexpected: %s %s", req.Method, req.URL.Path)
		return nil, nil
	})
	err := c.Put(context.Background(), "tok", "v", RecoverIfSoftDeleted)
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("want ErrAuthFailed (wrapped through recover failure), got %v", err)
	}
}

func TestErrorSentinelsAreDistinct(t *testing.T) {
	// Guard against accidental sentinel aliasing.
	sentinels := []error{ErrSecretNotFound, ErrAuthFailed, ErrConflict, ErrSoftDeletedSecretExists}
	seen := map[string]bool{}
	for _, e := range sentinels {
		if seen[e.Error()] {
			t.Fatalf("sentinel collision: %q", e.Error())
		}
		seen[e.Error()] = true
	}
}
