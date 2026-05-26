package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/httpx"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/jirabridge"
)

type jiraClient struct {
	baseURL   string
	project   string
	email     string
	token     string
	issueType string
	client    *http.Client
}

type jiraIssueRequest struct {
	Fields jiraIssueFields `json:"fields"`
}

type jiraIssueFields struct {
	Project     jiraProject    `json:"project"`
	Summary     string         `json:"summary"`
	IssueType   jiraIssueType  `json:"issuetype"`
	Labels      []string       `json:"labels,omitempty"`
	Description map[string]any `json:"description"`
}

type jiraProject struct {
	Key string `json:"key"`
}

type jiraIssueType struct {
	Name string `json:"name"`
}

func (c jiraClient) CreateIssue(ctx context.Context, event jirabridge.Event) error {
	payload := jiraIssueRequest{
		Fields: jiraIssueFields{
			Project:     jiraProject{Key: c.project},
			Summary:     event.Summary,
			IssueType:   jiraIssueType{Name: c.issueType},
			Labels:      event.Labels,
			Description: paragraphDocument(event.Description),
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal jira issue payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/rest/api/3/issue", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build jira request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if c.email != "" {
		req.SetBasicAuth(c.email, c.token)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("post jira issue: %w", err)
	}
	defer resp.Body.Close()

	if checkErr := httpx.CheckResponse(resp, 0); checkErr != nil {
		return fmt.Errorf("post jira issue: %w", checkErr)
	}

	return nil
}

func paragraphDocument(text string) map[string]any {
	content := make([]map[string]any, 0, 8)
	for _, line := range strings.Split(text, "\n") {
		paragraph := map[string]any{"type": "paragraph"}
		if line != "" {
			paragraph["content"] = []map[string]any{
				{
					"type": "text",
					"text": line,
				},
			}
		}
		content = append(content, paragraph)
	}

	return map[string]any{
		"type":    "doc",
		"version": 1,
		"content": content,
	}
}
