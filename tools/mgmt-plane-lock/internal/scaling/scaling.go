package scaling

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	LeaseStatusActive  = "active"
	LeaseStatusStandby = "standby"
)

type WorkloadKind string

const (
	DeploymentKind  WorkloadKind = "Deployment"
	StatefulSetKind WorkloadKind = "StatefulSet"
)

type Workload struct {
	Kind            WorkloadKind
	Namespace       string
	Name            string
	ActiveReplicas  int32
	StandbyReplicas int32
	Optional        bool
}

func DefaultWorkloads() []Workload {
	return []Workload{
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
			Namespace:       "argocd",
			Name:            "argocd-server",
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
			Namespace:       "external-secrets",
			Name:            "external-secrets",
			ActiveReplicas:  1,
			StandbyReplicas: 0,
			Optional:        true,
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
}

func ReadLeaseStatus(ctx context.Context, client kubernetes.Interface, namespace, configMapName string) (string, error) {
	cm, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get leadership status configmap: %w", err)
	}

	status := strings.TrimSpace(cm.Data["leadershipStatus"])
	switch status {
	case LeaseStatusActive, LeaseStatusStandby:
		return status, nil
	case "":
		return "", fmt.Errorf("configmap %s/%s missing leadershipStatus", namespace, configMapName)
	default:
		return "", fmt.Errorf("configmap %s/%s has unsupported leadershipStatus %q", namespace, configMapName, status)
	}
}

func ReconcileWorkloads(ctx context.Context, client kubernetes.Interface, workloads []Workload, active bool) error {
	for _, workload := range workloads {
		desiredReplicas := workload.StandbyReplicas
		if active {
			desiredReplicas = workload.ActiveReplicas
		}

		if err := reconcileWorkload(ctx, client, workload, desiredReplicas); err != nil {
			return err
		}
	}

	return nil
}

func UpdateClusterSecretLeaseStatus(ctx context.Context, client kubernetes.Interface, namespace, clusterName, leaseStatus string) error {
	secrets := client.CoreV1().Secrets(namespace)
	secret, err := secrets.Get(ctx, clusterName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		list, listErr := secrets.List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("argocd.argoproj.io/secret-type=cluster,akuity.io/argo-cd-cluster-name=%s", clusterName),
		})
		if listErr != nil {
			return fmt.Errorf("list argocd cluster secrets for %s: %w", clusterName, listErr)
		}
		if len(list.Items) == 0 {
			return fmt.Errorf("argocd cluster secret %q not found in namespace %s", clusterName, namespace)
		}
		secret = &list.Items[0]
		err = nil
	}
	if err != nil {
		return fmt.Errorf("get argocd cluster secret %q: %w", clusterName, err)
	}

	if secret.Labels == nil {
		secret.Labels = map[string]string{}
	}
	if secret.Labels["lease-status"] == leaseStatus {
		return nil
	}

	secret.Labels["lease-status"] = leaseStatus
	if _, err := secrets.Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update argocd cluster secret %q lease-status: %w", secret.Name, err)
	}

	return nil
}

func reconcileWorkload(ctx context.Context, client kubernetes.Interface, workload Workload, desiredReplicas int32) error {
	switch workload.Kind {
	case DeploymentKind:
		return reconcileDeployment(ctx, client, workload, desiredReplicas)
	case StatefulSetKind:
		return reconcileStatefulSet(ctx, client, workload, desiredReplicas)
	default:
		return fmt.Errorf("unsupported workload kind %q for %s/%s", workload.Kind, workload.Namespace, workload.Name)
	}
}

func reconcileDeployment(ctx context.Context, client kubernetes.Interface, workload Workload, desiredReplicas int32) error {
	deployments := client.AppsV1().Deployments(workload.Namespace)
	deployment, err := deployments.Get(ctx, workload.Name, metav1.GetOptions{})
	if handleMissingWorkload(err, workload) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get deployment %s/%s: %w", workload.Namespace, workload.Name, err)
	}

	if replicasEqual(deployment.Spec.Replicas, desiredReplicas) {
		return nil
	}

	deployment.Spec.Replicas = int32Ptr(desiredReplicas)
	if _, err := deployments.Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update deployment %s/%s replicas: %w", workload.Namespace, workload.Name, err)
	}

	return nil
}

func reconcileStatefulSet(ctx context.Context, client kubernetes.Interface, workload Workload, desiredReplicas int32) error {
	statefulSets := client.AppsV1().StatefulSets(workload.Namespace)
	statefulSet, err := statefulSets.Get(ctx, workload.Name, metav1.GetOptions{})
	if handleMissingWorkload(err, workload) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get statefulset %s/%s: %w", workload.Namespace, workload.Name, err)
	}

	if replicasEqual(statefulSet.Spec.Replicas, desiredReplicas) {
		return nil
	}

	statefulSet.Spec.Replicas = int32Ptr(desiredReplicas)
	if _, err := statefulSets.Update(ctx, statefulSet, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update statefulset %s/%s replicas: %w", workload.Namespace, workload.Name, err)
	}

	return nil
}

func handleMissingWorkload(err error, workload Workload) bool {
	return workload.Optional && apierrors.IsNotFound(err)
}

func replicasEqual(replicas *int32, desiredReplicas int32) bool {
	if replicas == nil {
		return desiredReplicas == 1
	}
	return *replicas == desiredReplicas
}

func int32Ptr(value int32) *int32 {
	return &value
}
