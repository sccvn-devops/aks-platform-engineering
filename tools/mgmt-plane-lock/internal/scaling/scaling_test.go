package scaling

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestReadLeaseStatus(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mgmt-leader-status",
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"leadershipStatus": LeaseStatusActive,
		},
	})

	status, err := ReadLeaseStatus(context.Background(), client, "kube-system", "mgmt-leader-status")
	if err != nil {
		t.Fatalf("ReadLeaseStatus() error = %v", err)
	}
	if status != LeaseStatusActive {
		t.Fatalf("ReadLeaseStatus() = %q, want %q", status, LeaseStatusActive)
	}
}

func TestReconcileWorkloadsScalesActiveResources(t *testing.T) {
	client := fake.NewSimpleClientset(
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller", Namespace: "argocd"},
			Spec:       appsv1.StatefulSetSpec{Replicas: int32Ptr(0)},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "argocd-repo-server", Namespace: "argocd"},
			Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(0)},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "crossplane", Namespace: "crossplane-system"},
			Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(0)},
		},
	)

	workloads := []Workload{
		{
			Kind:            StatefulSetKind,
			Namespace:       "argocd",
			Name:            "argocd-application-controller",
			ActiveReplicas:  3,
			StandbyReplicas: 0,
		},
		{
			Kind:            DeploymentKind,
			Namespace:       "argocd",
			Name:            "argocd-repo-server",
			ActiveReplicas:  2,
			StandbyReplicas: 0,
		},
		{
			Kind:            DeploymentKind,
			Namespace:       "crossplane-system",
			Name:            "crossplane",
			ActiveReplicas:  1,
			StandbyReplicas: 0,
		},
		{
			Kind:            DeploymentKind,
			Namespace:       "jira-bridge",
			Name:            "jira-bridge",
			ActiveReplicas:  1,
			StandbyReplicas: 0,
			Optional:        true,
		},
	}

	if err := ReconcileWorkloads(context.Background(), client, workloads, true); err != nil {
		t.Fatalf("ReconcileWorkloads() error = %v", err)
	}

	statefulSet, _ := client.AppsV1().StatefulSets("argocd").Get(context.Background(), "argocd-application-controller", metav1.GetOptions{})
	if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != 3 {
		t.Fatalf("argocd-application-controller replicas = %v, want 3", statefulSet.Spec.Replicas)
	}

	deployment, _ := client.AppsV1().Deployments("argocd").Get(context.Background(), "argocd-repo-server", metav1.GetOptions{})
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 2 {
		t.Fatalf("argocd-repo-server replicas = %v, want 2", deployment.Spec.Replicas)
	}

	crossplane, _ := client.AppsV1().Deployments("crossplane-system").Get(context.Background(), "crossplane", metav1.GetOptions{})
	if crossplane.Spec.Replicas == nil || *crossplane.Spec.Replicas != 1 {
		t.Fatalf("crossplane replicas = %v, want 1", crossplane.Spec.Replicas)
	}
}

func TestReconcileWorkloadsScalesStandbyToZero(t *testing.T) {
	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "external-secrets", Namespace: "external-secrets"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
	})

	workloads := []Workload{
		{
			Kind:            DeploymentKind,
			Namespace:       "external-secrets",
			Name:            "external-secrets",
			ActiveReplicas:  1,
			StandbyReplicas: 0,
		},
	}

	if err := ReconcileWorkloads(context.Background(), client, workloads, false); err != nil {
		t.Fatalf("ReconcileWorkloads() error = %v", err)
	}

	deployment, _ := client.AppsV1().Deployments("external-secrets").Get(context.Background(), "external-secrets", metav1.GetOptions{})
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 0 {
		t.Fatalf("external-secrets replicas = %v, want 0", deployment.Spec.Replicas)
	}
}

func TestUpdateClusterSecretLeaseStatus(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mgmt-we",
			Namespace: "argocd",
			Labels: map[string]string{
				"argocd.argoproj.io/secret-type": "cluster",
				"lease-status":                   LeaseStatusStandby,
			},
		},
	})

	if err := UpdateClusterSecretLeaseStatus(context.Background(), client, "argocd", "mgmt-we", LeaseStatusActive); err != nil {
		t.Fatalf("UpdateClusterSecretLeaseStatus() error = %v", err)
	}

	secret, _ := client.CoreV1().Secrets("argocd").Get(context.Background(), "mgmt-we", metav1.GetOptions{})
	if secret.Labels["lease-status"] != LeaseStatusActive {
		t.Fatalf("lease-status = %q, want %q", secret.Labels["lease-status"], LeaseStatusActive)
	}
}
