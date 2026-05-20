package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultStatusNamespace = "kube-system"
	defaultStatusConfigMap = "mgmt-leader-status"
	defaultMetricsAddr     = ":8080"
	defaultLeaseDuration   = 60 * time.Second
	defaultRenewInterval   = 15 * time.Second
	defaultPollInterval    = 5 * time.Second
	defaultArgoCDNamespace = "argocd"
)

type Controller struct {
	ClusterName      string
	LeaseBlobURL     string
	StatusNamespace  string
	StatusConfigMap  string
	MetricsAddr      string
	LeaseDuration    time.Duration
	RenewInterval    time.Duration
	PreferredMetaKey string
}

type CLI struct {
	LeaseBlobURL     string
	PreferredMetaKey string
}

type Scaler struct {
	ClusterName     string
	StatusNamespace string
	StatusConfigMap string
	ArgoCDNamespace string
	PollInterval    time.Duration
}

func LoadController() (Controller, error) {
	cfg := Controller{
		ClusterName:      os.Getenv("CLUSTER_NAME"),
		LeaseBlobURL:     os.Getenv("LEASE_BLOB_URL"),
		StatusNamespace:  getEnv("STATUS_NAMESPACE", defaultStatusNamespace),
		StatusConfigMap:  getEnv("STATUS_CONFIGMAP_NAME", defaultStatusConfigMap),
		MetricsAddr:      getEnv("METRICS_ADDR", defaultMetricsAddr),
		LeaseDuration:    getDurationEnv("LEASE_DURATION_SECONDS", defaultLeaseDuration),
		RenewInterval:    getDurationEnv("LEASE_RENEW_INTERVAL_SECONDS", defaultRenewInterval),
		PreferredMetaKey: getEnv("PREFERRED_CLUSTER_METADATA_KEY", "preferred-cluster"),
	}

	if cfg.ClusterName == "" {
		return Controller{}, errors.New("CLUSTER_NAME is required")
	}
	if cfg.LeaseBlobURL == "" {
		return Controller{}, errors.New("LEASE_BLOB_URL is required")
	}
	if cfg.LeaseDuration < 15*time.Second || cfg.LeaseDuration > 60*time.Second {
		return Controller{}, fmt.Errorf("LEASE_DURATION_SECONDS must be between 15 and 60 seconds, got %s", cfg.LeaseDuration)
	}
	if cfg.RenewInterval <= 0 {
		return Controller{}, fmt.Errorf("LEASE_RENEW_INTERVAL_SECONDS must be positive, got %s", cfg.RenewInterval)
	}
	if cfg.RenewInterval >= cfg.LeaseDuration {
		return Controller{}, fmt.Errorf("LEASE_RENEW_INTERVAL_SECONDS must be shorter than LEASE_DURATION_SECONDS")
	}

	return cfg, nil
}

func LoadCLI() (CLI, error) {
	cfg := CLI{
		LeaseBlobURL:     os.Getenv("LEASE_BLOB_URL"),
		PreferredMetaKey: getEnv("PREFERRED_CLUSTER_METADATA_KEY", "preferred-cluster"),
	}
	if cfg.LeaseBlobURL == "" {
		return CLI{}, errors.New("LEASE_BLOB_URL is required")
	}

	return cfg, nil
}

func LoadScaler() (Scaler, error) {
	cfg := Scaler{
		ClusterName:     os.Getenv("CLUSTER_NAME"),
		StatusNamespace: getEnv("STATUS_NAMESPACE", defaultStatusNamespace),
		StatusConfigMap: getEnv("STATUS_CONFIGMAP_NAME", defaultStatusConfigMap),
		ArgoCDNamespace: getEnv("ARGOCD_NAMESPACE", defaultArgoCDNamespace),
		PollInterval:    getDurationEnv("POLL_INTERVAL_SECONDS", defaultPollInterval),
	}

	if cfg.ClusterName == "" {
		return Scaler{}, errors.New("CLUSTER_NAME is required")
	}
	if cfg.PollInterval <= 0 {
		return Scaler{}, fmt.Errorf("POLL_INTERVAL_SECONDS must be positive, got %s", cfg.PollInterval)
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return fallback
	}

	return time.Duration(seconds) * time.Second
}
