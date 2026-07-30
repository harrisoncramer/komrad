package agents

import (
	"context"
	"fmt"

	"claude-agents/internal/harness"
	agentstore "claude-agents/internal/store/agent"
)

// reconcileMisses is how many consecutive idle observations a thinking agent must
// show before it settles to done. Requiring several guards against a capture landing
// on a redraw frame between tool calls, where the live UI is momentarily absent even
// though work is ongoing. Erring high matters because wrongly demoting a live agent is
// far worse than a stale state lingering a few seconds.
const reconcileMisses = 3

// tmuxProbe is the part of the tmux client the reconciler observes through.
// Observation keys on the pane's owner stamp rather than the pane's mere existence, so
// a pane id the tmux server has reissued to somebody else is not read as this agent
// still running.
type tmuxProbe interface {
	PaneOwner(pane string) string
	CapturePane(pane string) string
}

// activityProbe is all the reconciler needs from a harness, what a captured frame
// says the agent is doing. Which chrome that verdict is read out of belongs to the
// harness, since it differs by CLI and moves between releases of one.
type activityProbe interface {
	Activity(frame string) harness.Activity
}

type stateStore interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	SetStateByID(ctx context.Context, id, state string) error
	ClearPane(ctx context.Context, id string) error
}

// Reconciler is the single source of truth for agent state. Rather than react to an
// agent CLI's lifecycle events, which fire unreliably and miss endings like an escape
// interrupt, it observes each agent directly from tmux on a timer and writes the state
// that observation implies. Because it reads ground truth every pass, a wrong state
// cannot outlive the interval and there are no edges to miss.
type Reconciler struct {
	tmux    tmuxProbe
	harness activityProbe
	store   stateStore
	misses  map[string]int // agent id to consecutive idle observations
}

// NewReconciler builds a Reconciler over a tmux probe, the harness that reads its
// captures, and the state store.
func NewReconciler(t tmuxProbe, h activityProbe, s stateStore) *Reconciler {
	return &Reconciler{tmux: t, harness: h, store: s, misses: map[string]int{}}
}

// Reconcile observes every tracked agent once and corrects any stored state that
// reality has moved past. An agent already matching reality is left untouched, so
// running it repeatedly is safe and cheap.
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
	// without bound as agents come and go.
	for id := range r.misses {
		if _, ok := listed[id]; !ok {
			delete(r.misses, id)
		}
	}

	return nil
}

// reconcileAgent writes the single agent's state to match what tmux shows.
func (r *Reconciler) reconcileAgent(ctx context.Context, agent agentstore.Agent) {
	// A pane that is not this agent's means the agent is dormant and cannot be working,
	// whether its pane died or the tmux server has since given that id to somebody else.
	if !paneOwnedBy(r.tmux.PaneOwner(agent.Pane), agent.ID) {
		// A pane id kept on a dormant row is the one that collides after a server restart,
		// so forgetting it here is what stops a later pass, a stage preview, or a kill from
		// landing on another agent's pane.
		if agent.Pane != "" {
			_ = r.store.ClearPane(ctx, agent.ID)
		}
		if agent.State == agentstore.StateThinking || agent.State == agentstore.StateWaiting {
			r.apply(ctx, agent, agentstore.StateDone)
		} else {
			delete(r.misses, agent.ID)
		}
		return
	}

	frame := r.tmux.CapturePane(agent.Pane)

	// An empty capture means the pane could not be read, not that the agent went idle. A
	// live agent always shows at least its input box, so this is inconclusive and every
	// uncertain case is a no-op.
	if frame == "" {
		return
	}

	switch r.harness.Activity(frame) {
	case harness.ActivityWorking:
		r.apply(ctx, agent, agentstore.StateThinking)
	case harness.ActivityWaiting:
		r.apply(ctx, agent, agentstore.StateWaiting)
	case harness.ActivityUnknown:
		// The harness read the frame and could not tell, which stands exactly where a
		// capture that could not be read does.
	case harness.ActivityIdle:
		if agent.State == agentstore.StateThinking || agent.State == agentstore.StateWaiting {
			// Idle, but was active. Demoting waits for enough consecutive idle observations to
			// rule out a transient redraw between tool calls.
			r.misses[agent.ID]++
			if r.misses[agent.ID] >= reconcileMisses {
				r.apply(ctx, agent, agentstore.StateDone)
			}

			return
		}

		delete(r.misses, agent.ID)
	}
}

// apply writes a state and clears the agent's miss count, doing nothing when the stored
// state already matches so an unchanged agent never touches the store.
func (r *Reconciler) apply(ctx context.Context, agent agentstore.Agent, state string) {
	delete(r.misses, agent.ID)
	if agent.State == state {
		return
	}

	_ = r.store.SetStateByID(ctx, agent.ID, state)
}
