package checkouts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"komrad/internal/caller"
	"komrad/internal/events"
	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/logging"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	pullrequeststore "komrad/internal/store/pullrequest"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/wake"
	"komrad/lib/xpath"
	"komrad/proto/gen/oneofs"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

const (
	parkRebase parkSpace = "prerefresh"
	parkAbort  parkSpace = "aborted"
)

var (
	// ErrNoWorktree says the task has no checkout komrad cut for it.
	ErrNoWorktree = fail.Precondition("this task has no worktree of its own for komrad to act on")

	// errBusy says something else is already moving the worktree.
	errBusy = fail.Precondition("something else is already moving this worktree")

	// errWriterNotAtRest says the task's writer is not between turns.
	errWriterNotAtRest = fail.Precondition("the task's writer is not at rest")

	// errSelf says an agent asked komrad to act on the checkout it is working in.
	errSelf = fail.InvalidArgument("an agent cannot rebase, push or abort in the checkout it is working in")
)

// parkSpace is the ref namespace a park sits under.
type parkSpace string

// checkoutTarget is a task whose checkout komrad may act on, with the workflow it was cut for and
// its live writer, nil when it has none.
type checkoutTarget struct {
	task     *taskstore.Task
	workflow *workflowstore.Workflow
	writer   *agentstore.Agent
	// ontoBase re-targets a child onto the workflow's base branch, away from its parent.
	ontoBase bool
}

// dir is the checkout the target is acted on in.
func (t checkoutTarget) dir() string {
	return t.task.Checkout.Cwd
}

// taskFinder reads the task a caller named by its id or by its slug.
type taskFinder interface {
	FindTask(ctx context.Context, task oneofs.Task) (*taskstore.Task, error)
}

// txRunner runs a function inside one transaction.
type txRunner interface {
	InTx(ctx context.Context, fn func(store.DBTX) error) error
}

// forgeFinder is the forge a pull request is changed through, found by its address.
type forgeFinder interface {
	ForAddress(ctx context.Context, address string) (string, forge.Forge, error)
}

// eventRecorder queues the event that tells an agent what became of its worktree.
type eventRecorder interface {
	InsertManyTx(ctx context.Context, build func(tx pgx.Tx) ([]river.JobArgs, error)) error
}

// branchMover is the git a rebase, push or abort reads the checkout through and moves the branch with.
type branchMover interface {
	ReadTreeState(ctx context.Context, dir string) (git.TreeState, error)
	FetchBase(ctx context.Context, dir, base string) (ref, sha string, err error)
	CountCommitsToReplay(ctx context.Context, dir, ref string) (int, error)
	PredictRebase(ctx context.Context, dir, ref string) (git.Prediction, error)
	Park(ctx context.Context, dir, ref string) (string, error)
	ReleaseRefsUnder(ctx context.Context, dir, prefix, keep string) error
	Rebase(ctx context.Context, dir, ref string) error
	RebaseOnto(ctx context.Context, dir, ref, cutoff string) error
	CountAgainstRemote(ctx context.Context, dir, branch string) (*git.RemoteStanding, error)
	RefreshRemoteBranch(ctx context.Context, dir, branch string) error
	HoldsCommit(ctx context.Context, dir, sha string) (bool, error)
	ResolveBase(ctx context.Context, dir, base string) (string, error)
	CountAheadBehind(ctx context.Context, dir, ref string) (ahead, behind int, err error)
	PeekChanges(ctx context.Context, dir string) (git.Changes, error)
	Push(ctx context.Context, params git.PushParams) (string, error)
	ReadSequence(ctx context.Context, dir string) (git.Sequence, error)
	ReadRebaseOnto(ctx context.Context, dir string) (string, error)
	AbortSequence(ctx context.Context, dir string, sequence git.Sequence) error
	FetchBranchTip(ctx context.Context, dir, branch string) (string, error)
	IsAncestor(ctx context.Context, params git.IsAncestorParams) (bool, error)
	ReadMergeBase(ctx context.Context, dir, ref string) (string, error)
	ResolveCommit(ctx context.Context, dir, rev string) (string, error)
}

// NewServiceParams is what the service is built over.
type NewServiceParams struct {
	Agents          *agentstore.AgentStore
	Tasks           *taskstore.TaskStore
	TaskFinder      taskFinder
	Workflows       *workflowstore.WorkflowStore
	Events          eventRecorder
	Git             branchMover
	PullRequests    *pullrequeststore.PullRequestStore
	Forges          forgeFinder
	Locks           *TreeLocks
	Tx              txRunner
	CheckoutSync    wake.Waker
	Cascade         wake.Waker
	PullRequestSync wake.HostReader
}

// Service rebases, pushes and aborts in a task's existing worktree on a person's say-so.
type Service struct {
	agents          *agentstore.AgentStore
	tasks           *taskstore.TaskStore
	taskFinder      taskFinder
	workflows       *workflowstore.WorkflowStore
	events          eventRecorder
	git             branchMover
	pullRequests    *pullrequeststore.PullRequestStore
	forges          forgeFinder
	locks           *TreeLocks
	tx              txRunner
	checkoutSync    wake.Waker
	cascade         wake.Waker
	pullRequestSync wake.HostReader
}

