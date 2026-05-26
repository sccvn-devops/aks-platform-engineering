package scaling

import (
	"bytes"
	"context"
	"log"
	"testing"
	"time"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestDefaultWorkloadsReturnsExpectedNames(t *testing.T) {
	got := DefaultWorkloads()
	if len(got) < 4 {
		t.Fatalf("DefaultWorkloads len = %d, want >=4", len(got))
	}
	names := map[string]bool{}
	for _, w := range got {
		names[w.Namespace+"/"+w.Name] = true
	}
	for _, want := range []string{
		"argocd/argocd-application-controller",
		"argocd/argocd-repo-server",
		"crossplane-system/crossplane",
	} {
		if !names[want] {
			t.Fatalf("DefaultWorkloads missing %q; got %v", want, names)
		}
	}
}

func TestUpdateClusterSecretLeaseStatusFallsBackToLabelSelector(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "longer-name-secret",
			Namespace: "argocd",
			Labels: map[string]string{
				"argocd.argoproj.io/secret-type":  "cluster",
				"akuity.io/argo-cd-cluster-name":  "mgmt-we",
				"lease-status":                    LeaseStatusStandby,
			},
		},
	})

	if err := UpdateClusterSecretLeaseStatus(context.Background(), client, "argocd", "mgmt-we", LeaseStatusActive); err != nil {
		t.Fatalf("UpdateClusterSecretLeaseStatus() error = %v", err)
	}

	secret, _ := client.CoreV1().Secrets("argocd").Get(context.Background(), "longer-name-secret", metav1.GetOptions{})
	if secret.Labels["lease-status"] != LeaseStatusActive {
		t.Fatalf("lease-status = %q, want %q", secret.Labels["lease-status"], LeaseStatusActive)
	}
}

func TestUpdateClusterSecretLeaseStatusErrorsWhenNotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	if err := UpdateClusterSecretLeaseStatus(context.Background(), client, "argocd", "mgmt-we", LeaseStatusActive); err == nil {
		t.Fatal("UpdateClusterSecretLeaseStatus() with no secret returned nil, want error")
	}
}

func TestUpdateClusterSecretLeaseStatusNoOpWhenAlreadyDesired(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mgmt-we",
			Namespace: "argocd",
			Labels:    map[string]string{"lease-status": LeaseStatusActive},
		},
	})

	if err := UpdateClusterSecretLeaseStatus(context.Background(), client, "argocd", "mgmt-we", LeaseStatusActive); err != nil {
		t.Fatalf("noop update returned %v", err)
	}
}

func TestReconcileStatefulSetSkipsWhenAlreadyAtDesiredReplicas(t *testing.T) {
	client := fake.NewSimpleClientset(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ss", Namespace: "ns"},
		Spec:       appsv1.StatefulSetSpec{Replicas: int32Ptr(2)},
	})

	if err := reconcileStatefulSet(context.Background(), client,
		Workload{Kind: StatefulSetKind, Namespace: "ns", Name: "ss"}, 2); err != nil {
		t.Fatalf("reconcileStatefulSet returned %v, want nil", err)
	}
}

func TestReconcileWorkloadRejectsUnsupportedKind(t *testing.T) {
	client := fake.NewSimpleClientset()
	err := reconcileWorkload(context.Background(), client,
		Workload{Kind: "Unknown", Namespace: "ns", Name: "x"}, 1)
	if err == nil {
		t.Fatal("reconcileWorkload(Unknown) returned nil, want error")
	}
}

func TestReplicasEqualHandlesNilPointer(t *testing.T) {
	if !replicasEqual(nil, 1) {
		t.Fatal("replicasEqual(nil, 1) = false, want true (defaulting)")
	}
	if replicasEqual(nil, 2) {
		t.Fatal("replicasEqual(nil, 2) = true, want false")
	}
}

func TestReadLeaseStatusRejectsUnknownStatus(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "mgmt-leader-status", Namespace: "kube-system"},
		Data:       map[string]string{"leadershipStatus": "weirdness"},
	})
	if _, err := ReadLeaseStatus(context.Background(), client, "kube-system", "mgmt-leader-status"); err == nil {
		t.Fatal("ReadLeaseStatus(unknown) returned nil, want error")
	}
}

func TestReadLeaseStatusRejectsEmptyStatus(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "mgmt-leader-status", Namespace: "kube-system"},
		Data:       map[string]string{"leadershipStatus": ""},
	})
	if _, err := ReadLeaseStatus(context.Background(), client, "kube-system", "mgmt-leader-status"); err == nil {
		t.Fatal("ReadLeaseStatus(empty) returned nil, want error")
	}
}

func TestRunnerLogfWritesToCustomLogger(t *testing.T) {
	var buf bytes.Buffer
	r := &Runner{Logger: log.New(&buf, "", 0)}
	r.logf("hello %s", "world")
	if got := buf.String(); got != "hello world\n" {
		t.Fatalf("logf wrote %q, want %q", got, "hello world\n")
	}
}

func TestRunnerLogfDefaultsToStdLogger(t *testing.T) {
	r := &Runner{}
	r.logf("noop") // exercises the default path; should not panic
}

func TestRunnerRunRecoversFromWatchSetupFailure(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependWatchReactor("configmaps", func(_ k8stesting.Action) (bool, watch.Interface, error) {
		return true, nil, errInjected
	})

	r := &Runner{
		Config: config.Scaler{
			ClusterName:     "mgmt-we",
			StatusNamespace: "kube-system",
			StatusConfigMap: "mgmt-leader-status",
			ArgoCDNamespace: "argocd",
			PollInterval:    10 * time.Millisecond,
		},
		Clientset: client,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := r.Run(ctx); err != nil {
		t.Fatalf("Run() returned %v, want nil on watch-setup failure + ctx done", err)
	}
}

var errInjected = injectedError{}

type injectedError struct{}

func (injectedError) Error() string { return "injected" }
