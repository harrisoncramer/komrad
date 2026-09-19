package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"weave/internal/caller"
	"weave/internal/fail"
	"weave/internal/git"
	"weave/internal/logging"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
)

var (
	// ErrRebaseNoWorktree says the agent sits in a directory weave did not cut.
	ErrRebaseNoWorktree = fail.Precondition("this agent has no worktree of its own to rebase")

	// ErrRebaseAdopted says the branch belongs to whoever made it.
	ErrRebaseAdopted = fail.Precondition("this agent is on a branch weave did not cut, so its history is not weave's to rewrite")

	// ErrRebaseDirty says the checkout holds tracked changes.
	ErrRebaseDirty = fail.Precondition("the worktree holds tracked changes, so commit or discard them first")

	// ErrRebaseInProgress says something already owns the index.
	ErrRebaseInProgress = fail.Precondition("the worktree is already part-way through a merge or a rebase, so finish or abort that first")

	// ErrRebaseDetached says the checkout is on no branch, so a rebase would move nothing.
	ErrRebaseDetached = fail.Precondition("the worktree is on no branch, so a rebase would move nothing")

	// ErrRebaseBusy says the agent is not known to have come to rest.
	ErrRebaseBusy = fail.Precondition("the agent is working, so its files must not move under it")

	// ErrRebaseSelf says an agent asked to rebase the checkout it is working in.
	ErrRebaseSelf = fail.InvalidArgument("an agent cannot rebase the checkout it is working in")

	// ErrRebaseShallow says the clone cannot answer what the branch and the base share.
	ErrRebaseShallow = fail.Precondition("the clone is shallow, so what this branch shares with the base cannot be read")

	// ErrRebaseHiddenPaths says the index is holding a path's content back on purpose.
	ErrRebaseHiddenPaths = fail.Precondition("these paths are marked assume-unchanged or skip-worktree, so their contents cannot be read back if the move goes wrong")
)

// rebaseAgentReadWriter is the slice of the agent store a rebase reads and writes.
type rebaseAgentReadWriter interface {
	agentGetter
	SetSequenceInProgressByID(ctx context.Context, id string, inProgress bool) error
}

// worktreeRebaser is the git a rebase reads the checkout through and moves the branch with.
type worktreeRebaser interface {
	ReadTreeState(ctx context.Context, dir string) (git.TreeState, error)
	FetchBase(ctx context.Context, dir, base string) (ref, sha string, err error)
	CountCommitsToReplay(ctx context.Context, dir, ref string) (int, error)
	PredictRebase(ctx context.Context, dir, ref string) (git.Prediction, error)
	Park(ctx context.Context, dir, ref string) (string, error)
	ReleaseRefsUnder(ctx context.Context, dir, prefix, keep string) error
	Rebase(ctx context.Context, dir, ref string) error
	RebaseOnto(ctx context.Context, dir, ref, cutoff string) error
}

type NewRebaseServiceParams struct {
	Agents    rebaseAgentReadWriter
	Workflows workflowReader
	Settler   *Settler
	Notifier  systemMailSender
	Rebaser   worktreeRebaser
	Locks     *TreeLocks
}

// RebaseService moves an agent's existing worktree onto the latest base without replacing it.
type RebaseService struct {
	agents    rebaseAgentReadWriter
	workflows workflowReader
	settler   *Settler
	notifier  systemMailSender
	rebaser   worktreeRebaser
	locks     *TreeLocks
}

// NewRebaseService builds the service from its dependencies.
func NewRebaseService(params NewRebaseServiceParams) *RebaseService {
	locks := params.Locks
	if locks == nil {
		locks = NewTreeLocks()
	}

	return &RebaseService{
		agents:    params.Agents,
		workflows: params.Workflows,
		settler:   params.Settler,
		notifier:  params.Notifier,
		rebaser:   params.Rebaser,
		locks:     locks,
	}
}

// RebasePlan is what a rebase would do.
type RebasePlan struct {
	// BaseRef is what the branch would move onto, and BaseSHA the commit it named when read.
	BaseRef string
	BaseSHA string
	// Commits is how many of the agent's own commits would be replayed.
	Commits int
	Branch  string
	// Cutoff is the newest of the agent's own commits the base already holds whole, which is
	// where the replay starts, empty when the base holds none of them.
	Cutoff string
	// Landed is how many of its commits the base already holds, and Replay how many would move.
	Landed int
	Replay int
	// Merges is how many merge commits a rebase would discard.
	Merges int
	// ConflictPaths are the paths a merge of the base into the branch could not settle.
	ConflictPaths []string
}

