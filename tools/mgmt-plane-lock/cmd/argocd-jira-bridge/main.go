package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/httpx"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/jirabridge"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const stateKey = "state.json"

var applicationsGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "applications",
}

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
	Project     jiraProject   `json:"project"`
	Summary     string        `json:"summary"`
	IssueType   jiraIssueType `json:"issuetype"`
	Labels      []string      `json:"labels,omitempty"`
	Description map[string]any `json:"description"`
}

type jiraProject struct {
	Key string `json:"key"`
}

type jiraIssueType struct {
	Name string `json:"name"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadJiraBridge()
	if err != nil {
		log.Fatalf("load jira bridge config: %v", err)
	}

	restConfig, err := rest.InClusterConfig()
	if err != nil {
		restConfig, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			log.Fatalf("build kube config: %v", err)
		}
	}

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Fatalf("create kubernetes client: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		log.Fatalf("create dynamic client: %v", err)
	}

	jira := jiraClient{
		baseURL:   strings.TrimRight(cfg.JiraBaseURL, "/"),
		project:   cfg.JiraProjectKey,
		email:     cfg.JiraUserEmail,
		token:     cfg.JiraToken,
		issueType: cfg.JiraIssueTypeName,
		client: httpx.NewClient(httpx.WithPerAttemptTimeout(15 * time.Second)),
	}

	if err := reconcile(ctx, cfg, kubeClient, dynamicClient, jira); err != nil {
		log.Printf("initial reconcile failed: %v", err)
	}

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := reconcile(ctx, cfg, kubeClient, dynamicClient, jira); err != nil {
				log.Printf("reconcile failed: %v", err)
			}
		}
	}
}

func reconcile(ctx context.Context, cfg config.JiraBridge, kubeClient kubernetes.Interface, dynamicClient dynamic.Interface, jira jiraClient) error {
	stateCM, state, err := loadState(ctx, kubeClient, cfg)
	if err != nil {
		return err
	}

	apps, err := dynamicClient.Resource(applicationsGVR).Namespace(cfg.ArgoCDNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list argocd applications: %w", err)
	}

	nextState := jirabridge.State{Applications: map[string]jirabridge.ApplicationState{}}

	for i := range apps.Items {
		app := &apps.Items[i]
		current := jirabridge.SnapshotFromApplication(app)
		previous := state.Applications[current.Name].Snapshot

		events := jirabridge.EvaluateEvents(previous, current, cfg.ClusterName, time.Now(), cfg.JiraInfoIssueLabels, cfg.JiraSev1IssueLabels)
		if err := createIssues(ctx, jira, events); err != nil {
			return fmt.Errorf("create jira issue for %s: %w", current.Name, err)
		}

		nextState.Applications[current.Name] = jirabridge.ApplicationState{Snapshot: current.Snapshot}
	}

	encoded, err := nextState.Encode()
	if err != nil {
		return err
	}
	if stateCM.Data == nil {
		stateCM.Data = map[string]string{}
	}
	stateCM.Data[stateKey] = encoded

	if _, err := kubeClient.CoreV1().ConfigMaps(cfg.StateNamespace).Update(ctx, stateCM, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update state configmap: %w", err)
	}

	return nil
}

func createIssues(ctx context.Context, jira jiraClient, events []jirabridge.Event) error {
	for _, event := range events {
		if err := jira.CreateIssue(ctx, event); err != nil {
			return err
		}
	}

	return nil
}

func loadState(ctx context.Context, kubeClient kubernetes.Interface, cfg config.JiraBridge) (*corev1.ConfigMap, jirabridge.State, error) {
	configMaps := kubeClient.CoreV1().ConfigMaps(cfg.StateNamespace)
	stateCM, err := configMaps.Get(ctx, cfg.StateConfigMapName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		stateCM = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cfg.StateConfigMapName,
				Namespace: cfg.StateNamespace,
			},
			Data: map[string]string{
				stateKey: `{"applications":{}}`,
			},
		}
		created, createErr := configMaps.Create(ctx, stateCM, metav1.CreateOptions{})
		if createErr != nil {
			return nil, jirabridge.State{}, fmt.Errorf("create state configmap: %w", createErr)
		}
		stateCM = created
		err = nil
	}
	if err != nil {
		return nil, jirabridge.State{}, fmt.Errorf("get state configmap: %w", err)
	}

	state, err := jirabridge.ParseState(stateCM.Data[stateKey])
	if err != nil {
		return nil, jirabridge.State{}, err
	}

	return stateCM, state, nil
}

func (c jiraClient) CreateIssue(ctx context.Context, event jirabridge.Event) error {
	payload := jiraIssueRequest{
		Fields: jiraIssueFields{
			Project:   jiraProject{Key: c.project},
			Summary:   event.Summary,
			IssueType: jiraIssueType{Name: c.issueType},
			Labels:    event.Labels,
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
