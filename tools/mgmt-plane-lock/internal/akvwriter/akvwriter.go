// Package akvwriter writes secrets to Azure Key Vault using the REST API
// authenticated via the default Azure credential (Workload Identity).
package akvwriter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

const akvScope = "https://vault.azure.net/.default"

// Client writes secrets to Azure Key Vault.
type Client struct {
	vaultURL   string
	httpClient *http.Client
	credential azcore.TokenCredential
}

// New creates a Client for the given vault URL.
func New(vaultURL string) (*Client, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("create azure credential: %w", err)
	}
	return &Client{
		vaultURL:   vaultURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		credential: cred,
	}, nil
}

type setSecretBody struct {
	Value      string            `json:"value"`
	Attributes *secretAttributes `json:"attributes,omitempty"`
}

type secretAttributes struct {
	Enabled bool `json:"enabled"`
}

// SetSecret writes a secret to the vault. If the secret already exists its value is updated.
func (c *Client) SetSecret(ctx context.Context, name, value string) error {
	token, err := c.credential.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{akvScope}})
	if err != nil {
		return fmt.Errorf("get akv token: %w", err)
	}

	body, err := json.Marshal(setSecretBody{Value: value, Attributes: &secretAttributes{Enabled: true}})
	if err != nil {
		return fmt.Errorf("marshal secret body: %w", err)
	}

	url := fmt.Sprintf("%s/secrets/%s?api-version=7.4", c.vaultURL, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("set secret request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("set secret %s: status %d: %s", name, resp.StatusCode, string(respBody))
	}
	return nil
}

type secretVersionItem struct {
	ID         string            `json:"id"`
	Attributes secretAttributes  `json:"attributes"`
	Managed    bool              `json:"managed"`
}

type secretVersionsResponse struct {
	Value    []secretVersionItem `json:"value"`
	NextLink string              `json:"nextLink"`
}

// DisableOldVersions disables all but the latest version of a secret (for grace-period revocation).
func (c *Client) DisableOldVersions(ctx context.Context, name string) error {
	token, err := c.credential.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{akvScope}})
	if err != nil {
		return fmt.Errorf("get akv token: %w", err)
	}

	listURL := fmt.Sprintf("%s/secrets/%s/versions?api-version=7.4", c.vaultURL, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.Token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("list secret versions: %w", err)
	}
	defer resp.Body.Close()

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
		patchReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, v.ID+"?api-version=7.4", bytes.NewReader(body))
		if err != nil {
			return err
		}
		patchReq.Header.Set("Authorization", "Bearer "+token.Token)
		patchReq.Header.Set("Content-Type", "application/json")
		patchResp, err := c.httpClient.Do(patchReq)
		if err != nil {
			return fmt.Errorf("disable version: %w", err)
		}
		patchResp.Body.Close()
	}
	return nil
}

// GetSecretUpdatedAt returns the last-updated timestamp of the current secret version.
func (c *Client) GetSecretUpdatedAt(ctx context.Context, name string) (time.Time, error) {
	token, err := c.credential.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{akvScope}})
	if err != nil {
		return time.Time{}, fmt.Errorf("get akv token: %w", err)
	}

	url := fmt.Sprintf("%s/secrets/%s?api-version=7.4", c.vaultURL, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return time.Time{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token.Token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return time.Time{}, fmt.Errorf("get secret: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Attributes struct {
			Updated int64 `json:"updated"`
		} `json:"attributes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return time.Time{}, fmt.Errorf("decode secret: %w", err)
	}
	return time.Unix(result.Attributes.Updated, 0), nil
}
