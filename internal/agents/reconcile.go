package agents

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"slices"
	"time"

	"weave/internal/frames"
	"weave/internal/harness"
	"weave/internal/logging"
	agentstore "weave/internal/store/agent"
	"weave/internal/tmux"
)

const reconcileMisses = 3

const stateSeenCoalesce = 30 * time.Second

const stateSeenBound = 2 * time.Minute

const prLookupTimeout = 15 * time.Second

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

// frameRecorder keeps the pane capture behind a reading.
type frameRecorder interface {
	Record(params frames.RecordParams) error
	Forget(listed map[string]struct{})
}

type agentStateReadWriter interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	SetStateByID(ctx context.Context, id string, state agentstore.State) error
	SetTurnEnded(ctx context.Context, id string) error
	ForgetPane(ctx context.Context, params agentstore.ForgetPaneParams) (bool, error)
	SetMetadata(ctx context.Context, params agentstore.SetMetadataParams) (bool, error)
}

// Reconciler treats the pane as the only authority on an agent's state; any other signal may
// only accelerate a transition the pane would also reach.
type Reconciler struct {
	tmux      paneObserver
	harnesses harnessGetter
	store     agentStateReadWriter
	prFinder  prFinder
	frames    frameRecorder

	// unrecognizedTotal is only ever touched by the reconcile pass, so it needs none of the
	// locking the tally below does.
	unrecognizedTotal int
	tally             *tally
}

// NewReconciler builds a Reconciler over a tmux probe, the harnesses that read its
// captures, and the state store.
func NewReconciler(panes paneObserver, harnesses harnessGetter, states agentStateReadWriter) *Reconciler {
	return &Reconciler{
		tmux:      panes,
		harnesses: harnesses,
		store:     states,
		tally:     newTally(),
	}
}

// RecordTurnBoundary notes that agentID's own CLI reported its turn ending.
func (r *Reconciler) RecordTurnBoundary(agentID string) {
	r.tally.recordTurnBoundary(agentID)
}

// RecordReportedReason notes what agentID's own CLI reported it was doing, which annotates the
// state the pane decided and never moves it.
func (r *Reconciler) RecordReportedReason(agentID, reason string) {
	r.tally.recordReportedReason(agentID, reason)
}

// WithFrameRecorder attaches somewhere for the frame behind each reading to be kept, and
// nothing is written while it is unset.
func (r *Reconciler) WithFrameRecorder(recorder frameRecorder) *Reconciler {
	r.frames = recorder

	return r
}

// WithPRFinder attaches a way to check GitHub for a pull request an agent opened on its own,
// and an agent going idle is never checked while it is unset.
func (r *Reconciler) WithPRFinder(finder prFinder) *Reconciler {
	r.prFinder = finder

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

	known := make(map[string]struct{}, len(agents))
	for _, agent := range agents {
		known[agent.ID] = struct{}{}
		pane, inListing := panes[agent.Pane]
		r.reconcileAgent(ctx, agent, pane, inListing)
	}

	r.tally.prune(known)
	if r.frames != nil {
		r.frames.Forget(known)
	}

	return nil
}

func (r *Reconciler) reconcileAgent(ctx context.Context, agent agentstore.Agent, pane tmux.PaneStatus, inListing bool) {
	if !agent.State.DoesPaneDecide() {
		return
	}

	// An agent holding no pane is missing from every listing, so it has no absence to doubt.
	if agent.Pane != "" && r.tally.isAbsenceDoubted(agent.ID, inListing) {
		return
	}

	if !isPaneOwnedBy(pane.Owner, agent.ID) {
		if r.forget(ctx, agent, false) {
			r.apply(ctx, agent, agentstore.StateIdle)
		}

		return
	}

	if pane.IsRunningShell() {
		if !r.tally.hasShellGraceElapsed(agent.ID) {
			return
		}

		if r.forget(ctx, agent, true) {
			r.apply(ctx, agent, agentstore.StateIdle)
		}

		return
	}
	r.tally.clearShell(agent.ID)

	frame := r.tmux.CapturePane(agent.Pane)

	if frame == "" {
		return
	}

	hn, err := r.harnesses.GetByID(agent.Harness)
	if err != nil {
		return
	}

	activity := hn.ReadActivity(frame)
	r.recordFrame(ctx, agent, frame, activity)

	switch activity {
	case harness.ActivityWorking:
		r.apply(ctx, agent, agentstore.StateThinking)
	case harness.ActivityWaiting:
		r.apply(ctx, agent, agentstore.StateWaiting)
	case harness.ActivityUnknown:

	case harness.ActivityIdle, harness.ActivityComposing:
		r.settle(ctx, agent, agentstore.StateIdle)

	case harness.ActivityUnrecognized:
		r.observeUnrecognized(ctx, agent, frame)
	}
}

