package jirabridge

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Snapshot struct {
	SyncStatus       string `json:"syncStatus"`
	HealthStatus     string `json:"healthStatus"`
	HealthMessage    string `json:"healthMessage"`
	OperationPhase   string `json:"operationPhase"`
	OperationMessage string `json:"operationMessage"`
}

type ApplicationState struct {
	Snapshot Snapshot `json:"snapshot"`
}

type State struct {
	Applications map[string]ApplicationState `json:"applications"`
}

type EventType string

const (
	EventSyncFailed     EventType = "sync-failed"
	EventRolloutFailed  EventType = "rollout-analysis-failed"
	EventDriftCorrected EventType = "drift-corrected"
)

type ApplicationDetails struct {
	Name                 string
	Namespace            string
	Project              string
	DestinationName      string
	DestinationNamespace string
	SyncRevision         string
	Snapshot             Snapshot
}

type Event struct {
	Type        EventType
	Summary     string
	Description string
	Labels      []string
}

func ParseState(raw string) (State, error) {
	if strings.TrimSpace(raw) == "" {
		return State{Applications: map[string]ApplicationState{}}, nil
	}

	var state State
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return State{}, fmt.Errorf("decode state: %w", err)
	}
	if state.Applications == nil {
		state.Applications = map[string]ApplicationState{}
	}

	return state, nil
}

func (s State) Encode() (string, error) {
	if s.Applications == nil {
		s.Applications = map[string]ApplicationState{}
	}

	data, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("encode state: %w", err)
	}

	return string(data), nil
}

func SnapshotFromApplication(app *unstructured.Unstructured) ApplicationDetails {
	return ApplicationDetails{
		Name:                 app.GetName(),
		Namespace:            app.GetNamespace(),
		Project:              nestedString(app.Object, "spec", "project"),
		DestinationName:      nestedString(app.Object, "spec", "destination", "name"),
		DestinationNamespace: nestedString(app.Object, "spec", "destination", "namespace"),
		SyncRevision:         nestedString(app.Object, "status", "sync", "revision"),
		Snapshot: Snapshot{
			SyncStatus:       nestedString(app.Object, "status", "sync", "status"),
			HealthStatus:     nestedString(app.Object, "status", "health", "status"),
			HealthMessage:    nestedString(app.Object, "status", "health", "message"),
			OperationPhase:   nestedString(app.Object, "status", "operationState", "phase"),
			OperationMessage: nestedString(app.Object, "status", "operationState", "message"),
		},
	}
}

func EvaluateEvents(previous Snapshot, current ApplicationDetails, clusterName string, observedAt time.Time, infoLabels, sev1Labels []string) []Event {
	events := make([]Event, 0, 2)

	if isRolloutFailure(current.Snapshot) && (!isRolloutFailure(previous) || previous.HealthMessage != current.Snapshot.HealthMessage) {
		events = append(events, Event{
			Type:        EventRolloutFailed,
			Summary:     fmt.Sprintf("[SEV1] Argo Rollout analysis failed for %s", current.Name),
			Description: formatDescription("Argo Rollout analysis failure detected.", current, clusterName, observedAt),
			Labels:      append([]string(nil), sev1Labels...),
		})
	}

	if isSyncFailure(current.Snapshot) && (!isSyncFailure(previous) || previous.OperationMessage != current.Snapshot.OperationMessage) {
		events = append(events, Event{
			Type:        EventSyncFailed,
			Summary:     fmt.Sprintf("Argo CD sync failed for %s", current.Name),
			Description: formatDescription("Argo CD reported a failed sync operation.", current, clusterName, observedAt),
			Labels:      append([]string(nil), infoLabels...),
		})
	}

	if previous.SyncStatus == "OutOfSync" && current.Snapshot.SyncStatus == "Synced" {
		events = append(events, Event{
			Type:        EventDriftCorrected,
			Summary:     fmt.Sprintf("Argo CD drift corrected for %s", current.Name),
			Description: formatDescription("Argo CD reconciled previously out-of-sync application state back to Synced.", current, clusterName, observedAt),
			Labels:      append([]string(nil), infoLabels...),
		})
	}

	return events
}

func isSyncFailure(snapshot Snapshot) bool {
	return snapshot.OperationPhase == "Failed" || snapshot.OperationPhase == "Error"
}

func isRolloutFailure(snapshot Snapshot) bool {
	if snapshot.HealthStatus != "Degraded" {
		return false
	}

	message := strings.ToLower(strings.TrimSpace(snapshot.HealthMessage + " " + snapshot.OperationMessage))
	return strings.Contains(message, "rollout") || strings.Contains(message, "analysis")
}

func formatDescription(prefix string, current ApplicationDetails, clusterName string, observedAt time.Time) string {
	lines := []string{
		prefix,
		"",
		fmt.Sprintf("* Management cluster: %s", clusterName),
		fmt.Sprintf("* Application: %s", current.Name),
		fmt.Sprintf("* Argo CD namespace: %s", current.Namespace),
		fmt.Sprintf("* Project: %s", emptyFallback(current.Project, "default")),
		fmt.Sprintf("* Destination cluster: %s", emptyFallback(current.DestinationName, "unknown")),
		fmt.Sprintf("* Destination namespace: %s", emptyFallback(current.DestinationNamespace, "default")),
		fmt.Sprintf("* Sync status: %s", emptyFallback(current.Snapshot.SyncStatus, "unknown")),
		fmt.Sprintf("* Health status: %s", emptyFallback(current.Snapshot.HealthStatus, "unknown")),
		fmt.Sprintf("* Operation phase: %s", emptyFallback(current.Snapshot.OperationPhase, "unknown")),
		fmt.Sprintf("* Revision: %s", emptyFallback(current.SyncRevision, "unknown")),
		fmt.Sprintf("* Observed at: %s", observedAt.UTC().Format(time.RFC3339)),
	}

	if message := strings.TrimSpace(current.Snapshot.HealthMessage); message != "" {
		lines = append(lines, fmt.Sprintf("* Health message: %s", message))
	}
	if message := strings.TrimSpace(current.Snapshot.OperationMessage); message != "" {
		lines = append(lines, fmt.Sprintf("* Operation message: %s", message))
	}

	return strings.Join(lines, "\n")
}

func nestedString(obj map[string]any, fields ...string) string {
	value, found, err := unstructured.NestedString(obj, fields...)
	if err != nil || !found {
		return ""
	}
	return value
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
