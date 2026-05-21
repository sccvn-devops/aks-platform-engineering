package main

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/scaling"
)

func TestReconcileActiveCluster(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "mgmt-leader-status", Namespace: "kube-system"},
			Data: map[string]string{
				"leadershipStatus": scaling.LeaseStatusActive,
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mgmt-we",
				Namespace: "argocd",
				Labels: map[string]string{
					"argocd.argoproj.io/secret-type": "cluster",
					"lease-status":                   scaling.LeaseStatusStandby,
				},
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "crossplane", Namespace: "crossplane-system"},
			Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(0)},
		},
	)

	cfg := config.Scaler{
		ClusterName:     "mgmt-we",
		StatusNamespace: "kube-system",
		StatusConfigMap: "mgmt-leader-status",
		ArgoCDNamespace: "argocd",
	}

	workloads := []scaling.Workload{
		{
			Kind:            scaling.DeploymentKind,
			Namespace:       "crossplane-system",
			Name:            "crossplane",
			ActiveReplicas:  1,
			StandbyReplicas: 0,
		},
	}

	if err := reconcile(context.Background(), cfg, client, workloads); err != nil {
		t.Fatalf("reconcile() error = %v", err)
	}

	secret, _ := client.CoreV1().Secrets("argocd").Get(context.Background(), "mgmt-we", metav1.GetOptions{})
	if secret.Labels["lease-status"] != scaling.LeaseStatusActive {
		t.Fatalf("lease-status = %q, want %q", secret.Labels["lease-status"], scaling.LeaseStatusActive)
	}

	deployment, _ := client.AppsV1().Deployments("crossplane-system").Get(context.Background(), "crossplane", metav1.GetOptions{})
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		t.Fatalf("crossplane replicas = %v, want 1", deployment.Spec.Replicas)
	}
}

func int32Ptr(value int32) *int32 {
	return &value
}