// recordFrame keeps the capture a reading was taken from, marking the ones that dispute the
// state on record.
func (r *Reconciler) recordFrame(ctx context.Context, agent agentstore.Agent, frame string, activity harness.Activity) {
	if r.frames == nil {
		return
	}

	err := r.frames.Record(frames.RecordParams{
		AgentID:  agent.ID,
		Frame:    frame,
		Activity: activity.String(),
		Disputes: doesReadingDispute(activity, agent.State),
	})
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to record the frame an agent was read from",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

// doesReadingDispute reports whether a reading would leave an agent somewhere other than the
// state on record.
func doesReadingDispute(activity harness.Activity, state agentstore.State) bool {
	switch activity {
	case harness.ActivityWorking:
		return state != agentstore.StateThinking
	case harness.ActivityWaiting:
		return state != agentstore.StateWaiting

	case harness.ActivityIdle, harness.ActivityComposing:
		return state != agentstore.StateIdle
	case harness.ActivityUnrecognized:
		return state != agentstore.StateUncertain

	// A frame nothing could be read from leaves the agent where it was.
	case harness.ActivityUnknown:
		return false
	}

	return false
}

// settle moves an agent to where a frame that has stopped moving lands, taking either the agent's
// own CLI reporting its turn ended or enough passes in a row reading the same thing.
func (r *Reconciler) settle(ctx context.Context, agent agentstore.Agent, state agentstore.State) {
	if agent.State == state {
		r.tally.clearMisses(agent.ID)

		return
	}

	// A reported turn end says the agent finished, so it may only hurry along idle, never the
	// state that says weave could not tell either way.
	if state == agentstore.StateIdle && r.tally.hasFreshTurnBoundary(agent.ID, agent.StateSeenAt) {
		r.apply(ctx, agent, state)

		return
	}

	if r.tally.recordMiss(agent.ID, state) < reconcileMisses || !r.apply(ctx, agent, state) {
		return
	}

	// The unread mark says an agent finished a turn, which a frame weave could not read is no
	// evidence of.
	if state == agentstore.StateIdle {
		r.stampTurnEnd(ctx, agent)
	}
}

// stampTurnEnd raises the unread mark for a turn the pane watched stop while nothing reported
// it, so a harness with no end-of-turn hook still marks its agents unread.
func (r *Reconciler) stampTurnEnd(ctx context.Context, agent agentstore.Agent) {
	if err := r.store.SetTurnEnded(ctx, agent.ID); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to stamp a turn the pane settled",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

// observeUnrecognized holds a frame that keeps changing instead of demoting it, and settles the
// agent as uncertain once the frame goes static or the hold is capped.
func (r *Reconciler) observeUnrecognized(ctx context.Context, agent agentstore.Agent, frame string) {
	r.unrecognizedTotal++
	logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "a captured frame matched no known activity pattern",
		slog.String(logging.AgentID, agent.ID),
		slog.Int(logging.UnrecognizedFrameCount, r.unrecognizedTotal))

	if r.tally.isUnrecognizedHeld(agent.ID, hashFrame(frame)) {
		r.tally.clearMisses(agent.ID)

		return
	}

	r.settle(ctx, agent, agentstore.StateUncertain)
}

func hashFrame(frame string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(frame))

	return h.Sum64()
}

// forget drops the pane an agent is no longer running in and reports whether the row it was
// handed is still the one the store holds, since a resume landing mid-pass replaces the pane
// the walk is judging by.
func (r *Reconciler) forget(ctx context.Context, agent agentstore.Agent, mine bool) bool {
	if agent.Pane == "" {
		return true
	}

	if mine {
		if err := r.tmux.ClearPaneOwner(agent.Pane); err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to unstamp an abandoned pane",
				append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
		}
	}

	// A pane that goes part-way through a turn took that turn with it, and nothing brings it
	// back, so the resume has to be told to ask for it again.
	var interrupted *time.Time
	if agent.State.IsTurnUnfinished() {
		at := r.tally.now()
		interrupted = &at
	}

	forgotten, err := r.store.ForgetPane(ctx, agentstore.ForgetPaneParams{
		ID:            agent.ID,
		Pane:          agent.Pane,
		UpdatedAt:     agent.UpdatedAt,
		InterruptedAt: interrupted,
	})
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to forget a dormant agent's pane",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)

		return false
	}

	return forgotten
}

// apply writes the state a pass settled on and reports whether the store took it, which the
// caller needs before it hangs anything else off the transition.
func (r *Reconciler) apply(ctx context.Context, agent agentstore.Agent, state agentstore.State) bool {
	r.tally.clearDebounce(agent.ID)
	justWentIdle := state == agentstore.StateIdle && agent.State != agentstore.StateIdle
	if agent.State == state && r.tally.now().Sub(agent.StateSeenAt) < stateSeenCoalesce {
		return false
	}

	if err := r.store.SetStateByID(ctx, agent.ID, state); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to write an agent's reconciled state",
			append([]slog.Attr{
				slog.String(logging.AgentID, agent.ID),
				slog.String(logging.AgentState, string(state)),
			}, logging.Err(err)...)...)

		return false
	}

	if justWentIdle {
		r.checkPR(ctx, agent)
	}

	return true
}

// checkPR looks for a pull request an agent opened, once as the agent goes idle, and records
// the first one it finds without ever checking whether it later merges or closes.
func (r *Reconciler) checkPR(ctx context.Context, agent agentstore.Agent) {
	if r.prFinder == nil || agent.Branch == nil || *agent.Branch == "" {
		return
	}
	if slices.ContainsFunc(agent.Metadata, func(m agentstore.Metadata) bool { return m.Key == prMetadataKey }) {
		return
	}
	if r.tally.isRateLimited(agent.ID) {
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
				r.tally.backOff(agent.ID)
			}

			logging.From(bg).LogAttrs(bg, slog.LevelDebug, "failed to check for a pull request",
				append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)

			return
		}
		if url == "" {
			return
		}

		if _, err := r.store.SetMetadata(bg, agentstore.SetMetadataParams{
			ID:    agent.ID,
			Key:   prMetadataKey,
			Value: &url,
		}); err != nil {
			logging.From(bg).LogAttrs(bg, slog.LevelError, "failed to record a pull request found for an agent",
				append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
		}
	}()
}

func isStateUnconfirmed(agent agentstore.Agent) bool {
	return time.Since(agent.StateSeenAt) > stateSeenBound
}