// RebaseResult is what a rebase did.
type RebaseResult struct {
	RebasePlan
	// Conflicted says the rebase stopped part-way and is waiting for the agent.
	Conflicted bool
	// Moved says the branch reached the base without replaying anything, which is what a branch
	// whose work has entirely landed gets.
	Moved bool
	// ParkedRef holds the checkout as it stood before any of this, which is what an undo reads.
	ParkedRef string
}

// PlanRebase reads what a rebase would do without touching anything.
func (s *RebaseService) PlanRebase(ctx context.Context, agentID string) (*RebasePlan, error) {
	agent, err := s.eligible(ctx, agentID)
	if err != nil {
		return nil, err
	}

	state, err := s.rebaser.ReadTreeState(ctx, agent.Cwd)
	if err != nil {
		return nil, err
	}
	if err := refuseTreeState(state); err != nil {
		return nil, err
	}

	base := ""
	if flow, err := s.workflows.Get(ctx, agent.WorkflowID); err == nil {
		base = flow.Worktree.BaseBranch
	}

	ref, sha, err := s.rebaser.FetchBase(ctx, agent.Cwd, base)
	if err != nil {
		return nil, err
	}

	commits, err := s.rebaser.CountCommitsToReplay(ctx, agent.Cwd, ref)
	if err != nil {
		return nil, err
	}

	prediction, err := s.rebaser.PredictRebase(ctx, agent.Cwd, ref)
	if err != nil {
		return nil, err
	}

	return &RebasePlan{
		BaseRef:       ref,
		BaseSHA:       sha,
		Commits:       commits,
		Branch:        state.Branch,
		Cutoff:        prediction.Cutoff,
		Landed:        prediction.Landed,
		Replay:        prediction.Replay,
		Merges:        prediction.Merges,
		ConflictPaths: prediction.ConflictPaths,
	}, nil
}

// Rebase moves the agent's branch onto the latest base in place.
func (s *RebaseService) Rebase(ctx context.Context, agentID string) (*RebaseResult, error) {
	agent, err := s.eligible(ctx, agentID)
	if err != nil {
		return nil, err
	}

	// Keyed on the directory, which is what two operations actually collide over.
	unlock, taken := s.locks.TryLock(agent.Cwd)
	if !taken {
		return nil, fmt.Errorf("%w: something else is working in %s", ErrRebaseBusy, agent.Cwd)
	}
	defer unlock()

	// An agent with no pane is skipped rather than refused.
	if s.settler != nil && agent.Pane != "" {
		if _, err := s.settler.Settle(ctx, *agent); err != nil {
			return nil, err
		}
	}

	plan, err := s.PlanRebase(ctx, agentID)
	if err != nil {
		return nil, err
	}

	result := &RebaseResult{RebasePlan: *plan}

	// Parked before anything moves, since a replay from a cutoff drops commits on purpose and
	// the reflog holds refs rather than the files that were never committed.
	result.ParkedRef = buildParkRef(agentID)
	if _, err := s.rebaser.Park(ctx, agent.Cwd, result.ParkedRef); err != nil {
		return nil, err
	}

	if err := s.move(ctx, agent.Cwd, result); err != nil {
		return nil, err
	}

	s.releaseOlderParks(ctx, *agent, result.ParkedRef)

	if result.Conflicted {
		s.recordStopped(ctx, *agent)
	}

	s.tell(ctx, *agent, *result)

	return result, nil
}

// buildParkRef names where a checkout is held before it is moved, so that one agent's parks sort
// together and nothing collides with another's.
func buildParkRef(agentID string) string {
	return fmt.Sprintf("refs/weave/prerefresh/%s/%d", agentID, time.Now().UnixNano())
}

