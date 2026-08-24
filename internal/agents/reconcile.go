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

// reconcileMisses is how many consecutive idle observations a thinking agent must show
// before it settles to idle, which guards against a capture landing on a redraw frame
// between tool calls. It errs high, since wrongly demoting a live agent is worse than a
// stale state lingering a few seconds.
const reconcileMisses = 3

// reconcileShellGrace is how long an agent's pane must sit at a shell before its CLI is
// taken for gone. The launch command is typed into that shell, so between the pane opening
// and the CLI taking it over a live launch looks exactly like an exit. It is wall clock
// rather than a count of passes because the passes are not evenly spaced: anything driving
// the reconciler harder than the service does would otherwise burn through a count while
// the CLI was still starting. Both CLIs weave ships take about a second to hold their pane.
const reconcileShellGrace = 10 * time.Second

// paneObserver is how the reconciler watches a pane. It keys on the pane's owner stamp
// rather than its existence, so a reissued pane id is not read as this agent still running,
// and on the pane's foreground program, because the pane outlives the CLI launched in it.
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

// Reconciler is the single source of truth for agent state. Rather than react to an agent
// CLI's lifecycle events, which fire unreliably and miss endings like an escape interrupt,
// it observes each agent from tmux on a timer and writes the state that implies. Reading
// ground truth every pass means a wrong state cannot outlive the interval.
type Reconciler struct {
	tmux      paneObserver
	harnesses harnessGetter
	store     agentStateWriter
	misses    map[string]int       // agent id to consecutive idle observations
	shells    map[string]time.Time // agent id to when its pane was first seen back at a shell
	// now is the clock the shell grace is measured on, which a test replaces.
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

	// Tmux failing to answer says nothing about any agent, and treating it as every pane
	// being gone would clear every pane id and demote every agent in one pass.
	panes, err := r.tmux.GetPanes()
	if err != nil {
		return fmt.Errorf("failed to read panes for reconcile: %w", err)
	}

	listed := make(map[string]struct{}, len(agents))
	for _, agent := range agents {
		listed[agent.ID] = struct{}{}
		r.reconcileAgent(ctx, agent, panes[agent.Pane])
	}

	// What is remembered about agents that no longer list is forgotten, so neither map can
	// grow without bound.
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

// reconcileAgent writes the single agent's state to match what tmux shows.
func (r *Reconciler) reconcileAgent(ctx context.Context, agent agentstore.Agent, pane tmux.PaneStatus) {
	// A pane that is not this agent's means the agent is dormant, whether its pane died or
	// the tmux server has since given that id to somebody else.
	if !isPaneOwnedBy(pane.Owner, agent.ID) {
		r.forget(ctx, agent, false)
		r.apply(ctx, agent, agentstore.StateIdle)

		return
	}

	// A pane of this agent's back at a shell is dormant too. The launch command is typed
	// into a shell rather than run as the pane's process, so an exited CLI leaves the shell
	// holding a pane that is still alive, still stamped, and still showing whatever the CLI
	// drew last. Ownership alone reads that as a healthy idle agent, which is the one state
	// nothing can reach, since what gets typed at it goes to the shell.
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

	// An empty capture means the pane could not be read rather than that the agent went
	// idle, and every uncertain case is a no-op.
	if frame == "" {
		return
	}

	// A harness this binary does not implement can say nothing about the frame, so the
	// agent keeps the state it has.
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

// forget drops a dormant agent's claim on its pane. A pane id kept on a dormant row is the
// one that collides after a server restart, so forgetting it stops a later pass, a preview,
// or a kill from landing on another agent's pane. When the pane was this agent's, weave's
// stamp comes off it too: the window is left standing, since what the CLI drew last is the
// only account of how it ended, but a pane weave has stopped tracking should not go on
// naming an owner.
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

// apply writes a state and clears the agent's miss count, doing nothing when the stored
// state already matches.
func (r *Reconciler) apply(ctx context.Context, agent agentstore.Agent, state string) {
	delete(r.misses, agent.ID)
	delete(r.shells, agent.ID)
	if agent.State == state {
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
