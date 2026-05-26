// Package akvwriter is the single AKV write path for the mgmt-plane-lock
// binary fleet. It exposes a typed-error API over Azure Key Vault's REST surface
// and runs all outbound HTTP through internal/httpx so timeouts, retries, and
// redaction policy are uniform (FR-V4-15..22 / US-V4-04 + US-V4-05).
//
// Typed errors (matched via errors.Is) — FR-V4-20:
//
//	ErrSecretNotFound          — secret does not exist (404)
//	ErrAuthFailed              — AKV refused credential (401 or 403)
//	ErrConflict                — generic 409 not attributable to soft-deletion
//	ErrSoftDeletedSecretExists — destination exists in deleted-but-recoverable
//	                             state and Strategy = Overwrite
//
// Put semantics — FR-V4-19:
//
//	Strategy = Overwrite             — fail with ErrSoftDeletedSecretExists when
//	                                   the destination is soft-deleted (default;
//	                                   prevents silent revival).
//	Strategy = RecoverIfSoftDeleted  — recover the deleted secret, then write.
package akvwriter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/httpx"
)

const (
	akvScope   = "https://vault.azure.net/.default"
	apiVersion = "7.4"
)

// Strategy controls Put behaviour when the destination secret is soft-deleted.
type Strategy int

const (
	// Overwrite refuses to revive a soft-deleted secret — it returns
	// ErrSoftDeletedSecretExists so the caller can decide whether to purge
	// or recover deliberately. This is the safe default.
	Overwrite Strategy = iota
	// RecoverIfSoftDeleted recovers the deleted secret first, then writes
	// the new value into the recovered slot.
	RecoverIfSoftDeleted
)

// Typed sentinel errors. Callers test these with errors.Is.
var (
	ErrSecretNotFound          = errors.New("akvwriter: secret not found")
	ErrAuthFailed              = errors.New("akvwriter: authentication failed")
	ErrConflict                = errors.New("akvwriter: conflict")
	ErrSoftDeletedSecretExists = errors.New("akvwriter: secret is soft-deleted but recoverable")
)

// Secret is the projection returned by Get.
type Secret struct {
	Name      string
	Value     string
	UpdatedAt time.Time
	Enabled   bool
}

// SecretStore is the Put/Get surface implemented by both Client (real AKV) and
// the in-memory fake in fake_test.go. Consumers should program to this
// interface so unit tests can substitute the fake.
type SecretStore interface {
	Put(ctx context.Context, name, value string, strategy Strategy) error
	Get(ctx context.Context, name string) (Secret, error)
}

// Client writes secrets to Azure Key Vault over the REST API.
type Client struct {
	vaultURL   string
	httpClient *http.Client
	credential azcore.TokenCredential
}

// Compile-time assertion that *Client satisfies SecretStore.
var _ SecretStore = (*Client)(nil)

// New creates a Client for the given vault URL using the default Azure
// credential chain. The HTTP client comes from httpx.NewClient so retries +
// per-attempt timeouts + body-redaction-on-error are uniform across binaries.
func New(vaultURL string) (*Client, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("create azure credential: %w", err)
	}
	return &Client{
		vaultURL:   vaultURL,
		httpClient: httpx.NewClient(httpx.WithPerAttemptTimeout(30 * time.Second)),
		credential: cred,
	}, nil
}

// newWithDeps is the test seam — lets tests inject a fake credential and a
// stub HTTP client without touching azidentity or real networking.
func newWithDeps(vaultURL string, cred azcore.TokenCredential, hc *http.Client) *Client {
	return &Client{vaultURL: vaultURL, httpClient: hc, credential: cred}
}

type setSecretBody struct {
	Value      string            `json:"value"`
	Attributes *secretAttributes `json:"attributes,omitempty"`
}

type secretAttributes struct {
	Enabled bool `json:"enabled"`
}

// Put writes a secret. If the destination is soft-deleted, behaviour is
// governed by strategy (see Strategy constants).
//
// The happy path is a single PUT. On 409 Conflict, Put probes
// /deletedsecrets/{name} to distinguish a generic conflict from a
// deleted-but-recoverable secret; the typed return mirrors that distinction.
func (c *Client) Put(ctx context.Context, name, value string, strategy Strategy) error {
	err := c.putOnce(ctx, name, value)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrConflict) {
		return err
	}
	deleted, probeErr := c.isSoftDeleted(ctx, name)
	if probeErr != nil {
		// Surface the original conflict — we couldn't disambiguate, and the
		// probe error is less actionable than the conflict.
		return err
	}
	if !deleted {
		return err // genuine ErrConflict
	}
	switch strategy {
	case RecoverIfSoftDeleted:
		if recErr := c.recoverDeleted(ctx, name); recErr != nil {
			return fmt.Errorf("recover soft-deleted secret %s: %w", name, recErr)
		}
		return c.putOnce(ctx, name, value)
	default:
		return ErrSoftDeletedSecretExists
	}
}

func (c *Client) putOnce(ctx context.Context, name, value string) error {
	token, err := c.token(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(setSecretBody{Value: value, Attributes: &secretAttributes{Enabled: true}})
	if err != nil {
		return fmt.Errorf("marshal secret body: %w", err)
	}
	url := fmt.Sprintf("%s/secrets/%s?api-version=%s", c.vaultURL, name, apiVersion)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build put request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("put secret %s: %w", name, err)
	}
	defer resp.Body.Close()
	if cerr := classify(resp); cerr != nil {
		return cerr
	}
	return nil
}

