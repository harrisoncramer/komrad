package checkouts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"komrad/internal/agents"
	"komrad/internal/caller"
	"komrad/internal/fail"
	"komrad/internal/git"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	pullrequeststore "komrad/internal/store/pullrequest"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/xpath"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

var (
	// ErrNoWorktree says the agent sits in a directory komrad did not cut.
	ErrNoWorktree = fail.Precondition("this agent has no worktree of its own for komrad to act on")

	// ErrBusy says something else is already moving the worktree.
	ErrBusy = fail.Precondition("something else is already moving this worktree")

	// ErrSelf says an agent asked komrad to act on the checkout it is working in.
	ErrSelf = fail.InvalidArgument("an agent cannot rebase, push or abort in the checkout it is working in")
)

// agentReadWriter is the slice of the agent store a move of a checkout reads and writes.
type agentReadWriter interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
	SetSequenceByID(ctx context.Context, id string, sequence agentstore.Sequence) error
	GetLastRebaseByID(ctx context.Context, id string) (json.RawMessage, error)
}

// workflowReader reads the workflow an agent belongs to.
type workflowReader interface {
	Get(ctx context.Context, id string) (*workflowstore.Workflow, error)
}

// agentSettler brings an agent to rest, reporting whether a turn was cut to do it.
type agentSettler interface {
	Settle(ctx context.Context, agent agentstore.Agent) (bool, error)
}

