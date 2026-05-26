package jirabridge

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

const stateKey = "state.json"

// ApplicationsGVR is the GroupVersionResource for the argoproj.io
// Application CRD. Exported so cmd's main can declare it next to the
// dynamic-client wiring.
var ApplicationsGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "applications",
}

// IssueCreator is the contract Runner needs to turn an Event into a
// Jira issue. cmd's main wires this to the JSON-over-HTTP Jira REST
// API client; tests use the fake here.
type IssueCreator interface {
	CreateIssue(ctx context.Context, event Event) error
}

// Runner is the per-binary runner for argocd-jira-bridge (FR-V4-24).
// One Runner = one reconcile loop polling ArgoCD Applications and
// creating Jira issues on state transitions.
type Runner struct {
	Config        config.JiraBridge
	KubeClient    kubernetes.Interface
	DynamicClient dynamic.Interface
	JiraClient    IssueCreator
	Logger        *log.Logger

	// Clock + Now wrap time.Now() so unit tests can pin observed-at.
	Now func() time.Time
}

func (r *Runner) logf(format string, args ...any) {
	if r.Logger != nil {
		r.Logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Run reconciles every PollInterval until ctx is cancelled. Returns
// nil on graceful shutdown.
func (r *Runner) Run(ctx context.Context) error {
	if err := r.Reconcile(ctx); err != nil {
		r.logf("initial reconcile failed: %v", err)
	}

	ticker := time.NewTicker(r.Config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.Reconcile(ctx); err != nil {
				r.logf("reconcile failed: %v", err)
			}
		}
	}
}

// Reconcile snapshots ArgoCD Applications, diffs against the
// last-seen state, and creates Jira issues for events. Exported so
// tests can drive a single pass.
func (r *Runner) Reconcile(ctx context.Context) error {
	stateCM, state, err := loadState(ctx, r.KubeClient, r.Config)
	if err != nil {
		return err
	}

	apps, err := r.DynamicClient.Resource(ApplicationsGVR).Namespace(r.Config.ArgoCDNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list argocd applications: %w", err)
	}

	nextState := State{Applications: map[string]ApplicationState{}}

	for i := range apps.Items {
		app := &apps.Items[i]
		current := SnapshotFromApplication(app)
		previous := state.Applications[current.Name].Snapshot

		events := EvaluateEvents(previous, current, r.Config.ClusterName, r.now(), r.Config.JiraInfoIssueLabels, r.Config.JiraSev1IssueLabels)
		for _, event := range events {
			if err := r.JiraClient.CreateIssue(ctx, event); err != nil {
				return fmt.Errorf("create jira issue for %s: %w", current.Name, err)
			}
		}

		nextState.Applications[current.Name] = ApplicationState{Snapshot: current.Snapshot}
	}

	encoded, err := nextState.Encode()
	if err != nil {
		return err
	}
	if stateCM.Data == nil {
		stateCM.Data = map[string]string{}
	}
	stateCM.Data[stateKey] = encoded

	if _, err := r.KubeClient.CoreV1().ConfigMaps(r.Config.StateNamespace).Update(ctx, stateCM, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update state configmap: %w", err)
	}

	return nil
}

func loadState(ctx context.Context, kubeClient kubernetes.Interface, cfg config.JiraBridge) (*corev1.ConfigMap, State, error) {
	configMaps := kubeClient.CoreV1().ConfigMaps(cfg.StateNamespace)
	stateCM, err := configMaps.Get(ctx, cfg.StateConfigMapName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		stateCM = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cfg.StateConfigMapName,
				Namespace: cfg.StateNamespace,
			},
			Data: map[string]string{
				stateKey: `{"applications":{}}`,
			},
		}
		created, createErr := configMaps.Create(ctx, stateCM, metav1.CreateOptions{})
		if createErr != nil {
			return nil, State{}, fmt.Errorf("create state configmap: %w", createErr)
		}
		stateCM = created
		err = nil
	}
	if err != nil {
		return nil, State{}, fmt.Errorf("get state configmap: %w", err)
	}

	state, err := ParseState(stateCM.Data[stateKey])
	if err != nil {
		return nil, State{}, err
	}

	return stateCM, state, nil
}
