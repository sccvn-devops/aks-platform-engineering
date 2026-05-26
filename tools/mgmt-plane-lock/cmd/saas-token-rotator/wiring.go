package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/akvwriter"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/bootstrap"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/rotation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var tokenAgeDays = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "saas_token_age_days",
	Help: "Age of the SaaS token in days, by token name.",
}, []string{"token"})

// serveMetricsInBackground spins ServeMetrics on a goroutine. Lives
// here so main.go stays under the FR-V4-24 40-line bound.
func serveMetricsInBackground(ctx context.Context, addr string) {
	go func() {
		if err := bootstrap.ServeMetrics(ctx, addr, nil); err != nil {
			log.Printf("metrics server: %v", err)
		}
	}()
}

// buildRunner wires the rotation pipeline.
func buildRunner(tokenType string, gracePeriod time.Duration) *rotation.Runner {
	we, ne := buildVaultClients()
	return &rotation.Runner{
		TokenType: tokenType, SecretName: mustEnv("SECRET_NAME"),
		GracePeriod: gracePeriod, Minter: selectMinter(tokenType),
		WEStore: we, NEStore: ne, WEAger: we,
		LeaseCheck: rotation.LeaseCheckerFunc(isActiveManagementCluster),
		AgeReport: rotation.AgeReporterFunc(func(tt, name string, d float64) {
			tokenAgeDays.WithLabelValues(tt + "-" + name).Set(d)
		}),
	}
}

func buildVaultClients() (*akvwriter.Client, *akvwriter.Client) {
	we, err := akvwriter.New(mustEnv("KEY_VAULT_WE_URL"))
	if err != nil {
		log.Fatalf("create WE key vault client: %v", err)
	}
	ne, err := akvwriter.New(mustEnv("KEY_VAULT_NE_URL"))
	if err != nil {
		log.Fatalf("create NE key vault client: %v", err)
	}
	return we, ne
}

func selectMinter(tokenType string) rotation.Minter {
	switch tokenType {
	case "bitbucket":
		return rotation.MinterFunc(rotateBitbucketToken)
	case "jira":
		return rotation.MinterFunc(rotateJiraToken)
	default:
		log.Fatalf("unknown token type: %s", tokenType)
		return nil
	}
}

func isActiveManagementCluster(ctx context.Context) bool {
	statusNamespace := getEnv("STATUS_NAMESPACE", "kube-system")
	statusConfigMap := getEnv("STATUS_CONFIGMAP_NAME", "mgmt-leader-status")
	restConfig, err := bootstrap.KubeRestConfig()
	if err != nil {
		log.Printf("%v", err)
		return false
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
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
