package main

import (
	"log"
	"strings"
	"time"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/bootstrap"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/httpx"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/jirabridge"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

func buildRunner(cfg config.JiraBridge) *jirabridge.Runner {
	restConfig, err := bootstrap.KubeRestConfig()
	if err != nil {
		log.Fatalf("%v", err)
	}
	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Fatalf("create kubernetes client: %v", err)
	}
	dynClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		log.Fatalf("create dynamic client: %v", err)
	}
	jira := jiraClient{
		baseURL: strings.TrimRight(cfg.JiraBaseURL, "/"), project: cfg.JiraProjectKey,
		email: cfg.JiraUserEmail, token: cfg.JiraToken, issueType: cfg.JiraIssueTypeName,
		client: httpx.NewClient(httpx.WithPerAttemptTimeout(15 * time.Second)),
	}
	return &jirabridge.Runner{Config: cfg, KubeClient: kubeClient, DynamicClient: dynClient, JiraClient: jira}
}
