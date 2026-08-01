package agents

import (
	"context"
	"fmt"

	"weave/internal/harness"
	agentstore "weave/internal/store/agent"
)

// reconcileMisses is how many consecutive idle observations a thinking agent must show
// before it settles to idle, which guards against a capture landing on a redraw between
// tool calls and against a CLI that has not published its state yet. It errs high, since
// wrongly demoting a live agent is worse than a stale state lingering a few seconds. It
// applies only to an inferred verdict: a CLI reporting its own idleness is not guessing
// and settles at once.
const reconcileMisses = 3

// tmuxProbe is the part of the tmux client the reconciler observes through. It keys on the
// pane's owner stamp rather than its existence, so a reissued pane id is not read as this
// agent still running.
type tmuxProbe interface {
	PaneOwner(pane string) string
	CapturePane(pane string) string
}

// activityRegistry resolves the harness an agent is recorded against, since what an agent
// is doing is read differently per CLI. A row naming one nothing implements settles
// nothing.
type activityRegistry interface {
	Resolve(id string) (harness.Harness, error)
}

type stateStore interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	SetStateByID(ctx context.Context, id, state string) error
	ClearPane(ctx context.Context, id string) error
}

// Reconciler is the single source of truth for agent state. Rather than react to an agent
// CLI's lifecycle events, which fire unreliably and miss endings like an escape interrupt,
// it observes each agent on a timer and writes the state that implies. Reading ground
// truth every pass means a wrong state cannot outlive the interval. What an observation
// is made of belongs to the harness, since one CLI publishes what it is doing and another
// only draws it.
type Reconciler struct {
	tmux      tmuxProbe
	harnesses activityRegistry
	store     stateStore
	misses    map[string]int // agent id to consecutive idle observations
}

// NewReconciler builds a Reconciler over a tmux probe, the harnesses that read its
// captures, and the state store.
func NewReconciler(t tmuxProbe, harnesses activityRegistry, s stateStore) *Reconciler {
	return &Reconciler{tmux: t, harnesses: harnesses, store: s, misses: map[string]int{}}
}

// Reconcile observes every tracked agent once and corrects any stored state reality has
// moved past, leaving an agent that already matches untouched.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	agents, err := r.store.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents for reconcile: %w", err)
	}

	listed := make(map[string]struct{}, len(agents))
	for _, agent := range agents {
		listed[agent.ID] = struct{}{}
		r.reconcileAgent(ctx, agent)
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
func (r *Reconciler) reconcileAgent(ctx context.Context, agent agentstore.Agent) {
	// A pane that is not this agent's means the agent is dormant, whether its pane died or
	// the tmux server has since given that id to somebody else.
	if !paneOwnedBy(r.tmux.PaneOwner(agent.Pane), agent.ID) {
		// A pane id kept on a dormant row is the one that collides after a server restart,
		// so forgetting it stops a later pass, a preview, or a kill from landing on
		// another agent's pane.
		if agent.Pane != "" {
			_ = r.store.ClearPane(ctx, agent.ID)
		}
		r.apply(ctx, agent, agentstore.StateIdle)

		return
	}

	// A harness this binary does not implement can say nothing about the agent, so it
	// keeps the state it has.
	resolved, err := r.harnesses.Resolve(agent.Harness)
	if err != nil {
		return
	}

	reading := resolved.ReadActivity(harness.Observation{
		Frame:     r.tmux.CapturePane(agent.Pane),
		SessionID: agent.SessionID,
	})

	switch reading.Activity {
	case harness.ActivityWorking:
		r.apply(ctx, agent, agentstore.StateThinking)
	case harness.ActivityWaiting:
		r.apply(ctx, agent, agentstore.StateWaiting)
	case harness.ActivityUnknown:
		// The harness looked and could not tell, which is where a pane that could not
		// be captured lands. Every uncertain case is a no-op.
	case harness.ActivityIdle:
		if agent.State == agentstore.StateIdle || reading.Reported {
			r.apply(ctx, agent, agentstore.StateIdle)

			return
		}

		// Idle by inference, but was active. Demoting waits for enough consecutive idle
		// observations to rule out a redraw between tool calls.
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
