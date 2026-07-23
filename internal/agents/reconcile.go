package agents

import (
	"context"
	"fmt"
	"strings"

	"claude-agents/internal/store"
)

// workingMarker is the text Claude Code shows while it is busy generating or
// running a tool. It stays on screen continuously from the moment a turn starts
// until Claude returns to its prompt, and it disappears the instant the turn is
// interrupted, so finding it in a captured frame is the ground truth for "this
// agent is still working". A long-running tool keeps the marker up via its live
// timer, which is why the pane is a better signal than transcript activity. If
// Claude's wording ever changes, this is the one place to update.
const workingMarker = "esc to interrupt"

// reconcileMisses is how many consecutive idle observations a thinking agent
// must show before it is settled to done. Requiring two guards against a single
// capture landing on a redraw frame between tool calls, where the marker can be
// momentarily absent even though work is ongoing.
const reconcileMisses = 2

// tmuxProbe is the slice of the tmux client the reconciler observes through.
type tmuxProbe interface {
	HasSession(name string) bool
	CapturePane(pane string) string
}

// stateStore is the slice of the store the reconciler reads and corrects.
type stateStore interface {
	List(ctx context.Context) ([]store.Agent, error)
	SetState(ctx context.Context, name, state string) error
}

// Reconciler is the level-triggered safety net for agent state. The lifecycle
// hooks are edge-triggered: they advance state only when an event fires, so any
// ending that fires no event leaves the state stranded. The clearest case is an
// escape interrupt, which stops the work without a stop hook and leaves the
// agent stuck "thinking" forever; a crash or an externally killed tmux session
// strand it the same way. This loop periodically observes each agent's real
// state from tmux and corrects a stored state that no longer matches, so a
// missed edge self-heals on the next pass instead of lingering.
type Reconciler struct {
	tmux   tmuxProbe
	store  stateStore
	misses map[string]int // agent id -> consecutive idle observations
}

// NewReconciler builds a Reconciler over a tmux probe and the state store.
func NewReconciler(t tmuxProbe, s stateStore) *Reconciler {
	return &Reconciler{tmux: t, store: s, misses: map[string]int{}}
}

// Reconcile observes every tracked agent once and corrects any stored state that
// reality has moved past. It is idempotent: an agent already matching reality is
// left untouched, so running it repeatedly is safe and cheap.
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

	// Forget miss counts for agents that no longer list, so the map cannot grow
	// without bound as chats come and go.
	for id := range r.misses {
		if _, ok := listed[id]; !ok {
			delete(r.misses, id)
		}
	}

	return nil
}

// reconcileAgent settles a single agent toward what tmux shows is true.
func (r *Reconciler) reconcileAgent(ctx context.Context, agent store.Agent) {
	// A finished agent stays finished until the user prompts it again, so there
	// is nothing to reconcile.
	if agent.State == store.StateDone {
		return
	}

	// The tmux session is gone: the agent is dormant and certainly not working.
	// Settle it to done so the status bar and counts stop treating a vanished
	// agent as active. Restoring it later resets it to new.
	if !r.tmux.HasSession(agent.Name) {
		r.settle(ctx, agent, store.StateDone)
		return
	}

	// Only a thinking agent can be stranded by a missed edge. waiting is set and
	// cleared by hooks that do fire when Claude blocks and unblocks, and new has
	// not started work yet, so both are left to the hooks.
	if agent.State != store.StateThinking {
		delete(r.misses, agent.ID)
		return
	}

	if r.working(agent.Pane) {
		delete(r.misses, agent.ID)
		return
	}

	// Thinking, but the pane shows no work in flight. Demote only after enough
	// consecutive idle observations to rule out a transient redraw frame.
	r.misses[agent.ID]++
	if r.misses[agent.ID] >= reconcileMisses {
		r.settle(ctx, agent, store.StateDone)
	}
}

// working reports whether the pane's current frame shows Claude actively working.
func (r *Reconciler) working(pane string) bool {
	if pane == "" {
		return false
	}

	return strings.Contains(r.tmux.CapturePane(pane), workingMarker)
}

// settle writes a corrected state and clears the agent's miss count. A no-op
// when the stored state already matches, so an unchanged agent never touches the
// store.
func (r *Reconciler) settle(ctx context.Context, agent store.Agent, state string) {
	delete(r.misses, agent.ID)
	if agent.State == state {
		return
	}

	_ = r.store.SetState(ctx, agent.Name, state)
}
