package checkouts

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"komrad/internal/git"
	reviewstore "komrad/internal/store/review"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	worktreestore "komrad/internal/store/worktree"
	"komrad/internal/worktrees"
	"komrad/lib/xptr"

	"github.com/harrisoncramer/streamer"
)

// cwdLookTimeout bounds each look at a directory.
const cwdLookTimeout = 5 * time.Second

// baseFetchTimeout bounds the fetch of a base a merge has moved.
const baseFetchTimeout = 30 * time.Second

// cwdLookWorkers is how many directories are looked at together.
const cwdLookWorkers = 8

const standingReadTimeout = 30 * time.Second

// headReader reads a checkout's HEAD.
type headReader interface {
	ReadHeadState(ctx context.Context, params git.ReadHeadStateParams) (*git.HeadState, error)
}

type treeReader interface {
	ProbeTree(ctx context.Context, root string) (string, error)
	ReadBaseTip(ctx context.Context, dir, base string) (string, error)
	HasLanded(ctx context.Context, params git.HasLandedParams) (bool, error)
	FetchBase(ctx context.Context, dir, base string) (ref, sha string, err error)
}

type pullRequestObserver interface {
	Observe(ctx context.Context) (int64, error)
}

// NewCwdSyncerParams is what a cwd syncer is built over.
type NewCwdSyncerParams struct {
	Tasks        *taskstore.TaskStore
	Heads        headReader
	Trees        treeReader
	Checkouts    *worktrees.CheckoutReader
	Rounds       *reviewstore.ReviewStore
	Workflows    *workflowstore.WorkflowStore
	Worktrees    *worktreestore.WorktreeStore
	PullRequests pullRequestObserver
}

// CwdSyncer keeps what the panel shows of each task's checkout in step with the directory itself.
type CwdSyncer struct {
	tasks        *taskstore.TaskStore
	heads        headReader
	trees        treeReader
	checkouts    *worktrees.CheckoutReader
	rounds       *reviewstore.ReviewStore
	workflows    *workflowstore.WorkflowStore
	worktrees    *worktreestore.WorktreeStore
	pullRequests pullRequestObserver
	// walkedMu guards walked and fetched.
	walkedMu sync.Mutex
	// walked is the base tip and newest unlanded round each agent's landing was last walked at,
	// so a walk that would find what the last one did is skipped.
	walked map[string]string
	// fetched is the merge each task's base was last fetched for.
	fetched map[string]time.Time
}

// NewCwdSyncer builds a syncer over the task store.
func NewCwdSyncer(params NewCwdSyncerParams) *CwdSyncer {
	return &CwdSyncer{
		tasks:        params.Tasks,
		heads:        params.Heads,
		trees:        params.Trees,
		checkouts:    params.Checkouts,
		rounds:       params.Rounds,
		workflows:    params.Workflows,
		worktrees:    params.Worktrees,
		pullRequests: params.PullRequests,
		walked:       map[string]string{},
		fetched:      map[string]time.Time{},
	}
}

// cwdLook is what one pass found at a task's checkout.
type cwdLook struct {
	task       taskstore.Task
	removed    bool
	headUnread bool
	head       git.HeadState
	// movedSinceRound is the newest round's snapshot while the worktree no longer hashes to it.
	movedSinceRound string
	// landed is the newest round the base holds whole, by the agent whose round it is.
	landed map[string]int32
	// walkedAt is the base tip and newest round each agent's landing walk read, by agent.
	walkedAt map[string]string
	// diffStat is the lines the checkout adds and removes since its base, nil where there is none.
	diffStat *taskstore.DiffStat
	// tree is the worktree as the probe hashed it, empty when there was no round to probe against.
	tree     string
	worktree *worktreestore.Worktree
	standing *worktreestore.Standing
	err      error
}

