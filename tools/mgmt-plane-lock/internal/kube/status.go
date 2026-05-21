package kube

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Status struct {
	Namespace        string
	ConfigMapName    string
	ClusterName      string
	LeaseBlobURL     string
	LeadershipStatus string
	LeaseID          string
	PreferredCluster string
	LastRenewedAt    time.Time
}

func UpsertStatus(ctx context.Context, client kubernetes.Interface, status Status) error {
	configMaps := client.CoreV1().ConfigMaps(status.Namespace)
	existing, err := configMaps.Get(ctx, status.ConfigMapName, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("get status configmap: %w", err)
	}

	data := map[string]string{
		"clusterName":       status.ClusterName,
		"holderIdentity":    status.ClusterName,
		"leaseBlobURL":      status.LeaseBlobURL,
		"leadershipStatus":  status.LeadershipStatus,
		"leaseID":           status.LeaseID,
		"preferredCluster":  status.PreferredCluster,
		"lastObservedAt":    time.Now().UTC().Format(time.RFC3339),
		"lastRenewedAt":     status.LastRenewedAt.UTC().Format(time.RFC3339),
	}

	if existing == nil {
		_, err = configMaps.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      status.ConfigMapName,
				Namespace: status.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/name":       "mgmt-leader-lease",
					"app.kubernetes.io/managed-by": "argocd",
				},
			},
			Data: data,
		}, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create status configmap: %w", err)
		}
		return nil
	}

	existing.Data = data
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	existing.Labels["app.kubernetes.io/name"] = "mgmt-leader-lease"
	existing.Labels["app.kubernetes.io/managed-by"] = "argocd"

	_, err = configMaps.Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("update status configmap: %w", err)
	}

	return nil
}
