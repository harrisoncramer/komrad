package agents

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
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

const prLookupTimeout = 15 * time.Second

// prRateLimitBackoff is how long a rate-limited agent is left alone before another pull
// request check is attempted for it. This is a single, infrequent check per agent rather
// than a poll loop, so a long, simple pause is worth more than the queueing and per-window
// budget a real poller needs.
const prRateLimitBackoff = 15 * time.Minute

// prMetadataKey is the metadata key a found pull request is recorded under, the same key
// weave's own agents have been asked to record it under by hand.
const prMetadataKey = "pr"

// prFinder looks up the URL of an open pull request for a branch, an empty string with no
// error meaning there isn't one yet.
type prFinder interface {
	FindPR(ctx context.Context, dir, branch string) (string, error)
}

type paneObserver interface {
	GetPanes() (map[string]tmux.PaneStatus, error)
	CapturePane(pane string) string
	ClearPaneOwner(pane string) error
}

type agentStateWriter interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	SetStateByID(ctx context.Context, id, state string) error
	ClearPane(ctx context.Context, id string) error
	SetMetadata(ctx context.Context, params agentstore.SetMetadataParams) (bool, error)
}

// Reconciler treats the pane as the only authority on an agent's state; any other signal may
// only accelerate a transition the pane would also reach.
type Reconciler struct {
	tmux      paneObserver
	harnesses harnessGetter
	store     agentStateWriter
	prFinder  prFinder
	misses    map[string]int
	shells    map[string]time.Time

	// rateLimitedUntil is written from the goroutine a pull request check runs on, so it
	// is the one piece of state here that is not confined to the single-threaded
	// reconcile pass and needs its own lock.
	rateLimitMu      sync.Mutex
	rateLimitedUntil map[string]time.Time

	now func() time.Time
}

// NewReconciler builds a Reconciler over a tmux probe, the harnesses that read its
// captures, and the state store.
func NewReconciler(t paneObserver, harnesses harnessGetter, s agentStateWriter) *Reconciler {
	return &Reconciler{
		tmux:             t,
		harnesses:        harnesses,
		store:            s,
		misses:           map[string]int{},
		shells:           map[string]time.Time{},
		rateLimitedUntil: map[string]time.Time{},
		now:              time.Now,
	}
}

// WithPRFinder attaches a way to check GitHub for a pull request an agent opened on its
// own, outside weave's own tools, so it never chases something nobody asked it to look
// for. Left unset, an agent going idle is never checked.
func (r *Reconciler) WithPRFinder(f prFinder) *Reconciler {
	r.prFinder = f

	return r
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

	r.rateLimitMu.Lock()
	for id := range r.rateLimitedUntil {
		if _, ok := listed[id]; !ok {
			delete(r.rateLimitedUntil, id)
		}
	}
	r.rateLimitMu.Unlock()

	return nil
}

func (r *Reconciler) reconcileAgent(ctx context.Context, agent agentstore.Agent, pane tmux.PaneStatus) {
	if agent.State == agentstore.StateSettingUp || agentstore.IsFailed(agent.State) {
		return
	}

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
	justWentIdle := state == agentstore.StateIdle && agent.State != agentstore.StateIdle
	if agent.State == state && r.now().Sub(agent.StateSeenAt) < stateSeenCoalesce {
		return
	}

	if err := r.store.SetStateByID(ctx, agent.ID, state); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to write an agent's reconciled state",
			append([]slog.Attr{
				slog.String(logging.AgentID, agent.ID),
				slog.String(logging.AgentState, state),
			}, logging.Err(err)...)...)

		return
	}

	if justWentIdle {
		r.checkPR(ctx, agent)
	}
}

// checkPR looks for a pull request an agent opened on a branch weave already knows about,
// the moment the agent goes idle rather than on every pass, so a long-idle agent is not
// asked about over and over. Once one is found it is recorded for good: nothing here ever
// checks whether it later merges or closes.
func (r *Reconciler) checkPR(ctx context.Context, agent agentstore.Agent) {
	if r.prFinder == nil || agent.Branch == nil || *agent.Branch == "" {
		return
	}
	if slices.ContainsFunc(agent.Metadata, func(m agentstore.Metadata) bool { return m.Key == prMetadataKey }) {
		return
	}
	if r.isRateLimited(agent.ID) {
		return
	}

	bg := context.WithoutCancel(ctx)
	branch := *agent.Branch

	go func() {
		lookupCtx, cancel := context.WithTimeout(bg, prLookupTimeout)
		defer cancel()

		url, err := r.prFinder.FindPR(lookupCtx, agent.Cwd, branch)
		if err != nil {
			if isGHRateLimitError(err) {
				r.backOff(agent.ID)
			}

			logging.From(bg).LogAttrs(bg, slog.LevelDebug, "failed to check for a pull request",
				append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)

			return
		}
		if url == "" {
			return
		}

		if _, err := r.store.SetMetadata(bg, agentstore.SetMetadataParams{ID: agent.ID, Key: prMetadataKey, Value: &url}); err != nil {
			logging.From(bg).LogAttrs(bg, slog.LevelError, "failed to record a pull request found for an agent",
				append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
		}
	}()
}

func (r *Reconciler) isRateLimited(agentID string) bool {
	r.rateLimitMu.Lock()
	defer r.rateLimitMu.Unlock()

	until, ok := r.rateLimitedUntil[agentID]

	return ok && r.now().Before(until)
}

func (r *Reconciler) backOff(agentID string) {
	r.rateLimitMu.Lock()
	defer r.rateLimitMu.Unlock()

	r.rateLimitedUntil[agentID] = r.now().Add(prRateLimitBackoff)
}

func isStateUnconfirmed(agent agentstore.Agent) bool {
	return time.Since(agent.StateSeenAt) > stateSeenBound
}