// releaseOlderParks drops the parks this agent left behind before this one, since each pins a
// whole worktree in the shared clone and nothing else ever collects them.
func (s *RebaseService) releaseOlderParks(ctx context.Context, agent agentstore.Agent, keep string) {
	prefix := fmt.Sprintf("refs/weave/prerefresh/%s/", agent.ID)
	if err := s.rebaser.ReleaseRefsUnder(ctx, agent.Cwd, prefix, keep); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to release the older parks of a rebased agent",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

// move brings the branch onto the base, replaying its commits or, when the base already holds
// their content, moving the branch outright rather than colliding with itself.
func (s *RebaseService) move(ctx context.Context, dir string, result *RebaseResult) error {
	// Replayed from the cutoff when the base already holds part of this branch, which is what a
	// squash merge leaves behind: replaying those commits would collide with their own content.
	replay := func() error { return s.rebaser.Rebase(ctx, dir, result.BaseRef) }
	if result.Cutoff != "" {
		replay = func() error { return s.rebaser.RebaseOnto(ctx, dir, result.BaseRef, result.Cutoff) }
	}

	err := replay()
	switch {
	case errors.Is(err, git.ErrRebaseConflicted):
		result.Conflicted = true
	case err != nil:
		return err
	}

	// Landed is what separates a branch whose work all reached the base from one that never had
	// any, which is the commonest press of all and must not be told its commits are gone.
	result.Moved = result.Landed > 0 && result.Replay == 0 && !result.Conflicted

	return nil
}

// recordStopped puts the stopped rebase on the agent's row now, ahead of the pass that would
// otherwise take up to a whole interval to find it.
func (s *RebaseService) recordStopped(ctx context.Context, agent agentstore.Agent) {
	if err := s.agents.SetSequenceInProgressByID(ctx, agent.ID, true); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to record that an agent's rebase stopped part-way",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

// tell says what happened in the agent's mailbox.
func (s *RebaseService) tell(ctx context.Context, agent agentstore.Agent, result RebaseResult) {
	if s.notifier == nil {
		return
	}

	if _, err := s.notifier.SendSystemMail(ctx, agent.ID, rebaseMail(result)); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to tell an agent its worktree was rebased",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

// rebaseMail is what the agent is told, which turns on what became of its branch rather than on
// what was asked for.
func rebaseMail(result RebaseResult) string {
	switch {
	case result.Conflicted:
		return conflictedMail(result)
	case result.Moved:
		return movedMail(result)
	default:
		return replayedMail(result)
	}
}

// parkNote says where the checkout as it stood beforehand is kept, which is the only way back
// to a commit the replay dropped or a file that was never committed.
func parkNote(result RebaseResult) string {
	if result.ParkedRef == "" {
		return ""
	}

	return "\n\nA copy of the worktree from before this ran is kept at " + result.ParkedRef +
		". It holds anything you had not committed, and every commit that is no longer on the branch."
}

// conflictedMail is a rebase stopped part-way, which is the agent's to settle and nobody else's.
func conflictedMail(result RebaseResult) string {
	var body strings.Builder

	fmt.Fprintf(&body, "Your worktree was being rebased onto %s (%s). The rebase hit conflicts and stopped part way, so it is still in progress.\n\n",
		result.BaseRef, short(result.BaseSHA))
	body.WriteString("All your commits are still there. Your branch has not moved. " +
		"Do not resolve the conflicts and do not abort the rebase on your own. " +
		"Say what state you are in and wait to be told what to do. " +
		"The person who pressed the key decides whether this work is worth carrying forward.")

	body.WriteString("\n\nSay also whether you have a pull request open on this branch. " +
		"Finishing the rebase rewrites the commits it was opened from. " +
		"That invalidates an open pull request. It means nothing for one that has already merged.")
	body.WriteString(parkNote(result))

	return body.String()
}

// movedMail is a branch that reached the base without replaying anything, which is what a
// branch whose work has entirely landed gets.
func movedMail(result RebaseResult) string {
	var body strings.Builder

	fmt.Fprintf(&body, "Your branch is now on %s (%s).\n\n", result.BaseRef, short(result.BaseSHA))
	body.WriteString("Your work had already been merged into the base. " +
		"There was nothing left to replay, so your branch was moved straight onto it.\n\n")
	body.WriteString("Nothing was lost. Your own commits are no longer on the branch and the base's history " +
		"has taken their place. Start your next piece of work from here.")
	body.WriteString("\n\nAny pull request opened from this branch no longer tracks anything it carries.")
	body.WriteString(parkNote(result))

	return body.String()
}

// replayedMail is the ordinary rebase, which moved the agent's files under it.
func replayedMail(result RebaseResult) string {
	var body strings.Builder

	if result.Replay == 0 {
		fmt.Fprintf(&body, "Your worktree is up to date with %s (%s). You had no commits of your own to replay.\n\n",
			result.BaseRef, short(result.BaseSHA))
	} else {
		fmt.Fprintf(&body, "Your worktree was rebased onto %s (%s), replaying %d of your own commits.\n\n",
			result.BaseRef, short(result.BaseSHA), result.Replay)
	}
	body.WriteString("Your files may have changed under you. Read them again rather than trusting what you remember. ")
	if result.Landed > 0 {
		fmt.Fprintf(&body, "%d of your commits are no longer on the branch. "+
			"The base already held those changes, so replaying them would have collided with themselves. ", result.Landed)
	}
	body.WriteString("Your branch is still the same branch.")

	body.WriteString("\n\nThe rebase rewrote the commits any pull request on this branch was opened from, " +
		"so do not assume one you opened earlier still tracks your work.")
	body.WriteString(parkNote(result))

	return body.String()
}

// eligible answers the agent when a rebase is its to have, or the reason it is not.
func (s *RebaseService) eligible(ctx context.Context, agentID string) (*agentstore.Agent, error) {
	agent, err := s.agents.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrAgentNotFound.Saying("no agent with id %q", agentID)
	}
	if err != nil {
		return nil, err
	}

	if agent.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: %q", ErrAgentArchived, agent.Name)
	}

	if err := s.refuseTheCaller(ctx, *agent); err != nil {
		return nil, err
	}

	// A rebase types at the agent and then rewrites the branch under its checkout, which is the
	// collision somebody holding that tree stopped it to avoid.
	if agent.HeldAt != nil {
		return nil, ErrAgentHeld.Saying("%q is paused, unpause it first", agent.Name)
	}
	// A state the pane could not be read for says nothing about whether the agent is working.
	if !agent.State.IsAtRest() {
		return nil, fmt.Errorf("%w: %q is %s", ErrRebaseBusy, agent.Name, agent.State)
	}
	if agent.AdoptedBranch != "" {
		return nil, fmt.Errorf("%w: %s", ErrRebaseAdopted, agent.AdoptedBranch)
	}

	// A child runs in its parent's checkout, so this would rewrite the parent's branch.
	if agent.ParentAgentID != nil {
		return nil, fmt.Errorf("%w: it runs in its parent's checkout", ErrRebaseNoWorktree)
	}
	if isProvisionerDisabled(agent.ProvisionEnabled) {
		return nil, fmt.Errorf("%w: it was told to keep the directory it was given", ErrRebaseNoWorktree)
	}
	if agent.Cwd == "" || agent.WorktreeRemoved {
		return nil, fmt.Errorf("%w: it has no directory on disk", ErrRebaseNoWorktree)
	}

	flow, err := s.workflows.Get(ctx, agent.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the workflow of agent %q: %w", agent.Name, err)
	}
	if !flow.Worktree.Enabled {
		return nil, fmt.Errorf("%w: its workflow does not make worktrees", ErrRebaseNoWorktree)
	}

	return agent, nil
}

// refuseTheCaller turns down an agent asking for the checkout it is itself working in, which a
// child shares with its parent.
func (s *RebaseService) refuseTheCaller(ctx context.Context, agent agentstore.Agent) error {
	// Explainer: a rebase interrupts the agent it names and then rewrites the branch under the
	// whole directory, so the harm lands on whoever is working there rather than on whoever the
	// row belongs to. A person pressing the key carries no agent id and is unaffected.
	from := caller.From(ctx).AgentID
	if from == "" {
		return nil
	}
	if from == agent.ID {
		return fmt.Errorf("%w: %q is the one it is working in", ErrRebaseSelf, agent.Name)
	}

	asker, err := s.agents.GetByID(ctx, from)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read the agent asking to rebase %q: %w", agent.Name, err)
	}
	if asker.Cwd == "" || !isSameDir(asker.Cwd, agent.Cwd) {
		return nil
	}

	return fmt.Errorf("%w: %q sits in %s", ErrRebaseSelf, agent.Name, asker.Cwd)
}

// refuseTreeState is what the checkout itself has to say before a rebase may start.
func refuseTreeState(state git.TreeState) error {
	switch {
	case state.InProgress:
		return ErrRebaseInProgress
	case state.Branch == "":
		return ErrRebaseDetached
	case state.Shallow:
		return ErrRebaseShallow
	case len(state.HiddenPaths) > 0:
		return fmt.Errorf("%w: %s", ErrRebaseHiddenPaths, strings.Join(state.HiddenPaths, ", "))
	case state.TrackedChanges:
		return ErrRebaseDirty
	}

	return nil
}
