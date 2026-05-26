package rotation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/akvwriter"
)

// fakeStore is a minimal akvwriter.SecretStore for rotation-runner
// tests. Records the last Put + lets tests inject a put error.
type fakeStore struct {
	mu        sync.Mutex
	lastName  string
	lastValue string
	lastStrat akvwriter.Strategy
	putErr    error

	// disableErr controls DisableOldVersions; if disableCalls
	// is non-nil the call is recorded.
	disableErr   error
	disableCalls []string
}

func (f *fakeStore) Put(_ context.Context, name, value string, s akvwriter.Strategy) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastName, f.lastValue, f.lastStrat = name, value, s
	return f.putErr
}

func (f *fakeStore) Get(context.Context, string) (akvwriter.Secret, error) {
	return akvwriter.Secret{}, nil
}

func (f *fakeStore) DisableOldVersions(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disableCalls = append(f.disableCalls, name)
	return f.disableErr
}

type fakeAger struct {
	when time.Time
	err  error
}

func (f fakeAger) GetSecretUpdatedAt(context.Context, string) (time.Time, error) {
	return f.when, f.err
}

func TestRunnerSkipsWhenStandby(t *testing.T) {
	r := &Runner{
		TokenType:   "bitbucket",
		SecretName:  "bb-token",
		GracePeriod: 0,
		Minter:      MinterFunc(func(context.Context) (string, error) { return "", errors.New("must not call") }),
		WEStore:     &fakeStore{},
		NEStore:     &fakeStore{},
		LeaseCheck:  LeaseCheckerFunc(func(context.Context) bool { return false }),
	}

	err := r.Run(context.Background())
	if !errors.Is(err, ErrSkippedNotActive) {
		t.Fatalf("Run() = %v, want ErrSkippedNotActive", err)
	}
}