// Sync looks at every live task's checkout once and writes what changed, clearing the branch and
// repository when the directory names no git checkout, and observing the pull requests when any
// checkout moved onto a branch.
func (cs *CwdSyncer) Sync(ctx context.Context) error {
	tasks, err := cs.tasks.ListCheckouts(ctx)
	if err != nil {
		return fmt.Errorf("failed to list the task checkouts for cwd sync: %w", err)
	}

	rounds, err := cs.listNewestRounds(ctx)
	if err != nil {
		return err
	}

	worktrees, err := cs.listWorktreeConfigs(ctx)
	if err != nil {
		return err
	}

	unlanded, err := cs.rounds.ListUnlandedRounds(ctx)
	if err != nil {
		return fmt.Errorf("failed to list the unlanded review rounds for cwd sync: %w", err)
	}

	owned, err := cs.listOwnedWorktrees(ctx)
	if err != nil {
		return err
	}

	looks, err := cs.lookAtAll(ctx, lookAtAllParams{
		Tasks:     tasks,
		Rounds:    rounds,
		Unlanded:  unlanded,
		Worktrees: worktrees,
		Owned:     owned,
	})
	if err != nil {
		return err
	}

	var failures []error
	isObserving := false
	for _, look := range looks {
		isObserving = isObserving || look.hasMovedBranch()
		if look.err != nil {
			failures = append(failures, look.err)
		}
		if look.headUnread {
			continue
		}
		if err := cs.write(ctx, look); err != nil {
			failures = append(failures, err)
		}
		if err := cs.writeMovedSinceRound(ctx, look); err != nil {
			failures = append(failures, err)
		}
		if err := cs.writeLanded(ctx, look); err != nil {
			failures = append(failures, err)
		}
		if err := cs.writeDiffStat(ctx, look); err != nil {
			failures = append(failures, err)
		}
		if err := cs.writeStanding(ctx, look); err != nil {
			failures = append(failures, err)
		}
	}

	if isObserving {
		if _, err := cs.pullRequests.Observe(ctx); err != nil {
			failures = append(failures, err)
		}
	}

	return errors.Join(failures...)
}

// listNewestRounds is the snapshot of the newest round of every task's agents, by task id.
func (cs *CwdSyncer) listNewestRounds(ctx context.Context) (map[string]string, error) {
	newest, err := cs.rounds.ListNewestRounds(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the newest review rounds for cwd sync: %w", err)
	}

	rounds := map[string]string{}
	for _, round := range newest {
		rounds[round.TaskID] = round.SnapshotSHA
	}

	return rounds, nil
}

// listWorktreeConfigs is the worktree settings of each workflow, by workflow id.
func (cs *CwdSyncer) listWorktreeConfigs(ctx context.Context) (map[string]workflowstore.WorktreeConfig, error) {
	flows, err := cs.workflows.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the workflows for cwd sync: %w", err)
	}

	configs := map[string]workflowstore.WorktreeConfig{}
	for _, flow := range flows {
		configs[flow.ID] = flow.Worktree
	}

	return configs, nil
}

func (cs *CwdSyncer) listOwnedWorktrees(ctx context.Context) (map[string]worktreestore.Worktree, error) {
	stored, err := cs.worktrees.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the worktrees for cwd sync: %w", err)
	}

	owned := map[string]worktreestore.Worktree{}
	for _, worktree := range stored {
		owned[worktree.Dir] = worktree
	}

	return owned, nil
}

type lookAtAllParams struct {
	Tasks     []taskstore.Task
	Rounds    map[string]string
	Unlanded  map[string][]reviewstore.UnlandedRound
	Worktrees map[string]workflowstore.WorktreeConfig
	Owned     map[string]worktreestore.Worktree
}

