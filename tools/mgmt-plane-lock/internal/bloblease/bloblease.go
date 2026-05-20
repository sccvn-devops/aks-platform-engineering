package bloblease

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/lease"
	"github.com/google/uuid"
)

type Manager struct {
	blobClient        *blob.Client
	preferredMetaKey  string
}

type Properties struct {
	Metadata map[string]string
}

func New(ctx context.Context, blobURL, preferredMetaKey string) (*Manager, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("create azure credential: %w", err)
	}

	client, err := blob.NewClient(blobURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("create blob client: %w", err)
	}

	return &Manager{
		blobClient:       client,
		preferredMetaKey: preferredMetaKey,
	}, nil
}

func (m *Manager) Acquire(ctx context.Context, duration time.Duration) (string, error) {
	leaseID := uuid.NewString()
	leaseClient, err := lease.NewBlobClient(m.blobClient, &lease.BlobClientOptions{
		LeaseID: &leaseID,
	})
	if err != nil {
		return "", fmt.Errorf("create lease client: %w", err)
	}

	if _, err := leaseClient.AcquireLease(ctx, int32(duration/time.Second), nil); err != nil {
		return "", err
	}

	return leaseID, nil
}

func (m *Manager) Renew(ctx context.Context, leaseID string) error {
	leaseClient, err := lease.NewBlobClient(m.blobClient, &lease.BlobClientOptions{
		LeaseID: &leaseID,
	})
	if err != nil {
		return fmt.Errorf("create lease client: %w", err)
	}

	_, err = leaseClient.RenewLease(ctx, nil)
	return err
}

func (m *Manager) Release(ctx context.Context, leaseID string) error {
	leaseClient, err := lease.NewBlobClient(m.blobClient, &lease.BlobClientOptions{
		LeaseID: &leaseID,
	})
	if err != nil {
		return fmt.Errorf("create lease client: %w", err)
	}

	_, err = leaseClient.ReleaseLease(ctx, nil)
	if IsMissingLeaseError(err) {
		return nil
	}
	return err
}

func (m *Manager) Break(ctx context.Context) error {
	leaseClient, err := lease.NewBlobClient(m.blobClient, nil)
	if err != nil {
		return fmt.Errorf("create lease client: %w", err)
	}

	zero := int32(0)
	_, err = leaseClient.BreakLease(ctx, &lease.BlobBreakOptions{
		BreakPeriod: &zero,
	})
	if IsMissingLeaseError(err) {
		return nil
	}
	return err
}

func (m *Manager) Properties(ctx context.Context) (Properties, error) {
	resp, err := m.blobClient.GetProperties(ctx, nil)
	if err != nil {
		return Properties{}, err
	}

	metadata := make(map[string]string, len(resp.Metadata))
	for key, value := range resp.Metadata {
		if value == nil {
			continue
		}
		metadata[strings.ToLower(key)] = *value
	}

	return Properties{Metadata: metadata}, nil
}

func (m *Manager) PreferredCluster(ctx context.Context) (string, error) {
	props, err := m.Properties(ctx)
	if err != nil {
		return "", err
	}
	return props.Metadata[strings.ToLower(m.preferredMetaKey)], nil
}

func (m *Manager) SetPreferredCluster(ctx context.Context, cluster string) error {
	for i := 0; i < 12; i++ {
		props, err := m.blobClient.GetProperties(ctx, nil)
		if err != nil {
			return err
		}

		metadata := map[string]*string{}
		for key, value := range props.Metadata {
			metadata[key] = value
		}
		metadata[m.preferredMetaKey] = &cluster
		now := time.Now().UTC().Format(time.RFC3339)
		metadata["last-failback-requested-at"] = &now

		_, err = m.blobClient.SetMetadata(ctx, metadata, nil)
		if err == nil {
			return nil
		}
		if !IsLeaseConflict(err) {
			return err
		}

		if breakErr := m.Break(ctx); breakErr != nil {
			return fmt.Errorf("break lease before metadata update: %w", breakErr)
		}
		time.Sleep(5 * time.Second)
	}

	return errors.New("timed out waiting for blob lease break to update metadata")
}

func IsLeaseConflict(err error) bool {
	var respErr *azcore.ResponseError
	if !errors.As(err, &respErr) {
		return false
	}

	switch respErr.ErrorCode {
	case "LeaseIDMissing", "LeaseIDMismatchWithBlobOperation", "LeaseIsBreakingAndCannotBeAcquired", "LeaseAlreadyPresent":
		return true
	default:
		return respErr.StatusCode == 409
	}
}

func IsMissingLeaseError(err error) bool {
	if err == nil {
		return false
	}

	var respErr *azcore.ResponseError
	if !errors.As(err, &respErr) {
		return false
	}

	return respErr.ErrorCode == "LeaseNotPresentWithBlobOperation" || respErr.StatusCode == 404
}