func TestRunnerHappyPathWritesBothVaults(t *testing.T) {
	we := &fakeStore{}
	ne := &fakeStore{}
	ages := make(map[string]float64)
	r := &Runner{
		TokenType:   "jira",
		SecretName:  "jira-token",
		GracePeriod: 5 * time.Millisecond,
		Minter:      MinterFunc(func(context.Context) (string, error) { return "shiny-new-token", nil }),
		WEStore:     we,
		NEStore:     ne,
		LeaseCheck:  LeaseCheckerFunc(func(context.Context) bool { return true }),
		WEAger:      fakeAger{when: time.Now()},
		AgeReport: AgeReporterFunc(func(tt, name string, days float64) {
			ages[tt+"/"+name] = days
		}),
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if we.lastName != "jira-token" || we.lastValue != "shiny-new-token" || we.lastStrat != akvwriter.RecoverIfSoftDeleted {
		t.Fatalf("WE Put got (%q,%q,%v), want (jira-token, shiny-new-token, RecoverIfSoftDeleted)",
			we.lastName, we.lastValue, we.lastStrat)
	}
	if ne.lastName != "jira-token" || ne.lastValue != "shiny-new-token" {
		t.Fatalf("NE Put got (%q,%q), want (jira-token, shiny-new-token)", ne.lastName, ne.lastValue)
	}
	if len(we.disableCalls) != 1 || we.disableCalls[0] != "jira-token" {
		t.Fatalf("WE DisableOldVersions calls = %v, want [jira-token]", we.disableCalls)
	}
	if len(ne.disableCalls) != 1 {
		t.Fatalf("NE DisableOldVersions calls = %v, want one call", ne.disableCalls)
	}
	if _, ok := ages["jira/jira-token"]; !ok {
		t.Fatalf("AgeReport never called for jira/jira-token; got %#v", ages)
	}
}

// TestRunnerSIGTERMDuringGracePeriodExitsFast verifies AC3: ctx
// cancellation during the grace period returns the runner within 1s.
func TestRunnerSIGTERMDuringGracePeriodExitsFast(t *testing.T) {
	r := &Runner{
		TokenType:   "bitbucket",
		SecretName:  "bb-token",
		GracePeriod: time.Hour,
		Minter:      MinterFunc(func(context.Context) (string, error) { return "tok", nil }),
		WEStore:     &fakeStore{},
		NEStore:     &fakeStore{},
		LeaseCheck:  LeaseCheckerFunc(func(context.Context) bool { return true }),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- r.Run(ctx) }()

	// Allow the Puts to complete, then trigger SIGTERM-equivalent cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() = %v, want context.Canceled", err)
		}
		if elapsed := time.Since(start); elapsed >= time.Second {
			t.Fatalf("Run() took %v after cancel, want <1s (AC3)", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not exit within 2s of cancel — AC3 violated")
	}
}

func TestRunnerSurfacesMintError(t *testing.T) {
	r := &Runner{
		TokenType:  "bitbucket",
		SecretName: "bb-token",
		Minter:     MinterFunc(func(context.Context) (string, error) { return "", errors.New("mint failed") }),
		WEStore:    &fakeStore{},
		NEStore:    &fakeStore{},
		LeaseCheck: LeaseCheckerFunc(func(context.Context) bool { return true }),
	}

	err := r.Run(context.Background())
	if err == nil || !errorContains(err, "mint failed") {
		t.Fatalf("Run() = %v, want error containing 'mint failed'", err)
	}
}

func TestRunnerSurfacesWEPutError(t *testing.T) {
	we := &fakeStore{putErr: errors.New("we put failed")}
	r := &Runner{
		TokenType:  "bitbucket",
		SecretName: "bb-token",
		Minter:     MinterFunc(func(context.Context) (string, error) { return "tok", nil }),
		WEStore:    we,
		NEStore:    &fakeStore{},
		LeaseCheck: LeaseCheckerFunc(func(context.Context) bool { return true }),
	}

	err := r.Run(context.Background())
	if err == nil || !errorContains(err, "WE vault") {
		t.Fatalf("Run() = %v, want WE-vault wrap", err)
	}
}

func TestRunnerSurfacesNEPutError(t *testing.T) {
	r := &Runner{
		TokenType:  "bitbucket",
		SecretName: "bb-token",
		Minter:     MinterFunc(func(context.Context) (string, error) { return "tok", nil }),
		WEStore:    &fakeStore{},
		NEStore:    &fakeStore{putErr: errors.New("ne put failed")},
		LeaseCheck: LeaseCheckerFunc(func(context.Context) bool { return true }),
	}

	err := r.Run(context.Background())
	if err == nil || !errorContains(err, "NE vault") {
		t.Fatalf("Run() = %v, want NE-vault wrap", err)
	}
}

func TestRunnerValidatesRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		r    Runner
	}{
		{"empty TokenType", Runner{SecretName: "x", Minter: MinterFunc(func(context.Context) (string, error) { return "", nil }), WEStore: &fakeStore{}, NEStore: &fakeStore{}, LeaseCheck: LeaseCheckerFunc(func(context.Context) bool { return true })}},
		{"empty SecretName", Runner{TokenType: "x", Minter: MinterFunc(func(context.Context) (string, error) { return "", nil }), WEStore: &fakeStore{}, NEStore: &fakeStore{}, LeaseCheck: LeaseCheckerFunc(func(context.Context) bool { return true })}},
		{"nil Minter", Runner{TokenType: "x", SecretName: "y", WEStore: &fakeStore{}, NEStore: &fakeStore{}, LeaseCheck: LeaseCheckerFunc(func(context.Context) bool { return true })}},
		{"nil WEStore", Runner{TokenType: "x", SecretName: "y", Minter: MinterFunc(func(context.Context) (string, error) { return "", nil }), NEStore: &fakeStore{}, LeaseCheck: LeaseCheckerFunc(func(context.Context) bool { return true })}},
		{"nil NEStore", Runner{TokenType: "x", SecretName: "y", Minter: MinterFunc(func(context.Context) (string, error) { return "", nil }), WEStore: &fakeStore{}, LeaseCheck: LeaseCheckerFunc(func(context.Context) bool { return true })}},
		{"nil LeaseCheck", Runner{TokenType: "x", SecretName: "y", Minter: MinterFunc(func(context.Context) (string, error) { return "", nil }), WEStore: &fakeStore{}, NEStore: &fakeStore{}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.r.Run(context.Background()); err == nil {
				t.Fatalf("Run() returned nil, want validation error")
			}
		})
	}
}

func TestRunnerDisableErrorsAreLoggedNotFatal(t *testing.T) {
	we := &fakeStore{disableErr: errors.New("disable failed")}
	r := &Runner{
		TokenType:   "bitbucket",
		SecretName:  "bb-token",
		GracePeriod: time.Millisecond,
		Minter:      MinterFunc(func(context.Context) (string, error) { return "tok", nil }),
		WEStore:     we,
		NEStore:     &fakeStore{},
		LeaseCheck:  LeaseCheckerFunc(func(context.Context) bool { return true }),
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run() = %v, want nil (disable errors are non-fatal)", err)
	}
}

func errorContains(err error, substr string) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), substr)
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
