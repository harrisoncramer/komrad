package agents

import (
	"context"
	"fmt"

	"weave/internal/harness"
	agentstore "weave/internal/store/agent"
)

// reconcileMisses is how many consecutive idle observations a thinking agent must show
// before it settles to idle, which guards against a capture landing on a redraw frame
// between tool calls. It errs high, since wrongly demoting a live agent is worse than a
// stale state lingering a few seconds.
const reconcileMisses = 3

// tmuxProbe is the part of the tmux client the reconciler observes through. It keys on the
// pane's owner stamp rather than its existence, so a reissued pane id is not read as this
// agent still running.
type tmuxProbe interface {
	PaneOwners() (map[string]string, error)
	CapturePane(pane string) string
}

// activityRegistry resolves the harness an agent is recorded against, since what a frame
// means depends on which CLI drew it. A row naming one nothing implements settles nothing.
type activityRegistry interface {
	GetByID(id string) (harness.Harness, error)
}

type stateStore interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	SetStateByID(ctx context.Context, id, state string) error
	ClearPane(ctx context.Context, id string) error
}

// Reconciler is the single source of truth for agent state. Rather than react to an agent
// CLI's lifecycle events, which fire unreliably and miss endings like an escape interrupt,
// it observes each agent from tmux on a timer and writes the state that implies. Reading
// ground truth every pass means a wrong state cannot outlive the interval.
type Reconciler struct {
	tmux      tmuxProbe
	harnesses activityRegistry
	store     stateStore
	misses    map[string]int // agent id to consecutive idle observations
}

// NewReconciler builds a Reconciler over a tmux probe, the harnesses that read its
// captures, and the state store.
func NewReconciler(t tmuxProbe, harnesses activityRegistry, s stateStore) *Reconciler {
	return &Reconciler{
		tmux:      t,
		harnesses: harnesses,
		store:     s,
		misses:    map[string]int{},
	}
}

// Reconcile observes every tracked agent once and corrects any stored state reality has
// moved past, leaving an agent that already matches untouched.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	agents, err := r.store.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents for reconcile: %w", err)
	}

	// Tmux failing to answer says nothing about any agent, and treating it as every pane
	// being gone would clear every pane id and demote every agent in one pass.
	owners, err := r.tmux.PaneOwners()
	if err != nil {
		return fmt.Errorf("failed to read pane owners for reconcile: %w", err)
	}

	listed := make(map[string]struct{}, len(agents))
	for _, agent := range agents {
		listed[agent.ID] = struct{}{}
		r.reconcileAgent(ctx, agent, owners[agent.Pane])
	}

	// Miss counts for agents that no longer list are forgotten, so the map cannot grow
	// without bound.
	for id := range r.misses {
		if _, ok := listed[id]; !ok {
			delete(r.misses, id)
		}
	}

	return nil
}

// reconcileAgent writes the single agent's state to match what tmux shows.
func (r *Reconciler) reconcileAgent(ctx context.Context, agent agentstore.Agent, owner string) {
	// A pane that is not this agent's means the agent is dormant, whether its pane died or
	// the tmux server has since given that id to somebody else.
	if !paneOwnedBy(owner, agent.ID) {
		// A pane id kept on a dormant row is the one that collides after a server restart,
		// so forgetting it stops a later pass, a preview, or a kill from landing on
		// another agent's pane.
		if agent.Pane != "" {
			_ = r.store.ClearPane(ctx, agent.ID)
		}
		r.apply(ctx, agent, agentstore.StateIdle)

		return
	}

	frame := r.tmux.CapturePane(agent.Pane)

	// An empty capture means the pane could not be read rather than that the agent went
	// idle, and every uncertain case is a no-op.
	if frame == "" {
		return
	}

	// A harness this binary does not implement can say nothing about the frame, so the
	// agent keeps the state it has.
	resolved, err := r.harnesses.GetByID(agent.Harness)
	if err != nil {
		return
	}

	switch resolved.ReadActivity(frame) {
	case harness.ActivityWorking:
		r.apply(ctx, agent, agentstore.StateThinking)
	case harness.ActivityWaiting:
		r.apply(ctx, agent, agentstore.StateWaiting)
	case harness.ActivityUnknown:
		// The harness read the frame and could not tell, which stands where a capture
		// that could not be read does.
	case harness.ActivityIdle:
		if agent.State == agentstore.StateIdle {
			delete(r.misses, agent.ID)

			return
		}

		// Idle, but was active. Demoting waits for enough consecutive idle observations
		// to rule out a redraw between tool calls.
		r.misses[agent.ID]++
		if r.misses[agent.ID] >= reconcileMisses {
			r.apply(ctx, agent, agentstore.StateIdle)
		}
	}
}

// apply writes a state and clears the agent's miss count, doing nothing when the stored
// state already matches.
func (r *Reconciler) apply(ctx context.Context, agent agentstore.Agent, state string) {
	delete(r.misses, agent.ID)
	if agent.State == state {
		return
	}

	_ = r.store.SetStateByID(ctx, agent.ID, state)
}
