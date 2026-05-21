package bloblease

import (
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

func TestIsLeaseConflict(t *testing.T) {
	if IsLeaseConflict(nil) {
		t.Fatal("IsLeaseConflict(nil) = true, want false")
	}

	err := &azcore.ResponseError{StatusCode: 409, ErrorCode: "LeaseAlreadyPresent"}
	if !IsLeaseConflict(err) {
		t.Fatal("IsLeaseConflict(LeaseAlreadyPresent) = false, want true")
	}
}

func TestIsMissingLeaseError(t *testing.T) {
	if IsMissingLeaseError(nil) {
		t.Fatal("IsMissingLeaseError(nil) = true, want false")
	}

	err := &azcore.ResponseError{StatusCode: 404, ErrorCode: "LeaseNotPresentWithBlobOperation"}
	if !IsMissingLeaseError(err) {
		t.Fatal("IsMissingLeaseError(LeaseNotPresentWithBlobOperation) = false, want true")
	}
}

func TestLeaseConflictHelpersIgnoreUnrelatedErrors(t *testing.T) {
	err := errors.New("boom")
	if IsLeaseConflict(err) {
		t.Fatal("IsLeaseConflict(unrelated) = true, want false")
	}
	if IsMissingLeaseError(err) {
		t.Fatal("IsMissingLeaseError(unrelated) = true, want false")
	}
}
