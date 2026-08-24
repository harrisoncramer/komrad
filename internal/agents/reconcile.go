package agents

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"weave/internal/harness"
	"weave/internal/logging"
	agentstore "weave/internal/store/agent"
	"weave/internal/tmux"
)

const reconcileMisses = 3

const reconcileShellGrace = 10 * time.Second

const stateSeenCoalesce = 30 * time.Second

const stateSeenBound = 2 * time.Minute

type paneObserver interface {
	GetPanes() (map[string]tmux.PaneStatus, error)
	CapturePane(pane string) string
	ClearPaneOwner(pane string) error
}

type agentStateWriter interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	SetStateByID(ctx context.Context, id, state string) error
	ClearPane(ctx context.Context, id string) error
}

type Reconciler struct {
	tmux      paneObserver
	harnesses harnessGetter
	store     agentStateWriter
	misses    map[string]int
	shells    map[string]time.Time

	now func() time.Time
}

// NewReconciler builds a Reconciler over a tmux probe, the harnesses that read its
// captures, and the state store.
func NewReconciler(t paneObserver, harnesses harnessGetter, s agentStateWriter) *Reconciler {
	return &Reconciler{
		tmux:      t,
		harnesses: harnesses,
		store:     s,
		misses:    map[string]int{},
		shells:    map[string]time.Time{},
		now:       time.Now,
	}
}

// Reconcile observes every tracked agent once and corrects any stored state reality has
// moved past, leaving an agent that already matches untouched.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	agents, err := r.store.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents for reconcile: %w", err)
	}

	panes, err := r.tmux.GetPanes()
	if err != nil {
		return fmt.Errorf("failed to read panes for reconcile: %w", err)
	}

	listed := make(map[string]struct{}, len(agents))
	for _, agent := range agents {
		listed[agent.ID] = struct{}{}
		r.reconcileAgent(ctx, agent, panes[agent.Pane])
	}

	for id := range r.misses {
		if _, ok := listed[id]; !ok {
			delete(r.misses, id)
		}
	}
	for id := range r.shells {
		if _, ok := listed[id]; !ok {
			delete(r.shells, id)
		}
	}

	return nil
}

func (r *Reconciler) reconcileAgent(ctx context.Context, agent agentstore.Agent, pane tmux.PaneStatus) {
	if !isPaneOwnedBy(pane.Owner, agent.ID) {
		r.forget(ctx, agent, false)
		r.apply(ctx, agent, agentstore.StateIdle)

		return
	}

	if pane.RunsShell() {
		since, seen := r.shells[agent.ID]
		if !seen {
			r.shells[agent.ID] = r.now()

			return
		}
		if r.now().Sub(since) < reconcileShellGrace {
			return
		}

		r.forget(ctx, agent, true)
		r.apply(ctx, agent, agentstore.StateIdle)

		return
	}
	delete(r.shells, agent.ID)

	frame := r.tmux.CapturePane(agent.Pane)

	if frame == "" {
		return
	}

	hn, err := r.harnesses.GetByID(agent.Harness)
	if err != nil {
		return
	}

	switch hn.ReadActivity(frame) {
	case harness.ActivityWorking:
		r.apply(ctx, agent, agentstore.StateThinking)
	case harness.ActivityWaiting:
		r.apply(ctx, agent, agentstore.StateWaiting)
	case harness.ActivityUnknown:

	case harness.ActivityIdle:
		if agent.State == agentstore.StateIdle {
			delete(r.misses, agent.ID)

			return
		}

		r.misses[agent.ID]++
		if r.misses[agent.ID] >= reconcileMisses {
			r.apply(ctx, agent, agentstore.StateIdle)
		}
	}
}

func (r *Reconciler) forget(ctx context.Context, agent agentstore.Agent, mine bool) {
	if agent.Pane == "" {
		return
	}

	if mine {
		if err := r.tmux.ClearPaneOwner(agent.Pane); err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to unstamp an abandoned pane",
				append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
		}
	}

	if err := r.store.ClearPane(ctx, agent.ID); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to forget a dormant agent's pane",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

func (r *Reconciler) apply(ctx context.Context, agent agentstore.Agent, state string) {
	delete(r.misses, agent.ID)
	delete(r.shells, agent.ID)
	if agent.State == state && r.now().Sub(agent.StateSeenAt) < stateSeenCoalesce {
		return
	}

	if err := r.store.SetStateByID(ctx, agent.ID, state); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to write an agent's reconciled state",
			append([]slog.Attr{
				slog.String(logging.AgentID, agent.ID),
				slog.String(logging.AgentState, state),
			}, logging.Err(err)...)...)
	}
}

func isStateUnconfirmed(agent agentstore.Agent) bool {
	return time.Since(agent.StateSeenAt) > stateSeenBound
}
