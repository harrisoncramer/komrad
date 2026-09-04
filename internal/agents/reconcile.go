package agents

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"weave/internal/frames"
	"weave/internal/harness"
	"weave/internal/logging"
	agentstore "weave/internal/store/agent"
	"weave/internal/tmux"
)

const reconcileMisses = 3

const unrecognizedHoldLimit = 5

const reconcileShellGrace = 10 * time.Second

// listingsBeforePaneGone is how many pane listings in a row have to leave a pane out before it
// is believed gone, since a listing that came back short reads exactly like one that came back
// right and forgetting a live pane costs the agent a binding nothing restores.
const listingsBeforePaneGone = 3

const stateSeenCoalesce = 30 * time.Second

const stateSeenBound = 2 * time.Minute

const prLookupTimeout = 15 * time.Second

// prRateLimitBackoff is how long a rate-limited agent is left alone before its pull request is
// checked again, kept long because this is one infrequent check per agent rather than a poll
// loop.
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

// frameRecorder keeps the pane capture behind a reading, which is nothing at all until a
// recording is turned on.
type frameRecorder interface {
	Record(params frames.RecordParams) error
	Forget(listed map[string]struct{})
}

type agentStateWriter interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	SetStateByID(ctx context.Context, id string, state agentstore.State) error
	ForgetPane(ctx context.Context, params agentstore.ForgetPaneParams) (bool, error)
	SetMetadata(ctx context.Context, params agentstore.SetMetadataParams) (bool, error)
}

// Reconciler treats the pane as the only authority on an agent's state; any other signal may
// only accelerate a transition the pane would also reach.
type Reconciler struct {
	tmux      paneObserver
	harnesses harnessGetter
	store     agentStateWriter
	prFinder  prFinder
	frames    frameRecorder

	// mu guards tracked and every field of the entries inside it, since an entry is
	// reached by the reconcile pass, by the RPC handler recording a turn boundary, and by
	// the goroutine a pull request check runs on.
	mu                sync.Mutex
	tracked           map[string]trackedAgent
	unrecognizedTotal int
	now               func() time.Time
}

// trackedAgent is what one pass leaves the next to work from about a single agent, all of it
// counters and timestamps the panes drove rather than anything the store holds.
type trackedAgent struct {
	misses            int
	listingsMissing   int
	shellSince        time.Time
	turnBoundary      time.Time
	rateLimitedUntil  time.Time
	frameHash         uint64
	frameHashSeen     bool
	unrecognizedHolds int
}

// NewReconciler builds a Reconciler over a tmux probe, the harnesses that read its
// captures, and the state store.
func NewReconciler(panes paneObserver, harnesses harnessGetter, states agentStateWriter) *Reconciler {
	return &Reconciler{
		tmux:      panes,
		harnesses: harnesses,
		store:     states,
		tracked:   map[string]trackedAgent{},
		now:       time.Now,
	}
}

// RecordTurnBoundary notes that agentID's own CLI reported its turn ending.
func (r *Reconciler) RecordTurnBoundary(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.tracked[agentID]
	entry.turnBoundary = r.now()
	r.tracked[agentID] = entry
}

func (r *Reconciler) hasFreshTurnBoundary(agentID string, since time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.tracked[agentID].turnBoundary.After(since)
}

func (r *Reconciler) recordMiss(agentID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.tracked[agentID]
	entry.misses++
	r.tracked[agentID] = entry

	return entry.misses
}

func (r *Reconciler) missCount(agentID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.tracked[agentID].misses
}

func (r *Reconciler) clearMisses(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.tracked[agentID]
	if !ok {
		return
	}

	entry.misses = 0
	r.tracked[agentID] = entry
}

// clearDebounce drops what an agent had accrued towards a transition once one has been
// decided, leaving the frame hash and any rate limit, which both outlive a change of state.
func (r *Reconciler) clearDebounce(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.tracked[agentID]
	if !ok {
		return
	}

	entry.misses = 0
	entry.listingsMissing = 0
	entry.shellSince = time.Time{}
	entry.turnBoundary = time.Time{}
	entry.unrecognizedHolds = 0
	r.tracked[agentID] = entry
}

// holdMissingPane reports whether a pane the listing left out is to be waited on rather than
// forgotten, which it is until enough listings in a row have agreed the pane is gone.
func (r *Reconciler) holdMissingPane(agentID string, inListing bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.tracked[agentID]
	if inListing {
		// A pane that was never missing has nothing to return, and writing anyway would put an
		// agent no frame has been read for into tracked.
		if ok && entry.listingsMissing > 0 {
			entry.listingsMissing = 0
			r.tracked[agentID] = entry
		}

		return false
	}

	entry.listingsMissing++
	r.tracked[agentID] = entry

	return entry.listingsMissing < listingsBeforePaneGone
}

// hasShellGraceElapsed reports whether an agent's pane has been showing a bare shell for longer
// than the grace a CLI gets to start, noting the first sighting when this is one.
func (r *Reconciler) hasShellGraceElapsed(agentID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.tracked[agentID]
	if entry.shellSince.IsZero() {
		entry.shellSince = r.now()
		r.tracked[agentID] = entry

		return false
	}

	return r.now().Sub(entry.shellSince) >= reconcileShellGrace
}

func (r *Reconciler) clearShell(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.tracked[agentID]
	if !ok {
		return
	}

	entry.shellSince = time.Time{}
	r.tracked[agentID] = entry
}

