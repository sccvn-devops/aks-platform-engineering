// Command saas-token-rotator rotates Bitbucket workspace tokens and Jira service-account
// tokens on a schedule, writing the new credentials to both regional Azure Key Vaults.
// It is lease-aware: reads the mgmt-leader-status ConfigMap and exits immediately when
// the local cluster is not the active management plane.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/akvwriter"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/httpx"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// httpClient is the binary-wide HTTP client. Constructed from httpx.NewClient
// so timeouts, retries, and error redaction come from the single transport
// seam (FR-V4-15..18). No http.DefaultClient anywhere in this binary.
var httpClient = httpx.NewClient(httpx.WithPerAttemptTimeout(30 * time.Second))

var (
	tokenAgeDays = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "saas_token_age_days",
		Help: "Age of the SaaS token in days, by token name.",
	}, []string{"token"})
)

func main() {
	tokenType := flag.String("token-type", "", "Token type to rotate: bitbucket or jira")
	gracePeriodHours := flag.Int("grace-period-hours", 24, "Hours before old token versions are disabled")
	metricsAddr := flag.String("metrics-addr", ":8080", "Prometheus metrics address")
	flag.Parse()

	if *tokenType == "" {
		log.Fatal("--token-type is required (bitbucket or jira)")
	}

	ctx := context.Background()

	// Check lease status — exit if not the active management cluster.
	if !isActiveManagementCluster(ctx) {
		log.Println("not the active management cluster, skipping rotation")
		return
	}

	kv1URL := mustEnv("KEY_VAULT_WE_URL")
	kv2URL := mustEnv("KEY_VAULT_NE_URL")

	weClient, err := akvwriter.New(kv1URL)
	if err != nil {
		log.Fatalf("create WE key vault client: %v", err)
	}
	neClient, err := akvwriter.New(kv2URL)
	if err != nil {
		log.Fatalf("create NE key vault client: %v", err)
	}

	// Expose metrics while the job runs.
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(*metricsAddr, nil); err != nil {
			log.Printf("metrics server: %v", err)
		}
	}()

	var secretName string
	var newToken string

	switch *tokenType {
	case "bitbucket":
		secretName = mustEnv("SECRET_NAME")
		newToken, err = rotateBitbucketToken(ctx)
		if err != nil {
			log.Fatalf("rotate bitbucket token: %v", err)
		}
	case "jira":
		secretName = mustEnv("SECRET_NAME")
		newToken, err = rotateJiraToken(ctx)
		if err != nil {
			log.Fatalf("rotate jira token: %v", err)
		}
	default:
		log.Fatalf("unknown token type: %s", *tokenType)
	}

	// Write new token to both regional vaults. RecoverIfSoftDeleted lets the
	// rotator complete even if a prior aborted run left the secret in the
	// soft-deleted state (e.g., manual operator delete during an incident).
	if err := weClient.Put(ctx, secretName, newToken, akvwriter.RecoverIfSoftDeleted); err != nil {
		log.Fatalf("write WE vault secret %s: %v", secretName, err)
	}
	if err := neClient.Put(ctx, secretName, newToken, akvwriter.RecoverIfSoftDeleted); err != nil {
		log.Fatalf("write NE vault secret %s: %v", secretName, err)
	}
	log.Printf("wrote rotated %s token to both regional vaults as %s", *tokenType, secretName)

	// Disable old versions after grace period.
	graceDuration := time.Duration(*gracePeriodHours) * time.Hour
	time.Sleep(graceDuration)

	for _, c := range []*akvwriter.Client{weClient, neClient} {
		if err := c.DisableOldVersions(ctx, secretName); err != nil {
			log.Printf("disable old versions %s: %v", secretName, err)
		}
	}
	log.Printf("disabled old versions of %s after %d-hour grace period", secretName, *gracePeriodHours)

	// Emit age metric (days since last rotation = ~0 now).
	updatedAt, err := weClient.GetSecretUpdatedAt(ctx, secretName)
	if err == nil {
		ageD := time.Since(updatedAt).Hours() / 24
		tokenAgeDays.WithLabelValues(*tokenType + "-" + secretName).Set(ageD)
	}
}

func isActiveManagementCluster(ctx context.Context) bool {
	statusNamespace := getEnv("STATUS_NAMESPACE", "kube-system")
	statusConfigMap := getEnv("STATUS_CONFIGMAP_NAME", "mgmt-leader-status")

	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		kubeConfig, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			log.Printf("build kube config: %v", err)
			return false
		}
	}
	clientset, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		log.Printf("create kubernetes client: %v", err)
		return false
	}

	cm, err := clientset.CoreV1().ConfigMaps(statusNamespace).Get(ctx, statusConfigMap, metav1.GetOptions{})
	if err != nil {
		log.Printf("get leader status configmap: %v", err)
		return false
	}
	return cm.Data["leadershipStatus"] == "active"
}

// rotateBitbucketToken mints a new Bitbucket workspace token via the Bitbucket Cloud API.
func rotateBitbucketToken(ctx context.Context) (string, error) {
	workspace := mustEnv("BITBUCKET_WORKSPACE")
	clientID := mustEnv("BITBUCKET_OAUTH_CLIENT_ID")
	clientSecret := mustEnv("BITBUCKET_OAUTH_CLIENT_SECRET")

	// Exchange client credentials for a new workspace access token.
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

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