// NewService builds the service from its dependencies.
func NewService(params NewServiceParams) *Service {
	locks := params.Locks
	if locks == nil {
		locks = NewTreeLocks()
	}

	return &Service{
		agents:          params.Agents,
		tasks:           params.Tasks,
		taskFinder:      params.TaskFinder,
		workflows:       params.Workflows,
		events:          params.Events,
		git:             params.Git,
		pullRequests:    params.PullRequests,
		forges:          params.Forges,
		locks:           locks,
		tx:              params.Tx,
		checkoutSync:    params.CheckoutSync,
		cascade:         params.Cascade,
		pullRequestSync: params.PullRequestSync,
	}
}

// buildParkRef names where a checkout is held before it is moved, under the space for that kind of move.
func buildParkRef(space parkSpace, taskID string) string {
	return fmt.Sprintf("refs/komrad/%s/%s/%d", space, taskID, time.Now().UnixNano())
}

// eligible answers the task and its workflow when its worktree is komrad's to act on, or the reason it is not.
func (s *Service) eligible(ctx context.Context, named oneofs.Task) (*checkoutTarget, error) {
	task, err := s.taskFinder.FindTask(ctx, named)
	if err != nil {
		return nil, err
	}

	if err := s.refuseTheCaller(ctx, *task); err != nil {
		return nil, err
	}

	if task.DeletingAt != nil {
		return nil, fmt.Errorf("%w: %q is being deleted", ErrNoWorktree, task.Slug)
	}
	if task.Checkout.Cwd == "" || task.Checkout.IsWorktreeRemoved() {
		return nil, fmt.Errorf("%w: %q has no directory on disk", ErrNoWorktree, task.Slug)
	}

	flow, err := s.workflows.Get(ctx, task.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the workflow of task %q: %w", task.Slug, err)
	}
	if !flow.Worktree.Enabled {
		return nil, fmt.Errorf("%w: its workflow does not make worktrees", ErrNoWorktree)
	}

	writer, err := readWriter(ctx, s.agents, *task)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	return &checkoutTarget{
		task:     task,
		workflow: flow,
		writer:   writer,
	}, nil
}

// readWriter is the task's live writer, answering store.ErrNotFound when it has none and refusing
// one that is still being set up.
func readWriter(ctx context.Context, agents *agentstore.AgentStore, task taskstore.Task) (*agentstore.Agent, error) {
	writer, err := agents.GetTaskWriter(ctx, task.ID)
	if err != nil {
		return nil, err
	}

	if writer.State == agentstore.StateSettingUp {
		return nil, fmt.Errorf("%w: %q is still being set up", errBusy, writer.Name)
	}

	return writer, nil
}

// recheckWriter reads the task's writer again once its checkout is locked, waiting out a launch
// claiming the task as it does, and refuses a writer that came back since the target was read or
// one that is not between turns.
func (s *Service) recheckWriter(ctx context.Context, target checkoutTarget) error {
	return s.tx.InTx(ctx, func(db store.DBTX) error {
		if _, err := taskstore.NewTaskStore(db).GetUndeletedForUpdate(ctx, target.task.ID); err != nil {
			return err
		}

		writer, err := readWriter(ctx, agentstore.NewAgentStore(db), *target.task)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if target.writer == nil {
			return fmt.Errorf("%w: %q started writing task %q", errBusy, writer.Name, target.task.Slug)
		}
		if !isAtRest(*writer) {
			return fmt.Errorf("%w: %q is not between turns", errWriterNotAtRest, writer.Name)
		}

		return nil
	})
}

// refuseTheCaller turns down an agent asking for the checkout it is itself working in, which a
// reviewer shares with its task's writer.
func (s *Service) refuseTheCaller(ctx context.Context, task taskstore.Task) error {
	from := caller.From(ctx).AgentID
	if from == "" {
		return nil
	}

	asker, err := s.agents.GetByID(ctx, from)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read the agent asking to act on task %q: %w", task.Slug, err)
	}
	if asker.Cwd == "" || !xpath.IsSameDir(asker.Cwd, task.Checkout.Cwd) {
		return nil
	}

	return fmt.Errorf("%w: %q sits in %s", errSelf, asker.Name, asker.Cwd)
}

// queueEvent queues one event about a task on its own, warning rather than failing when it cannot.
func (s *Service) queueEvent(ctx context.Context, taskID string, payload events.Payload) {
	event, err := events.NewEvent(payload)
	if err == nil {
		err = s.events.InsertManyTx(ctx, func(pgx.Tx) ([]river.JobArgs, error) {
			return []river.JobArgs{*event}, nil
		})
	}
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to queue an event for a task",
			append([]slog.Attr{slog.String(logging.TaskID, taskID), slog.String(logging.EventTopic, payload.Topic())}, logging.Err(err)...)...)
	}
}

// lockTree takes the lock on a checkout, refusing one something else is already moving.
func (s *Service) lockTree(dir string) (func(), error) {
	unlock, taken := s.locks.TryLock(dir)
	if !taken {
		return nil, fmt.Errorf("%w: %s", errBusy, dir)
	}

	return unlock, nil
}
