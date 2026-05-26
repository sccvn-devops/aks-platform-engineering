package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/httpx"
)

// httpClient is the binary-wide HTTP client. Constructed from httpx.NewClient
// so timeouts, retries, and error redaction come from the single transport
// seam (FR-V4-15..18). No http.DefaultClient anywhere in this binary.
var httpClient = httpx.NewClient(httpx.WithPerAttemptTimeout(30 * time.Second))

// rotateBitbucketToken mints a new Bitbucket workspace token via the Bitbucket Cloud API.
func rotateBitbucketToken(ctx context.Context) (string, error) {
	workspace := mustEnv("BITBUCKET_WORKSPACE")
	clientID := mustEnv("BITBUCKET_OAUTH_CLIENT_ID")
	clientSecret := mustEnv("BITBUCKET_OAUTH_CLIENT_SECRET")

	body := bytes.NewBufferString("grant_type=client_credentials")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://bitbucket.org/site/oauth2/access_token", body)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("bitbucket oauth: %w", err)
	}
	defer resp.Body.Close()

	if checkErr := httpx.CheckResponse(resp, 0); checkErr != nil {
		return "", fmt.Errorf("bitbucket oauth: %w", checkErr)
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode bitbucket token: %w", err)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("empty bitbucket access token for workspace %s", workspace)
	}
	return result.AccessToken, nil
}

// rotateJiraToken mints a new Jira service account API token via the Atlassian API.
func rotateJiraToken(ctx context.Context) (string, error) {
	email := mustEnv("JIRA_SERVICE_ACCOUNT_EMAIL")
	existingToken := mustEnv("JIRA_CURRENT_API_TOKEN")

	payload, _ := json.Marshal(map[string]string{
		"label": fmt.Sprintf("platform-rotation-%d", time.Now().Unix()),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.atlassian.com/me/api-tokens", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(email, existingToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("create jira api token: %w", err)
	}
	defer resp.Body.Close()

	if checkErr := httpx.CheckResponse(resp, 0); checkErr != nil {
		return "", fmt.Errorf("create jira api token: %w", checkErr)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode jira token: %w", err)
	}
	if result.Token == "" {
		return "", fmt.Errorf("empty jira api token response")
	}
	return result.Token, nil
}
