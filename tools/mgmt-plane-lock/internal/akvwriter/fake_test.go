package akvwriter

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fake is the in-memory SecretStore used by consumer unit tests (and by this
// package's own tests to verify the SecretStore contract end-to-end without
// HTTP). It models present/soft-deleted/missing states + auth + arbitrary
// next-error injection so each typed sentinel can be exercised.
type fake struct {
	mu        sync.Mutex
	secrets   map[string]Secret
	deleted   map[string]Secret
	now       func() time.Time
	authError error
	nextError error // one-shot; cleared after use
}

func newFake() *fake {
	return &fake{
		secrets: map[string]Secret{},
		deleted: map[string]Secret{},
		now:     time.Now,
	}
}

func (f *fake) consumeNextError() error {
	if f.nextError != nil {
		e := f.nextError
		f.nextError = nil
		return e
	}
	return nil
}

func (f *fake) Put(ctx context.Context, name, value string, strategy Strategy) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.authError != nil {
		return f.authError
	}
	if e := f.consumeNextError(); e != nil {
		return e
	}
	if d, ok := f.deleted[name]; ok {
		switch strategy {
		case RecoverIfSoftDeleted:
			delete(f.deleted, name)
			d.Value = value
			d.Enabled = true
			d.UpdatedAt = f.now()
			f.secrets[name] = d
			return nil
		default:
			return ErrSoftDeletedSecretExists
		}
	}
	f.secrets[name] = Secret{Name: name, Value: value, UpdatedAt: f.now(), Enabled: true}
	return nil
}

func (f *fake) Get(ctx context.Context, name string) (Secret, error) {
	if err := ctx.Err(); err != nil {
		return Secret{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.authError != nil {
		return Secret{}, f.authError
	}
	s, ok := f.secrets[name]
	if !ok {
		return Secret{}, ErrSecretNotFound
	}
	return s, nil
}

// Delete moves the named secret into the soft-deleted set (test helper).
func (f *fake) Delete(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.secrets[name]; ok {
		delete(f.secrets, name)
		f.deleted[name] = s
	}
}

func (f *fake) SetAuthError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authError = err
}

func (f *fake) FailNext(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextError = err
}

// Compile-time check: fake satisfies the same SecretStore surface as *Client.
var _ SecretStore = (*fake)(nil)

// --- Tests that exercise the fake adapter directly. These ensure the fake
// reproduces the same typed-error semantics consumers will rely on. ---

func TestFake_PutGet_HappyPath(t *testing.T) {
	f := newFake()
	if err := f.Put(context.Background(), "tok", "v1", Overwrite); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := f.Get(context.Background(), "tok")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Value != "v1" || !got.Enabled {
		t.Fatalf("unexpected secret: %+v", got)
	}
}

func TestFake_GetNotFound(t *testing.T) {
	f := newFake()
	_, err := f.Get(context.Background(), "missing")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("want ErrSecretNotFound, got %v", err)
	}
}

func TestFake_SoftDeleted_OverwriteRefuses(t *testing.T) {
	f := newFake()
	_ = f.Put(context.Background(), "tok", "v1", Overwrite)
	f.Delete("tok")
	err := f.Put(context.Background(), "tok", "v2", Overwrite)
	if !errors.Is(err, ErrSoftDeletedSecretExists) {
		t.Fatalf("want ErrSoftDeletedSecretExists, got %v", err)
	}
}

func TestFake_SoftDeleted_RecoverWritesNewValue(t *testing.T) {
	f := newFake()
	_ = f.Put(context.Background(), "tok", "v1", Overwrite)
	f.Delete("tok")
	if err := f.Put(context.Background(), "tok", "v2", RecoverIfSoftDeleted); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, _ := f.Get(context.Background(), "tok")
	if got.Value != "v2" {
		t.Fatalf("want v2, got %q", got.Value)
	}
}

func TestFake_AuthErrorInjection(t *testing.T) {
	f := newFake()
	f.SetAuthError(ErrAuthFailed)
	if err := f.Put(context.Background(), "x", "y", Overwrite); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("Put: want ErrAuthFailed, got %v", err)
	}
	if _, err := f.Get(context.Background(), "x"); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("Get: want ErrAuthFailed, got %v", err)
	}
}

func TestFake_NextErrorOneShot(t *testing.T) {
	f := newFake()
	f.FailNext(ErrConflict)
	if err := f.Put(context.Background(), "k", "v", Overwrite); !errors.Is(err, ErrConflict) {
		t.Fatalf("first Put: want ErrConflict, got %v", err)
	}
	if err := f.Put(context.Background(), "k", "v", Overwrite); err != nil {
		t.Fatalf("second Put: want nil, got %v", err)
	}
}

func TestFake_CompileAssertionInterface(t *testing.T) {
	// Both real Client and fake must satisfy SecretStore — this asserts the
	// interface alignment AC3 requires.
	var _ SecretStore = (*Client)(nil)
	var _ SecretStore = (*fake)(nil)
}