// eventRecorder queues the event that tells an agent what became of its worktree.
type eventRecorder interface {
	InsertManyTx(ctx context.Context, build func(tx pgx.Tx) ([]river.JobArgs, error)) error
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

// worktreePusher is the git a push reads the checkout through and sends the branch with.
type worktreePusher interface {
	ReadTreeState(ctx context.Context, dir string) (git.TreeState, error)
	CountAgainstRemote(ctx context.Context, dir, branch string) (*git.RemoteStanding, error)
	ResolveBase(ctx context.Context, dir, base string) (string, error)
	CountAheadBehind(ctx context.Context, dir, ref string) (ahead, behind int, err error)
	PeekChanges(ctx context.Context, dir string) (git.Changes, error)
	Push(ctx context.Context, params git.PushParams) (string, error)
}

// worktreeAborter is the git an abort reads the checkout through and undoes the sequence with.
type worktreeAborter interface {
	ReadTreeState(ctx context.Context, dir string) (git.TreeState, error)
	ReadSequence(ctx context.Context, dir string) (git.Sequence, error)
	ReadRebaseOnto(ctx context.Context, dir string) (string, error)
	Park(ctx context.Context, dir, ref string) (string, error)
	AbortSequence(ctx context.Context, dir string, sequence git.Sequence) error
}

// pullRequestLister reads the pull requests stored for a branch.
type pullRequestLister interface {
	ListPage(ctx context.Context, params pullrequeststore.ListPageParams) ([]pullrequeststore.PullRequest, error)
}

// loopWaker asks a background loop to run before its next tick.
type loopWaker interface {
	Wake()
}

// NewServiceParams is what the service is built over.
type NewServiceParams struct {
	Agents          agentReadWriter
	Workflows       workflowReader
	Settler         agentSettler
	Events          eventRecorder
	Rebaser         worktreeRebaser
	Pusher          worktreePusher
	Aborter         worktreeAborter
	PullRequests    pullRequestLister
	Locks           *TreeLocks
	CheckoutSync    loopWaker
	PullRequestSync loopWaker
}

// Service rebases, pushes and aborts in an agent's existing worktree on a person's say-so.
type Service struct {
	agents          agentReadWriter
	workflows       workflowReader
	settler         agentSettler
	events          eventRecorder
	rebaser         worktreeRebaser
	pusher          worktreePusher
	aborter         worktreeAborter
	pullRequests    pullRequestLister
	locks           *TreeLocks
	checkoutSync    loopWaker
	pullRequestSync loopWaker
}

// NewService builds the service from its dependencies.
func NewService(params NewServiceParams) *Service {
	locks := params.Locks
	if locks == nil {
		locks = NewTreeLocks()
	}

	return &Service{
		agents:          params.Agents,
		workflows:       params.Workflows,
		settler:         params.Settler,
		events:          params.Events,
		rebaser:         params.Rebaser,
		pusher:          params.Pusher,
		aborter:         params.Aborter,
		pullRequests:    params.PullRequests,
		locks:           locks,
		checkoutSync:    params.CheckoutSync,
		pullRequestSync: params.PullRequestSync,
	}
}

// settle brings the agent to rest before its files move, reporting whether a turn was cut.
func (s *Service) settle(ctx context.Context, agent agentstore.Agent) (bool, error) {
	if agent.Pane == "" {
		return false, nil
	}

	return s.settler.Settle(ctx, agent)
}

// buildParkRef names where a checkout is held before it is moved.
func buildParkRef(agentID string) string {
	return fmt.Sprintf("refs/komrad/prerefresh/%s/%d", agentID, time.Now().UnixNano())
}

// eligible answers the agent when its worktree is komrad's to act on, or the reason it is not.
func (s *Service) eligible(ctx context.Context, agentID string) (*agentstore.Agent, error) {
	agent, err := s.read(ctx, agentID)
	if err != nil {
		return nil, err
	}

	if err := s.refuseTheCaller(ctx, *agent); err != nil {
		return nil, err
	}

	if agent.State == agentstore.StateSettingUp {
		return nil, fmt.Errorf("%w: %q is still being set up", ErrBusy, agent.Name)
	}

	// A child runs in its parent's checkout, so this would act on the parent's branch.
	if agent.ParentAgentID != nil {
		return nil, fmt.Errorf("%w: it runs in its parent's checkout", ErrNoWorktree)
	}
	if agent.IsProvisionerDisabled() {
		return nil, fmt.Errorf("%w: it was told to keep the directory it was given", ErrNoWorktree)
	}
	if agent.Cwd == "" || agent.WorktreeRemoved {
		return nil, fmt.Errorf("%w: it has no directory on disk", ErrNoWorktree)
	}

	flow, err := s.workflows.Get(ctx, agent.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the workflow of agent %q: %w", agent.Name, err)
	}
	if !flow.Worktree.Enabled {
		return nil, fmt.Errorf("%w: its workflow does not make worktrees", ErrNoWorktree)
	}

	return agent, nil
}

// read fetches the row, refusing an agent that is archived or that a person is holding.
func (s *Service) read(ctx context.Context, agentID string) (*agentstore.Agent, error) {
	agent, err := s.agents.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, agents.ErrAgentNotFound.Saying("no agent with id %q", agentID)
	}
	if err != nil {
		return nil, err
	}

	if agent.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: %q", agents.ErrAgentArchived, agent.Name)
	}

	if agent.HeldAt != nil {
		return nil, agents.ErrAgentHeld.Saying("%q is paused, unpause it first", agent.Name)
	}

	return agent, nil
}

// refuseTheCaller turns down an agent asking for the checkout it is itself working in, which a
// child shares with its parent.
func (s *Service) refuseTheCaller(ctx context.Context, agent agentstore.Agent) error {
	from := caller.From(ctx).AgentID
	if from == "" {
		return nil
	}
	if from == agent.ID {
		return fmt.Errorf("%w: %q is the one it is working in", ErrSelf, agent.Name)
	}

	asker, err := s.agents.GetByID(ctx, from)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read the agent asking to rebase %q: %w", agent.Name, err)
	}
	if asker.Cwd == "" || !xpath.IsSameDir(asker.Cwd, agent.Cwd) {
		return nil
	}

	return fmt.Errorf("%w: %q sits in %s", ErrSelf, agent.Name, asker.Cwd)
}