// Get fetches the current secret version's value + metadata.
func (c *Client) Get(ctx context.Context, name string) (Secret, error) {
	token, err := c.token(ctx)
	if err != nil {
		return Secret{}, err
	}
	url := fmt.Sprintf("%s/secrets/%s?api-version=%s", c.vaultURL, name, apiVersion)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Secret{}, fmt.Errorf("build get request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Secret{}, fmt.Errorf("get secret %s: %w", name, err)
	}
	defer resp.Body.Close()
	if cerr := classify(resp); cerr != nil {
		return Secret{}, cerr
	}
	var raw struct {
		Value      string `json:"value"`
		Attributes struct {
			Enabled bool  `json:"enabled"`
			Updated int64 `json:"updated"`
		} `json:"attributes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Secret{}, fmt.Errorf("decode secret %s: %w", name, err)
	}
	return Secret{
		Name:      name,
		Value:     raw.Value,
		UpdatedAt: time.Unix(raw.Attributes.Updated, 0),
		Enabled:   raw.Attributes.Enabled,
	}, nil
}

// isSoftDeleted reports whether the secret is currently in the deleted-but-
// recoverable state. A 200 from /deletedsecrets/{name} means yes; a 404 means
// no; anything else is an error.
func (c *Client) isSoftDeleted(ctx context.Context, name string) (bool, error) {
	token, err := c.token(ctx)
	if err != nil {
		return false, err
	}
	url := fmt.Sprintf("%s/deletedsecrets/%s?api-version=%s", c.vaultURL, name, apiVersion)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("probe deleted secret %s: %w", name, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("probe deleted secret %s: unexpected status %d", name, resp.StatusCode)
	}
}

func (c *Client) recoverDeleted(ctx context.Context, name string) error {
	token, err := c.token(ctx)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/deletedsecrets/%s/recover?api-version=%s", c.vaultURL, name, apiVersion)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("build recover request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("recover request: %w", err)
	}
	defer resp.Body.Close()
	return classify(resp)
}

func (c *Client) token(ctx context.Context) (string, error) {
	tok, err := c.credential.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{akvScope}})
	if err != nil {
		return "", fmt.Errorf("get akv token: %w", err)
	}
	return tok.Token, nil
}

// classify maps an AKV response to a typed sentinel error or returns nil for
// 2xx. For unrecognised non-2xx (e.g., 5xx) it returns *httpx.Error so the
// FR-V4-17 redaction-by-default contract still applies (no raw body in the
// error string). The body is drained on every error path so the connection
// can be reused.
func classify(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return ErrAuthFailed
	case resp.StatusCode == http.StatusNotFound:
		return ErrSecretNotFound
	case resp.StatusCode == http.StatusConflict:
		return ErrConflict
	default:
		e := &httpx.Error{StatusCode: resp.StatusCode, Status: resp.Status}
		if resp.Request != nil && resp.Request.URL != nil {
			u := *resp.Request.URL
			u.RawQuery = ""
			e.URL = u.String()
		}
		return e
	}
}

type secretVersionItem struct {
	ID         string           `json:"id"`
	Attributes secretAttributes `json:"attributes"`
	Managed    bool             `json:"managed"`
}

type secretVersionsResponse struct {
	Value    []secretVersionItem `json:"value"`
	NextLink string              `json:"nextLink"`
}

// DisableOldVersions disables all but the latest version of a secret (for
// grace-period revocation). Uses the same typed-error classification as Put/Get.
func (c *Client) DisableOldVersions(ctx context.Context, name string) error {
	token, err := c.token(ctx)
	if err != nil {
		return err
	}

	listURL := fmt.Sprintf("%s/secrets/%s/versions?api-version=%s", c.vaultURL, name, apiVersion)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return fmt.Errorf("build list-versions request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("list secret versions %s: %w", name, err)
	}
	defer resp.Body.Close()
	if cerr := classify(resp); cerr != nil {
		return cerr
	}

	var versions secretVersionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return fmt.Errorf("decode versions: %w", err)
	}

	if len(versions.Value) <= 1 {
		return nil
	}

	// Disable all but the last version (newest is last in the list).
	for _, v := range versions.Value[:len(versions.Value)-1] {
		if !v.Attributes.Enabled {
			continue
		}
		body, _ := json.Marshal(map[string]any{"attributes": map[string]bool{"enabled": false}})
		patchReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, v.ID+"?api-version="+apiVersion, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("build patch request: %w", err)
		}
		patchReq.Header.Set("Authorization", "Bearer "+token)
		patchReq.Header.Set("Content-Type", "application/json")
		patchResp, err := c.httpClient.Do(patchReq)
		if err != nil {
			return fmt.Errorf("disable version: %w", err)
		}
		if cerr := classify(patchResp); cerr != nil {
			patchResp.Body.Close()
			return fmt.Errorf("disable version %s: %w", v.ID, cerr)
		}
		patchResp.Body.Close()
	}
	return nil
}

// GetSecretUpdatedAt returns the last-updated timestamp of the current secret
// version. Implemented in terms of Get to keep the typed-error mapping in one
// place.
func (c *Client) GetSecretUpdatedAt(ctx context.Context, name string) (time.Time, error) {
	s, err := c.Get(ctx, name)
	if err != nil {
		return time.Time{}, err
	}
	return s.UpdatedAt, nil
}