func (cs *CwdSyncer) lookAtAll(ctx context.Context, params lookAtAllParams) ([]cwdLook, error) {
	tasks := params.Tasks
	looker, err := streamer.NewStreamer(streamer.NewStreamerParams[taskstore.Task, cwdLook]{
		WorkerCount: cwdLookWorkers,
		Work: func(ctx context.Context, task taskstore.Task) (cwdLook, error) {
			look := cs.look(ctx, task, params.Rounds[task.ID])
			if look.headUnread {
				return look, nil
			}

			config := params.Worktrees[task.WorkflowID]
			cs.readStanding(ctx, &look, readStandingParams{
				Owned: params.Owned,
				Base:  config.BaseBranch,
			})
			if look.removed || look.head.Root == "" {
				return look, nil
			}

			cs.walkLandings(ctx, &look, walkLandingsParams{
				Rounds: params.Unlanded[task.ID],
				Base:   config.BaseBranch,
			})
			cs.measure(ctx, &look, config)

			return look, nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build the cwd looker: %w", err)
	}

	input := make(chan taskstore.Task, len(tasks))
	for _, task := range tasks {
		input <- task
	}
	close(input)

	results, workErrs, err := looker.Stream(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to look at task checkouts: %w", err)
	}

	looks := make([]cwdLook, 0, len(tasks))
	for look := range results {
		looks = append(looks, look)
	}
	for err := range workErrs {
		return nil, fmt.Errorf("failed to look at a task checkout: %w", err)
	}

	return looks, nil
}

func (cs *CwdSyncer) look(ctx context.Context, task taskstore.Task, newest string) cwdLook {
	look := cwdLook{
		task:    task,
		removed: isCwdRemoved(ctx, task.Checkout.Cwd),
	}
	if look.removed {
		return look
	}

	head, err := cs.readHead(ctx, task.Checkout.Cwd, newest)
	if err != nil {
		look.headUnread = true
		look.err = fmt.Errorf("failed to read the checkout of task %q: %w", task.Slug, err)

		return look
	}
	look.head = *head

	probe, err := cs.probeTree(ctx, look.head)
	if err != nil {
		look.movedSinceRound = task.Checkout.MovedSinceRound
		look.err = fmt.Errorf("failed to hash the worktree of task %q: %w", task.Slug, err)

		return look
	}

	look.tree = probe
	if look.head.SnapshotTree != "" && probe != look.head.SnapshotTree {
		look.movedSinceRound = newest
	}

	return look
}

// hasMovedBranch is whether the look found the checkout on a branch or repository its task does not hold.
func (cl cwdLook) hasMovedBranch() bool {
	branch := xptr.NonEmpty(cl.head.Branch)
	if branch == nil {
		return false
	}

	checkout := cl.task.Checkout

	return !xptr.Equal(checkout.Branch, branch) || !xptr.Equal(checkout.RepositoryURL, xptr.NonEmpty(cl.head.RepositoryURL))
}

// write records what the look found, when any of it differs from the task.
func (cs *CwdSyncer) write(ctx context.Context, look cwdLook) error {
	stored, head := look.task.Checkout, look.head
	read := taskstore.SetHeadParams{
		Branch:          xptr.NonEmpty(head.Branch),
		RepositoryURL:   xptr.NonEmpty(head.RepositoryURL),
		HeadSHA:         xptr.NonEmpty(head.SHA),
		WorktreeRemoved: look.removed,
		Sequence:        sequenceOf(head.Sequence),
	}
	unchanged := xptr.Equal(stored.Branch, read.Branch) &&
		xptr.Equal(stored.RepositoryURL, read.RepositoryURL) &&
		xptr.Equal(stored.HeadSHA, read.HeadSHA) &&
		stored.WorktreeRemoved == read.WorktreeRemoved &&
		stored.Sequence == read.Sequence
	if unchanged {
		return nil
	}

	if err := cs.tasks.SetHead(ctx, look.task.ID, read); err != nil {
		return fmt.Errorf("failed to set the head of task %q: %w", look.task.Slug, err)
	}

	return nil
}

// writeMovedSinceRound records the newest round the worktree has moved away from, when the task
// says otherwise.
func (cs *CwdSyncer) writeMovedSinceRound(ctx context.Context, look cwdLook) error {
	if look.task.Checkout.MovedSinceRound == look.movedSinceRound {
		return nil
	}

	if err := cs.tasks.SetMovedSinceRound(ctx, look.task.ID, look.movedSinceRound); err != nil {
		return fmt.Errorf("failed to set the round the checkout of task %q moved since: %w", look.task.Slug, err)
	}

	return nil
}

// writeLanded marks the rounds the look found landed, and every earlier one of the same agent,
// then remembers where each walk stood so the next pass can skip it.
func (cs *CwdSyncer) writeLanded(ctx context.Context, look cwdLook) error {
	for agentID, ordinal := range look.landed {
		if _, err := cs.rounds.MarkRoundsLanded(ctx, agentID, ordinal); err != nil {
			return fmt.Errorf("failed to land the rounds of agent %q: %w", agentID, err)
		}
	}

	cs.walkedMu.Lock()
	maps.Copy(cs.walked, look.walkedAt)
	cs.walkedMu.Unlock()

	return nil
}

// writeDiffStat records the lines the look measured, when the task says otherwise.
func (cs *CwdSyncer) writeDiffStat(ctx context.Context, look cwdLook) error {
	if xptr.Equal(look.task.Checkout.DiffStat, look.diffStat) {
		return nil
	}

	if err := cs.tasks.SetDiffStat(ctx, look.task.ID, look.diffStat); err != nil {
		return fmt.Errorf("failed to set the diff stat of task %q: %w", look.task.Slug, err)
	}

	return nil
}

func (cs *CwdSyncer) writeStanding(ctx context.Context, look cwdLook) error {
	if look.standing == nil || isStandingUnchanged(look.worktree.Standing, *look.standing) {
		return nil
	}

	if err := cs.worktrees.RecordStanding(ctx, *look.standing); err != nil {
		return fmt.Errorf("failed to record where the checkout of task %q stands: %w", look.task.Slug, err)
	}

	return nil
}

type walkLandingsParams struct {
	Rounds []reviewstore.UnlandedRound
	Base   string
}

// walkLandings walks the unlanded rounds of each agent on the task, recording on the look the
// newest of each the base holds whole.
func (cs *CwdSyncer) walkLandings(ctx context.Context, look *cwdLook, params walkLandingsParams) {
	if len(params.Rounds) == 0 {
		return
	}

	if err := cs.fetchMergedBase(ctx, look.task, params); err != nil {
		look.err = errors.Join(look.err, err)

		return
	}

	look.landed, look.walkedAt = map[string]int32{}, map[string]string{}
	for _, rounds := range groupRoundsByAgent(params.Rounds) {
		landed, walkedAt, err := cs.walkLanding(ctx, walkLandingParams{
			Dir:    look.task.Checkout.Cwd,
			Rounds: rounds,
			Base:   params.Base,
		})
		if err != nil {
			look.err = errors.Join(look.err, err)

			continue
		}
		if landed > 0 {
			look.landed[rounds[0].AgentID] = landed
		}
		if walkedAt != "" {
			look.walkedAt[rounds[0].AgentID] = walkedAt
		}
	}
}

type walkLandingParams struct {
	Dir    string
	Rounds []reviewstore.UnlandedRound
	Base   string
}

// walkLanding finds the newest of one agent's unlanded rounds the base holds whole, skipping the
// walk while neither the base tip nor the newest round has moved since the last one.
func (cs *CwdSyncer) walkLanding(ctx context.Context, params walkLandingParams) (int32, string, error) {
	agentID := params.Rounds[0].AgentID

	ctx, cancel := context.WithTimeout(ctx, cwdLookTimeout)
	defer cancel()

	tip, err := cs.trees.ReadBaseTip(ctx, params.Dir, params.Base)
	if errors.Is(err, git.ErrBaseMissing) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("failed to read the base tip of %q: %w", params.Dir, err)
	}

	walkedAt := tip + ":" + params.Rounds[0].SnapshotSHA
	cs.walkedMu.Lock()
	isWalked := cs.walked[agentID] == walkedAt
	cs.walkedMu.Unlock()
	if isWalked {
		return 0, "", nil
	}

	for _, round := range params.Rounds {
		landed, err := cs.trees.HasLanded(ctx, git.HasLandedParams{
			Dir:         params.Dir,
			Base:        params.Base,
			BaseSHA:     round.BaseSHA,
			SnapshotSHA: round.SnapshotSHA,
		})
		if err != nil {
			return 0, "", fmt.Errorf("failed to read whether round %d of agent %q landed: %w", round.Ordinal, agentID, err)
		}
		if landed {
			return round.Ordinal, walkedAt, nil
		}
	}

	return 0, walkedAt, nil
}

// fetchMergedBase fetches the base once for each merge of the task's pull request, so the walk
// reads a base that holds what merged.
func (cs *CwdSyncer) fetchMergedBase(ctx context.Context, task taskstore.Task, params walkLandingsParams) error {
	merged := params.Rounds[0].MergedAt
	if merged == nil {
		return nil
	}

	cs.walkedMu.Lock()
	isFetched := cs.fetched[task.ID].Equal(*merged)
	cs.walkedMu.Unlock()
	if isFetched {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, baseFetchTimeout)
	defer cancel()

	_, _, err := cs.trees.FetchBase(ctx, task.Checkout.Cwd, params.Base)
	if err != nil && !errors.Is(err, git.ErrBaseMissing) {
		return fmt.Errorf("failed to fetch the base of task %q: %w", task.Slug, err)
	}

	cs.walkedMu.Lock()
	cs.fetched[task.ID] = *merged
	cs.walkedMu.Unlock()

	return nil
}

// groupRoundsByAgent splits a task's unlanded rounds into each agent's, newest first as they came.
func groupRoundsByAgent(rounds []reviewstore.UnlandedRound) [][]reviewstore.UnlandedRound {
	var groups [][]reviewstore.UnlandedRound
	index := map[string]int{}
	for _, round := range rounds {
		at, seen := index[round.AgentID]
		if !seen {
			at = len(groups)
			index[round.AgentID] = at
			groups = append(groups, nil)
		}
		groups[at] = append(groups[at], round)
	}

	return groups
}

func isCwdRemoved(ctx context.Context, dir string) bool {
	if dir == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(ctx, cwdLookTimeout)
	defer cancel()

	statErr := make(chan error, 1)
	go func() {
		_, err := os.Stat(dir)
		statErr <- err
	}()

	select {
	case err := <-statErr:
		return errors.Is(err, os.ErrNotExist)
	case <-ctx.Done():
		return false
	}
}

func (cs *CwdSyncer) readHead(ctx context.Context, dir, snapshotSHA string) (*git.HeadState, error) {
	if dir == "" {
		return &git.HeadState{}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, cwdLookTimeout)
	defer cancel()

	return cs.heads.ReadHeadState(ctx, git.ReadHeadStateParams{Dir: dir, SnapshotSHA: snapshotSHA})
}

// probeTree hashes the worktree, and not at all when there is no round to measure it against.
func (cs *CwdSyncer) probeTree(ctx context.Context, head git.HeadState) (string, error) {
	if head.Root == "" || head.SnapshotTree == "" {
		return "", nil
	}

	ctx, cancel := context.WithTimeout(ctx, cwdLookTimeout)
	defer cancel()

	return cs.trees.ProbeTree(ctx, head.Root)
}

// measure records on the look the lines the checkout adds and removes since it left its base.
func (cs *CwdSyncer) measure(ctx context.Context, look *cwdLook, config workflowstore.WorktreeConfig) {
	ctx, cancel := context.WithTimeout(ctx, cwdLookTimeout)
	defer cancel()

	stat, err := cs.checkouts.MeasureDiff(ctx, worktrees.MeasureDiffParams{
		Dir:     look.head.Root,
		Base:    config.BaseBranch,
		Tree:    look.tree,
		Exclude: config.DiffExclude,
	})
	if errors.Is(err, git.ErrBaseMissing) || errors.Is(err, git.ErrBaseUnrelated) {
		return
	}
	if err != nil {
		look.keepDiffStat(err)

		return
	}

	look.diffStat = &taskstore.DiffStat{Added: stat.Added, Removed: stat.Removed}
}

type readStandingParams struct {
	Owned map[string]worktreestore.Worktree
	Base  string
}

func (cs *CwdSyncer) readStanding(ctx context.Context, look *cwdLook, params readStandingParams) {
	worktree, ok := params.Owned[filepath.Clean(look.task.Checkout.Cwd)]
	if !ok || look.removed {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, standingReadTimeout)
	defer cancel()

	standing := cs.checkouts.ReadStanding(ctx, worktrees.ReadStandingParams{
		Dir:      worktree.Dir,
		Readable: worktree.Readable,
		Base:     params.Base,
		HeadSHA:  look.head.SHA,
	})
	look.worktree = &worktree
	look.standing = &standing
}

// keepDiffStat leaves the row's diff stat standing when git could not measure the checkout.
func (cl *cwdLook) keepDiffStat(err error) {
	cl.diffStat = cl.task.Checkout.DiffStat
	cl.err = errors.Join(cl.err, fmt.Errorf("failed to measure the checkout of task %q: %w", cl.task.Slug, err))
}

// sequenceOf is a sequence git read, as the task stores it.
func sequenceOf(sequence git.Sequence) taskstore.Sequence {
	if !sequence.IsInProgress() {
		return taskstore.SequenceNone
	}

	return taskstore.Sequence(sequence)
}

func isStandingUnchanged(stored *worktreestore.Standing, read worktreestore.Standing) bool {
	return stored != nil &&
		stored.Dirty == read.Dirty &&
		stored.Sequencing == read.Sequencing &&
		stored.Unlanded == read.Unlanded &&
		stored.Error == read.Error
}
