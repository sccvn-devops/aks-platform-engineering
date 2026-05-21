package config

import (
	"testing"
	"time"
)

func TestLoadControllerDefaults(t *testing.T) {
	t.Setenv("CLUSTER_NAME", "mgmt-we")
	t.Setenv("LEASE_BLOB_URL", "https://example.blob.core.windows.net/leases/mgmt-active")

	cfg, err := LoadController()
	if err != nil {
		t.Fatalf("LoadController() error = %v", err)
	}

	if cfg.StatusNamespace != "kube-system" {
		t.Fatalf("StatusNamespace = %q, want kube-system", cfg.StatusNamespace)
	}
	if cfg.StatusConfigMap != "mgmt-leader-status" {
		t.Fatalf("StatusConfigMap = %q, want mgmt-leader-status", cfg.StatusConfigMap)
	}
	if cfg.LeaseDuration != 60*time.Second {
		t.Fatalf("LeaseDuration = %s, want 60s", cfg.LeaseDuration)
	}
	if cfg.RenewInterval != 15*time.Second {
		t.Fatalf("RenewInterval = %s, want 15s", cfg.RenewInterval)
	}
}

func TestLoadControllerRejectsInvalidLeaseWindow(t *testing.T) {
	t.Setenv("CLUSTER_NAME", "mgmt-we")
	t.Setenv("LEASE_BLOB_URL", "https://example.blob.core.windows.net/leases/mgmt-active")
	t.Setenv("LEASE_DURATION_SECONDS", "61")

	if _, err := LoadController(); err == nil {
		t.Fatal("LoadController() error = nil, want validation error")
	}
}

func TestLoadScalerDefaults(t *testing.T) {
	t.Setenv("CLUSTER_NAME", "mgmt-ne")

	cfg, err := LoadScaler()
	if err != nil {
		t.Fatalf("LoadScaler() error = %v", err)
	}

	if cfg.StatusNamespace != "kube-system" {
		t.Fatalf("StatusNamespace = %q, want kube-system", cfg.StatusNamespace)
	}
	if cfg.StatusConfigMap != "mgmt-leader-status" {
		t.Fatalf("StatusConfigMap = %q, want mgmt-leader-status", cfg.StatusConfigMap)
	}
	if cfg.ArgoCDNamespace != "argocd" {
		t.Fatalf("ArgoCDNamespace = %q, want argocd", cfg.ArgoCDNamespace)
	}
	if cfg.PollInterval != 5*time.Second {
		t.Fatalf("PollInterval = %s, want 5s", cfg.PollInterval)
	}
}

func TestLoadScalerRejectsMissingClusterName(t *testing.T) {
	if _, err := LoadScaler(); err == nil {
		t.Fatal("LoadScaler() error = nil, want validation error")
	}
}

func TestLoadJiraBridgeDefaults(t *testing.T) {
	t.Setenv("CLUSTER_NAME", "mgmt-we")
	t.Setenv("JIRA_BASE_URL", "https://example.atlassian.net")
	t.Setenv("JIRA_PROJECT_KEY", "IDP")
	t.Setenv("JIRA_TOKEN", "token")

	cfg, err := LoadJiraBridge()
	if err != nil {
		t.Fatalf("LoadJiraBridge() error = %v", err)
	}

	if cfg.ArgoCDNamespace != "argocd" {
		t.Fatalf("ArgoCDNamespace = %q, want argocd", cfg.ArgoCDNamespace)
	}
	if cfg.StateNamespace != "jira-bridge" {
		t.Fatalf("StateNamespace = %q, want jira-bridge", cfg.StateNamespace)
	}
	if cfg.StateConfigMapName != "jira-bridge-state" {
		t.Fatalf("StateConfigMapName = %q, want jira-bridge-state", cfg.StateConfigMapName)
	}
	if cfg.JiraIssueTypeName != "Task" {
		t.Fatalf("JiraIssueTypeName = %q, want Task", cfg.JiraIssueTypeName)
	}
	if cfg.PollInterval != 5*time.Second {
		t.Fatalf("PollInterval = %s, want 5s", cfg.PollInterval)
	}
}

func TestLoadJiraBridgeRejectsMissingRequiredValues(t *testing.T) {
	t.Setenv("CLUSTER_NAME", "mgmt-we")

	if _, err := LoadJiraBridge(); err == nil {
		t.Fatal("LoadJiraBridge() error = nil, want validation error")
	}
}
