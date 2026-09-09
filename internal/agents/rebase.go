package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"weave/internal/git"
	"weave/internal/logging"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
)

var (
	// ErrRebaseNoWorktree says the agent sits in a directory weave did not cut.
	ErrRebaseNoWorktree = errors.New("this agent has no worktree of its own to rebase")

	// ErrRebaseAdopted says the branch belongs to whoever made it.
	ErrRebaseAdopted = errors.New("this agent is on a branch weave did not cut, so its history is not weave's to rewrite")

	// ErrRebaseDirty says the checkout holds tracked changes.
	ErrRebaseDirty = errors.New("the worktree holds tracked changes, so commit or discard them first")

	// ErrRebaseInProgress says something already owns the index.
	ErrRebaseInProgress = errors.New("the worktree is already part-way through a merge or a rebase, so finish or abort that first")

	// ErrRebaseDetached says the checkout is on no branch, so a rebase would move nothing.
	ErrRebaseDetached = errors.New("the worktree is on no branch, so a rebase would move nothing")

	// ErrRebaseBusy says the agent is part-way through a turn.
	ErrRebaseBusy = errors.New("the agent is working, so its files must not move under it")
)

// rebaseAgentReadWriter is the slice of the agent store a rebase reads and writes.
type rebaseAgentReadWriter interface {
	agentGetter
	metadataSetter
}

type NewRebaseServiceParams struct {
	Agents      rebaseAgentReadWriter
	Workflows   workflowReader
	Interrupter agentInterrupter
	Notifier    systemMailSender
	Locks       *TreeLocks
}

// RebaseService moves an agent's existing worktree onto the latest base without replacing it.
type RebaseService struct {
	agents      rebaseAgentReadWriter
	workflows   workflowReader
	interrupter agentInterrupter
	notifier    systemMailSender
	locks       *TreeLocks
}

