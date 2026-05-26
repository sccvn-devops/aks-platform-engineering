package bootstrap

import (
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubeRestConfig returns the rest.Config the binary should use:
// in-cluster when available, falling back to the operator's
// kubeconfig file at $HOME/.kube/config. Used by every binary in the
// suite — kept here so the InCluster + clientcmd fallback pattern
// lives in one place.
func KubeRestConfig() (*rest.Config, error) {
	cfg, err := rest.InClusterConfig()
	if err == nil {
		return cfg, nil
	}
	cfg, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	if err != nil {
		return nil, fmt.Errorf("build kube config: %w", err)
	}
	return cfg, nil
}
