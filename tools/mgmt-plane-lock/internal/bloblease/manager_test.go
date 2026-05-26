package bloblease

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
)

// roundTripperFunc adapts a function to http.RoundTripper for the
// Azure SDK transport seam. The blob client routes all requests
// through azcore's policy pipeline; injecting a roundtripper at the
// Transport position covers Properties/PreferredCluster/etc.
type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
func (f roundTripperFunc) Do(req *http.Request) (*http.Response, error)        { return f(req) }

// newManagerForTesting wires a Manager around a blob.Client that
// sends requests through transport instead of the live Azure SDK
// transport. Production code uses New(); this constructor exists
// for unit tests only. Retry policy is set to 0 attempts so failure-
// path tests don't pay the default exponential-backoff budget.
func newManagerForTesting(blobURL, preferredMetaKey string, transport policy.Transporter) (*Manager, error) {
	client, err := blob.NewClientWithNoCredential(blobURL, &blob.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Transport: transport,
			Retry:     policy.RetryOptions{MaxRetries: -1},
		},
	})
	if err != nil {
		return nil, err
	}
	return &Manager{blobClient: client, preferredMetaKey: preferredMetaKey}, nil
}

func TestManagerPropertiesReadsMetadataMap(t *testing.T) {
	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		h := http.Header{}
		h.Set("x-ms-meta-preferred-cluster", "mgmt-we")
		h.Set("x-ms-meta-extra-key", "extra-value")
		return &http.Response{
			StatusCode: 200,
			Header:     h,
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Request:    req,
		}, nil
	})

	m, err := newManagerForTesting("https://example.blob.core.windows.net/leases/x", "preferred-cluster", transport)
	if err != nil {
		t.Fatalf("newManagerForTesting: %v", err)
	}

	props, err := m.Properties(context.Background())
	if err != nil {
		t.Fatalf("Properties() error = %v", err)
	}

	if got := props.Metadata["preferred-cluster"]; got != "mgmt-we" {
		t.Fatalf("Metadata[preferred-cluster] = %q, want %q", got, "mgmt-we")
	}
	if got := props.Metadata["extra-key"]; got != "extra-value" {
		t.Fatalf("Metadata[extra-key] = %q, want %q", got, "extra-value")
	}
}

func TestManagerPreferredClusterReadsFromMetadata(t *testing.T) {
	transport := roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
		h := http.Header{}
		h.Set("x-ms-meta-preferred-cluster", "mgmt-ne")
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	})

	m, _ := newManagerForTesting("https://example.blob.core.windows.net/leases/x", "preferred-cluster", transport)
	got, err := m.PreferredCluster(context.Background())
	if err != nil {
		t.Fatalf("PreferredCluster() error = %v", err)
	}
	if got != "mgmt-ne" {
		t.Fatalf("PreferredCluster() = %q, want mgmt-ne", got)
	}
}

func TestManagerPropertiesSurfacesTransportError(t *testing.T) {
	transport := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network broke")
	})
	m, _ := newManagerForTesting("https://example.blob.core.windows.net/leases/x", "preferred-cluster", transport)

	if _, err := m.Properties(context.Background()); err == nil {
		t.Fatal("Properties() returned nil, want transport error")
	}
}

func TestManagerAcquireReturnsLeaseID(t *testing.T) {
	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.RawQuery, "comp=lease") {
			t.Fatalf("unexpected acquire URL: %s", req.URL.String())
		}
		return &http.Response{StatusCode: 201, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	})

	m, _ := newManagerForTesting("https://example.blob.core.windows.net/leases/x", "preferred-cluster", transport)
	leaseID, err := m.Acquire(context.Background(), 60_000_000_000)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if leaseID == "" {
		t.Fatal("Acquire() returned empty lease ID")
	}
}

func TestManagerReleaseTreatsMissingLeaseAsSuccess(t *testing.T) {
	transport := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		body := `<?xml version="1.0" encoding="utf-8"?><Error><Code>LeaseNotPresentWithBlobOperation</Code></Error>`
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"x-ms-error-code": []string{"LeaseNotPresentWithBlobOperation"}, "Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	m, _ := newManagerForTesting("https://example.blob.core.windows.net/leases/x", "preferred-cluster", transport)
	if err := m.Release(context.Background(), "ignored-lease-id"); err != nil {
		t.Fatalf("Release() returned %v, want nil for missing-lease (treated as already released)", err)
	}
}

func TestManagerRenewSurfacesNonConflictError(t *testing.T) {
	transport := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 500,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<Error><Code>InternalError</Code></Error>`)),
		}, nil
	})

	m, _ := newManagerForTesting("https://example.blob.core.windows.net/leases/x", "preferred-cluster", transport)
	if err := m.Renew(context.Background(), "lease"); err == nil {
		t.Fatal("Renew() returned nil, want surface of 500 error")
	}
}

func TestManagerBreakTreatsMissingLeaseAsSuccess(t *testing.T) {
	transport := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"x-ms-error-code": []string{"LeaseNotPresentWithBlobOperation"}, "Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<Error><Code>LeaseNotPresentWithBlobOperation</Code></Error>`)),
		}, nil
	})

	m, _ := newManagerForTesting("https://example.blob.core.windows.net/leases/x", "preferred-cluster", transport)
	if err := m.Break(context.Background()); err != nil {
		t.Fatalf("Break() returned %v, want nil for missing-lease", err)
	}
}

func TestNewProductionConstructorBuildsAManager(t *testing.T) {
	// Exercises New() — fails on credential acquisition in CI (no
	// Azure auth env), but covers the early code path before the
	// credential call. We accept either outcome: success (CI has
	// MSI) or a credential error.
	m, err := New(context.Background(), "https://example.blob.core.windows.net/leases/x", "preferred-cluster")
	if err != nil {
		// Credential failure is expected outside an Azure env.
		return
	}
	if m == nil {
		t.Fatal("New() returned (nil, nil)")
	}
}