// NewRebaseService builds the service from its dependencies.
func NewRebaseService(params NewRebaseServiceParams) *RebaseService {
	locks := params.Locks
	if locks == nil {
		locks = NewTreeLocks()
	}

	return &RebaseService{
		agents:      params.Agents,
		workflows:   params.Workflows,
		interrupter: params.Interrupter,
		notifier:    params.Notifier,
		locks:       locks,
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
	// SameTree says the branch and the base name one tree, so replaying would only collide.
	SameTree bool
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
	// FastForwarded says the branch was moved onto the base outright rather than replayed,
	// which is what a branch whose content the base already holds gets instead.
	FastForwarded bool
	// ClearedPullRequest says a recorded pull request was dropped, since the rebase rewrote the
	// branch it was opened from.
	ClearedPullRequest bool
}

// PlanRebase reads what a rebase would do without touching anything.
func (s *RebaseService) PlanRebase(ctx context.Context, agentID string) (*RebasePlan, error) {
	agent, err := s.eligible(ctx, agentID)
	if err != nil {
		return nil, err
	}

	state, err := git.ReadTreeState(ctx, agent.Cwd)
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

	ref, sha, err := git.FetchBase(ctx, agent.Cwd, base)
	if err != nil {
		return nil, err
	}

	commits, err := git.CountCommitsToReplay(ctx, agent.Cwd, ref)
	if err != nil {
		return nil, err
	}

	prediction, err := git.PredictRebase(ctx, agent.Cwd, ref)
	if err != nil {
		return nil, err
	}

	return &RebasePlan{
		BaseRef:       ref,
		BaseSHA:       sha,
		Commits:       commits,
		Branch:        state.Branch,
		SameTree:      prediction.SameTree,
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

	plan, err := s.PlanRebase(ctx, agentID)
	if err != nil {
		return nil, err
	}

	// The interrupt keys are sent before the files move, without waiting for the CLI to read
	// them, and an agent with no pane is skipped rather than refused.
	if s.interrupter != nil && agent.Pane != "" {
		if err := s.interrupter.Interrupt(ctx, agentID); err != nil {
			return nil, fmt.Errorf("failed to interrupt agent %q before rebasing it: %w", agent.Name, err)
		}
	}

	result := &RebaseResult{RebasePlan: *plan}

	if err := s.move(ctx, agent.Cwd, result); err != nil {
		return nil, err
	}

	// A completed rebase rewrites the branch, where a stopped one has not moved it yet.
	if !result.Conflicted {
		result.ClearedPullRequest = s.clearPullRequest(ctx, *agent)
	}

	s.tell(ctx, *agent, *result)

	return result, nil
}

// move brings the branch onto the base, replaying its commits or, when the base already holds
// their content, moving the branch outright rather than colliding with itself.
func (s *RebaseService) move(ctx context.Context, dir string, result *RebaseResult) error {
	if result.SameTree {
		if err := git.FastForward(ctx, dir, result.BaseRef); err != nil {
			return err
		}
		result.FastForwarded = true

		return nil
	}

	err := git.Rebase(ctx, dir, result.BaseRef)
	switch {
	case errors.Is(err, git.ErrRebaseConflicted):
		result.Conflicted = true
	case err != nil:
		return err
	}

	return nil
}

// clearPullRequest drops the recorded pull request and reports whether there was one.
func (s *RebaseService) clearPullRequest(ctx context.Context, agent agentstore.Agent) bool {
	if metadataFor(agent, prMetadataKey) == "" {
		return false
	}

	if _, err := s.agents.SetMetadata(ctx, agentstore.SetMetadataParams{
		ID:  agent.ID,
		Key: prMetadataKey,
	}); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to clear the pull request of a rebased agent",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)

		return false
	}

	return true
}

// tell says what happened in the agent's mailbox.
func (s *RebaseService) tell(ctx context.Context, agent agentstore.Agent, result RebaseResult) {
	if s.notifier == nil {
		return
	}

	if _, err := s.notifier.SendSystemMail(ctx, agent.ID, rebaseMail(agent, result)); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to tell an agent its worktree was rebased",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

// rebaseMail is what the agent is told, which turns on what became of its branch rather than on
// what was asked for.
func rebaseMail(agent agentstore.Agent, result RebaseResult) string {
	switch {
	case result.Conflicted:
		return conflictedMail(agent, result)
	case result.FastForwarded:
		return fastForwardedMail(result)
	default:
		return replayedMail(result)
	}
}

// conflictedMail is a rebase stopped part-way, which is the agent's to settle and nobody else's.
func conflictedMail(agent agentstore.Agent, result RebaseResult) string {
	var body strings.Builder

	fmt.Fprintf(&body, "Your worktree was rebased onto %s (%s) and the rebase stopped on conflicts, so it is still in progress.\n\n",
		result.BaseRef, short(result.BaseSHA))
	body.WriteString("Your commits are all still there, and your branch has not moved yet. " +
		"Do not resolve it or abort it on your own initiative: say what state you are in and wait to be told " +
		"which way to go, since whoever pressed the key is the one deciding whether this work is worth " +
		"carrying forward.")

	if metadataFor(agent, prMetadataKey) != "" {
		body.WriteString(" Say too whether the pull request recorded against you is still open, since finishing " +
			"the rebase rewrites the commits it was opened from: that invalidates an open one, and means nothing " +
			"for one already merged.")
	}

	return body.String()
}

// fastForwardedMail is a branch moved onto the base outright, which is what a branch whose work
// the base already holds gets instead of a replay that would collide with itself.
func fastForwardedMail(result RebaseResult) string {
	var body strings.Builder

	fmt.Fprintf(&body, "Your branch was moved onto %s (%s) outright rather than rebased.\n\n", result.BaseRef, short(result.BaseSHA))
	body.WriteString("The base already held every change your own commits made, which is what happens once your work " +
		"lands there as one squashed commit, so replaying them would only have collided with your own files. ")
	body.WriteString("No file on disk changed and nothing was lost. Your own commits are no longer on the branch, " +
		"and the base's history stands in their place, so start your next piece of work from here.")

	if result.ClearedPullRequest {
		body.WriteString(" The pull request recorded against you has been cleared, since it no longer tracks anything " +
			"this branch carries.")
	}

	return body.String()
}

// replayedMail is the ordinary rebase, which moved the agent's files under it.
func replayedMail(result RebaseResult) string {
	var body strings.Builder

	fmt.Fprintf(&body, "Your worktree was rebased onto %s (%s), replaying %d of your own commits.\n\n",
		result.BaseRef, short(result.BaseSHA), result.Commits)
	body.WriteString("Your files may have changed under you, so read them again rather than trusting what you remember. " +
		"Nothing was lost and your branch is the same branch.")

	if result.ClearedPullRequest {
		body.WriteString(" The pull request recorded against you has been cleared, because the rebase rewrote the " +
			"branch it was opened from: open a new one rather than assuming the old one still tracks your work.")
	}

	return body.String()
}

// eligible answers the agent when a rebase is its to have, or the reason it is not.
func (s *RebaseService) eligible(ctx context.Context, agentID string) (*agentstore.Agent, error) {
	agent, err := s.agents.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrAgentNotFound
	}
	if err != nil {
		return nil, err
	}

	if agent.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: %q", ErrAgentArchived, agent.Name)
	}
	if agent.State.IsTurnUnfinished() {
		return nil, fmt.Errorf("%w: %q", ErrRebaseBusy, agent.Name)
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

// refuseTreeState is what the checkout itself has to say before a rebase may start.
func refuseTreeState(state git.TreeState) error {
	switch {
	case state.InProgress:
		return ErrRebaseInProgress
	case state.Branch == "":
		return ErrRebaseDetached
	case state.TrackedChanges:
		return ErrRebaseDirty
	}

	return nil
}