// holdUnrecognized records the frame an agent is sitting on and reports whether it differs
// from the one before it, which is the reason to wait rather than demote, up to the limit on
// how long a frame that never settles may hold.
func (r *Reconciler) holdUnrecognized(agentID string, hash uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.tracked[agentID]
	changed := entry.frameHashSeen && hash != entry.frameHash
	entry.frameHash = hash
	entry.frameHashSeen = true

	hold := changed && entry.unrecognizedHolds < unrecognizedHoldLimit
	if hold {
		entry.unrecognizedHolds++
	}
	r.tracked[agentID] = entry

	return hold
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

	r.prune(known)
	if r.frames != nil {
		r.frames.Forget(known)
	}

	return nil
}

// prune forgets the agents the store no longer holds, so an archived or deleted one leaves
// nothing behind to accrue against its id if it ever comes back.
func (r *Reconciler) prune(known map[string]struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	maps.DeleteFunc(r.tracked, func(id string, _ trackedAgent) bool {
		_, stillKnown := known[id]

		return !stillKnown
	})
}

func (r *Reconciler) reconcileAgent(ctx context.Context, agent agentstore.Agent, pane tmux.PaneStatus, inListing bool) {
	if !agent.State.DoesPaneDecide() {
		return
	}

	// An agent holding no pane is missing from every listing, so it has no absence to doubt.
	if agent.Pane != "" && r.holdMissingPane(agent.ID, inListing) {
		return
	}

	if !isPaneOwnedBy(pane.Owner, agent.ID) {
		if r.forget(ctx, agent, false) {
			r.apply(ctx, agent, agentstore.StateIdle)
		}

		return
	}

	if pane.IsRunningShell() {
		if !r.hasShellGraceElapsed(agent.ID) {
			return
		}

		if r.forget(ctx, agent, true) {
			r.apply(ctx, agent, agentstore.StateIdle)
		}

		return
	}
	r.clearShell(agent.ID)

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
		r.settleIdle(ctx, agent)

	case harness.ActivityUnrecognized:
		r.observeUnrecognized(ctx, agent, frame)
	}
}

// recordFrame keeps the capture a reading was taken from while a recording is on, marking the
// ones the state on record does not agree with, which are the readings somebody turned it on
// to find.
func (r *Reconciler) recordFrame(ctx context.Context, agent agentstore.Agent, frame string, activity harness.Activity) {
	if r.frames == nil {
		return
	}

	err := r.frames.Record(frames.RecordParams{
		AgentID:  agent.ID,
		Frame:    frame,
		Activity: activity.String(),
		Disputes: stateFor(activity) != agent.State,
	})
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to record the frame an agent was read from",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

// stateFor is the state a reading would leave an agent in, which is what a recording compares
// against the state on record to tell a reading worth keeping from one that changes nothing.
func stateFor(activity harness.Activity) agentstore.State {
	switch activity {
	case harness.ActivityWorking:
		return agentstore.StateThinking
	case harness.ActivityWaiting:
		return agentstore.StateWaiting
	case harness.ActivityIdle, harness.ActivityComposing:
		return agentstore.StateIdle
	case harness.ActivityUnknown, harness.ActivityUnrecognized:
		return ""
	}

	return ""
}

// settleIdle is what any frame the harness treats as an ordinary finished state runs
// through, whether or not the harness positively recognised it.
func (r *Reconciler) settleIdle(ctx context.Context, agent agentstore.Agent) {
	if agent.State == agentstore.StateIdle {
		r.clearMisses(agent.ID)

		return
	}

	if r.hasFreshTurnBoundary(agent.ID, agent.StateSeenAt) {
		r.apply(ctx, agent, agentstore.StateIdle)

		return
	}

	if r.recordMiss(agent.ID) >= reconcileMisses {
		r.apply(ctx, agent, agentstore.StateIdle)
	}
}

// observeUnrecognized holds a frame that keeps changing instead of demoting it, and
// falls through to settleIdle once the frame goes static or the hold is capped.
func (r *Reconciler) observeUnrecognized(ctx context.Context, agent agentstore.Agent, frame string) {
	r.unrecognizedTotal++
	logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "a captured frame matched no known activity pattern",
		slog.String(logging.AgentID, agent.ID),
		slog.Int(logging.UnrecognizedFrameCount, r.unrecognizedTotal))

	if r.holdUnrecognized(agent.ID, hashFrame(frame)) {
		r.clearMisses(agent.ID)

		return
	}

	r.settleIdle(ctx, agent)
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
		at := r.now()
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

func (r *Reconciler) apply(ctx context.Context, agent agentstore.Agent, state agentstore.State) {
	r.clearDebounce(agent.ID)
	justWentIdle := state == agentstore.StateIdle && agent.State != agentstore.StateIdle
	if agent.State == state && r.now().Sub(agent.StateSeenAt) < stateSeenCoalesce {
		return
	}

	if err := r.store.SetStateByID(ctx, agent.ID, state); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to write an agent's reconciled state",
			append([]slog.Attr{
				slog.String(logging.AgentID, agent.ID),
				slog.String(logging.AgentState, string(state)),
			}, logging.Err(err)...)...)

		return
	}

	if justWentIdle {
		r.checkPR(ctx, agent)
	}
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

func (r *Reconciler) isRateLimited(agentID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.now().Before(r.tracked[agentID].rateLimitedUntil)
}

func (r *Reconciler) backOff(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.tracked[agentID]
	entry.rateLimitedUntil = r.now().Add(prRateLimitBackoff)
	r.tracked[agentID] = entry
}

func isStateUnconfirmed(agent agentstore.Agent) bool {
	return time.Since(agent.StateSeenAt) > stateSeenBound
}
