package agents

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"komrad/internal/frames"
	"komrad/internal/harness"
	"komrad/internal/logging"
	agentstore "komrad/internal/store/agent"
	"komrad/internal/tmux"
)

const reconcileMisses = 3

const stateSeenCoalesce = 30 * time.Second

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
	SetAlive(ctx context.Context, ids []string, alive bool) error
}

// NewReconcilerParams is what a Reconciler is built over: a tmux probe for who owns a pane,
// an observer for what each agent is doing, and the state store.
type NewReconcilerParams struct {
	Panes    paneKiller
	Observer harness.Observer
	States   agentStateReadWriter
}

// Reconciler treats the pane as the only authority on an agent's state, letting any other
// signal only hurry along a transition the pane would reach anyway.
type Reconciler struct {
	tmux     paneKiller
	observer harness.Observer
	store    agentStateReadWriter
	frames   frameRecorder
	screens  screenKeeper

	// unrecognizedTotal is only ever touched by the reconcile pass, so it needs none of the
	// locking the tally below does.
	unrecognizedTotal int
	tally             *tally
}

// NewReconciler builds a Reconciler over the probe, observer and store it is given.
func NewReconciler(params NewReconcilerParams) *Reconciler {
	return &Reconciler{
		tmux:     params.Panes,
		observer: params.Observer,
		store:    params.States,
		tally:    newTally(),
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

// WithFrameRecorder attaches somewhere to keep the frame behind each reading, nothing being
// written while it is unset.
func (r *Reconciler) WithFrameRecorder(recorder frameRecorder) *Reconciler {
	r.frames = recorder

	return r
}

// WithScreenKeeper attaches somewhere to save the screen each agent rests on.
func (r *Reconciler) WithScreenKeeper(keeper screenKeeper) *Reconciler {
	r.screens = keeper

	return r
}

// Reconcile observes every tracked agent once and corrects any stored state reality has moved
// past, leaving an agent that already matches untouched.
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
	var came, went []string
	for _, agent := range agents {
		known[agent.ID] = struct{}{}
		pane, inListing := panes[agent.Pane]
		switch alive := isPaneOwnedBy(pane.Owner, agent.ID); {
		case alive && !agent.Alive:
			came = append(came, agent.ID)
		case !alive && agent.Alive:
			went = append(went, agent.ID)
		}
		r.reconcileAgent(ctx, agent, pane, inListing)
	}

	r.setAlive(ctx, came, true)
	r.setAlive(ctx, went, false)
	r.tally.prune(known)
	if r.frames != nil {
		r.frames.Forget(known)
	}
	if r.screens != nil {
		r.screens.Forget(known)
	}

	return nil
}

// setAlive writes the liveness of the agents whose row the pane truth disagreed with.
func (r *Reconciler) setAlive(ctx context.Context, ids []string, alive bool) {
	if len(ids) == 0 {
		return
	}

	if err := r.store.SetAlive(ctx, ids, alive); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to record whose panes came or went",
			append([]slog.Attr{slog.Bool(logging.AgentAlive, alive)}, logging.Err(err)...)...)
	}
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
			r.apply(ctx, agent, decideRestingState(agent))
		}

		return
	}

	if pane.IsRunningShell() {
		if !r.tally.hasShellGraceElapsed(agent.ID) {
			return
		}

		if r.forget(ctx, agent, true) {
			r.apply(ctx, agent, decideRestingState(agent))
		}

		return
	}
	r.tally.clearShell(agent.ID)

	observed, err := r.observer.Observe(ctx, buildTarget(agent))
	if err != nil {
		return
	}

	activity := observed.Activity
	r.recordFrame(ctx, agent, observed.Frame, activity)
	if r.screens != nil {
		r.screens.Keep(ctx, KeepParams{
			Agent:    agent,
			Frame:    observed.Frame,
			Activity: activity,
		})
	}

	switch activity {
	case harness.ActivityWorking:
		r.apply(ctx, agent, agentstore.StateThinking)
	case harness.ActivityWaiting:
		r.apply(ctx, agent, agentstore.StateWaiting)
	case harness.ActivityUnknown:

	case harness.ActivityIdle, harness.ActivityComposing:
		if isTurnFailureStanding(agent) {
			r.apply(ctx, agent, agentstore.StateErrored)

			break
		}

		r.settle(ctx, agent, agentstore.StateIdle)

	case harness.ActivityUnrecognized:
		r.observeUnrecognized(ctx, agent, observed.Frame)
	}
}

// recordFrame keeps the capture a reading was taken from, marking the ones that dispute the
// state on record.
func (r *Reconciler) recordFrame(ctx context.Context, agent agentstore.Agent, frame string, activity harness.Activity) {
	// A source that reads no screen leaves nothing to replay a misreading through.
	if r.frames == nil || frame == "" {
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
		return state != agentstore.StateIdle && state != agentstore.StateErrored
	case harness.ActivityUnrecognized:
		return state != agentstore.StateUncertain

	// A frame nothing could be read from leaves the agent where it was.
	case harness.ActivityUnknown:
		return false
	}

	return false
}

// decideRestingState decides where an agent whose pane stopped moving lands.
func decideRestingState(agent agentstore.Agent) agentstore.State {
	if isTurnFailureStanding(agent) {
		return agentstore.StateErrored
	}

	return agentstore.StateIdle
}

// isTurnFailureStanding reports whether the failed turn on record is the one the pane came to rest from.
func isTurnFailureStanding(agent agentstore.Agent) bool {
	if !agent.IsLastTurnFailed() {
		return false
	}

	return !agent.State.IsTurnUnfinished() || !agent.TurnFailedAt.Before(agent.StateChangedAt)
}

// settle moves an agent to where a frame that has stopped moving lands, taking either the
// agent's own CLI reporting its turn ended or enough passes in a row reading the same thing.
func (r *Reconciler) settle(ctx context.Context, agent agentstore.Agent, state agentstore.State) {
	if agent.State == state {
		r.tally.clearMisses(agent.ID)

		return
	}

	// A reported turn end says the agent finished, so it may only hurry along idle, never the
	// state that says komrad could not tell either way.
	if state == agentstore.StateIdle && r.tally.hasFreshTurnBoundary(agent.ID, agent.StateSeenAt) {
		r.apply(ctx, agent, state)

		return
	}

	if r.tally.recordMiss(agent.ID, state) < reconcileMisses || !r.apply(ctx, agent, state) {
		return
	}

	// A frame komrad could not read is no evidence that a turn ended.
	if state == agentstore.StateIdle {
		r.stampTurnEnd(ctx, agent)
	}
}

// stampTurnEnd records a turn the pane watched stop while nothing reported it.
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

	if r.tally.isUnrecognizedHeld(agent.ID, frames.Hash(frame)) {
		r.tally.clearMisses(agent.ID)

		return
	}

	r.settle(ctx, agent, agentstore.StateUncertain)
}

// forget drops the pane an agent is no longer running in and reports whether the row it was
// handed is still the one the store holds.
func (r *Reconciler) forget(ctx context.Context, agent agentstore.Agent, mine bool) bool {
	if agent.Pane == "" {
		return true
	}

	if mine {
		r.tmux.KillPane(agent.Pane)
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

// apply writes the state a pass settled on and reports whether the store took it.
func (r *Reconciler) apply(ctx context.Context, agent agentstore.Agent, state agentstore.State) bool {
	r.tally.clearDebounce(agent.ID)
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

	return true
}
