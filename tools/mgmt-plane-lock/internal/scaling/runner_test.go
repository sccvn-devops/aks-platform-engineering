package scaling

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRunnerReconcileScalesUpOnActive(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "mgmt-leader-status", Namespace: "kube-system"},
			Data:       map[string]string{"leadershipStatus": LeaseStatusActive},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mgmt-we",
				Namespace: "argocd",
				Labels: map[string]string{
					"argocd.argoproj.io/secret-type": "cluster",
					"lease-status":                   LeaseStatusStandby,
				},
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "crossplane", Namespace: "crossplane-system"},
			Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(0)},
		},
	)

	r := &Runner{
		Config: config.Scaler{
			ClusterName:     "mgmt-we",
			StatusNamespace: "kube-system",
			StatusConfigMap: "mgmt-leader-status",
			ArgoCDNamespace: "argocd",
			PollInterval:    10 * time.Millisecond,
		},
		Clientset: client,
		Workloads: []Workload{
			{Kind: DeploymentKind, Namespace: "crossplane-system", Name: "crossplane", ActiveReplicas: 1, StandbyReplicas: 0},
		},
	}

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	deployment, _ := client.AppsV1().Deployments("crossplane-system").Get(context.Background(), "crossplane", metav1.GetOptions{})
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		t.Fatalf("crossplane replicas = %v, want 1", deployment.Spec.Replicas)
	}

	secret, _ := client.CoreV1().Secrets("argocd").Get(context.Background(), "mgmt-we", metav1.GetOptions{})
	if secret.Labels["lease-status"] != LeaseStatusActive {
		t.Fatalf("lease-status = %q, want %q", secret.Labels["lease-status"], LeaseStatusActive)
	}
}

func TestRunnerRunExitsOnContextCancel(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "mgmt-leader-status", Namespace: "kube-system"},
			Data:       map[string]string{"leadershipStatus": LeaseStatusActive},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mgmt-we",
				Namespace: "argocd",
				Labels: map[string]string{
					"argocd.argoproj.io/secret-type": "cluster",
					"lease-status":                   LeaseStatusStandby,
				},
			},
		},
	)

	r := &Runner{
		Config: config.Scaler{
			ClusterName:     "mgmt-we",
			StatusNamespace: "kube-system",
			StatusConfigMap: "mgmt-leader-status",
			ArgoCDNamespace: "argocd",
			PollInterval:    20 * time.Millisecond,
		},
		Clientset: client,
		Workloads: []Workload{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- r.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not exit after ctx cancel within 2s")
	}
}

func TestRunnerRunWithNilClientsetReturnsError(t *testing.T) {
	r := &Runner{
		Config: config.Scaler{PollInterval: 10 * time.Millisecond},
	}

	err := r.Run(context.Background())
	if err == nil || !errors.Is(err, errNilClientset) {
		t.Fatalf("Run() with nil clientset = %v, want errNilClientset", err)
	}
}

func TestRunnerReconcileSurfacesReadLeaseError(t *testing.T) {
	client := fake.NewSimpleClientset()

	r := &Runner{
		Config: config.Scaler{
			ClusterName:     "mgmt-we",
			StatusNamespace: "kube-system",
			StatusConfigMap: "mgmt-leader-status",
			ArgoCDNamespace: "argocd",
		},
		Clientset: client,
		Workloads: []Workload{},
	}

	err := r.Reconcile(context.Background())
	if err == nil {
		t.Fatal("Reconcile() with missing configmap returned nil, want error")
	}
}
