package jirabridge

import (
	"strings"
	"testing"
	"time"
)

func TestEvaluateEventsDetectsSyncFailure(t *testing.T) {
	current := ApplicationDetails{
		Name: "payments-app-prod-aks-prod-we",
		Snapshot: Snapshot{
			OperationPhase:   "Failed",
			OperationMessage: "one or more sync tasks failed",
			SyncStatus:       "OutOfSync",
		},
	}

	events := EvaluateEvents(Snapshot{}, current, "mgmt-we", time.Unix(0, 0), []string{"informational"}, []string{"sev1"})
	if len(events) != 1 {
		t.Fatalf("EvaluateEvents() len = %d, want 1", len(events))
	}
	if events[0].Type != EventSyncFailed {
		t.Fatalf("event type = %q, want %q", events[0].Type, EventSyncFailed)
	}
	if !strings.Contains(events[0].Description, "failed sync operation") {
		t.Fatalf("description = %q, want sync failure context", events[0].Description)
	}
}

func TestEvaluateEventsDetectsRolloutFailureAsSev1(t *testing.T) {
	current := ApplicationDetails{
		Name: "payments-app-prod-aks-prod-we",
		Snapshot: Snapshot{
			HealthStatus:  "Degraded",
			HealthMessage: "Rollout aborted after analysis failure",
		},
	}

	events := EvaluateEvents(Snapshot{}, current, "mgmt-we", time.Unix(0, 0), []string{"informational"}, []string{"sev1"})
	if len(events) != 1 {
		t.Fatalf("EvaluateEvents() len = %d, want 1", len(events))
	}
	if events[0].Type != EventRolloutFailed {
		t.Fatalf("event type = %q, want %q", events[0].Type, EventRolloutFailed)
	}
	if events[0].Summary != "[SEV1] Argo Rollout analysis failed for payments-app-prod-aks-prod-we" {
		t.Fatalf("summary = %q", events[0].Summary)
	}
}

func TestEvaluateEventsDetectsDriftCorrection(t *testing.T) {
	previous := Snapshot{SyncStatus: "OutOfSync"}
	current := ApplicationDetails{
		Name: "payments-app-dev-aks-dev-we",
		Snapshot: Snapshot{
			SyncStatus: "Synced",
		},
	}

	events := EvaluateEvents(previous, current, "mgmt-we", time.Unix(0, 0), []string{"informational"}, []string{"sev1"})
	if len(events) != 1 {
		t.Fatalf("EvaluateEvents() len = %d, want 1", len(events))
	}
	if events[0].Type != EventDriftCorrected {
		t.Fatalf("event type = %q, want %q", events[0].Type, EventDriftCorrected)
	}
}

func TestParseStateDefaultsEmptyApplications(t *testing.T) {
	state, err := ParseState("")
	if err != nil {
		t.Fatalf("ParseState() error = %v", err)
	}
	if len(state.Applications) != 0 {
		t.Fatalf("ParseState() applications len = %d, want 0", len(state.Applications))
	}
}
