package checkouts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// cwdLookWorkers is how many directories komrad looks at together.
const cwdLookWorkers = 8

const standingReadTimeout = 30 * time.Second

// headReader reads a checkout's HEAD.
type headReader interface {
	ReadHeadState(ctx context.Context, params git.ReadHeadStateParams) (*git.HeadState, error)
}

type treeReader interface {
	ProbeTree(ctx context.Context, root string) (string, error)
	ReadFingerprint(ctx context.Context, params git.ReadFingerprintParams) (*git.Fingerprint, error)
	ReadBase(ctx context.Context, dir, base string) (*git.Base, error)
	ReadDiffStat(ctx context.Context, params git.ReadDiffStatParams) (*git.DiffStat, error)
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
	// walked is the base tip and newest unlanded round each task's landing was last walked at,
	// so komrad skips a walk that would find what the last one did.
	walked map[string]string
	// fetched is the merge each task's base was last fetched for.
	fetched map[string]time.Time
	// seenMu guards seen.
	seenMu sync.Mutex
	// seen is what each task's worktree last fingerprinted as and what was read of it then.
	seen map[string]seenWorktree
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
		seen:         map[string]seenWorktree{},
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
	// landed is the newest round the base holds whole, zero when none does.
	landed int32
	// walkedAt is the base tip and newest round the landing walk read, empty when it was skipped.
	walkedAt string
	// diffStat is the lines the checkout adds and removes since its base, nil where there is none.
	diffStat *taskstore.DiffStat
	// tree is the worktree as the probe hashed it, empty when there was no round to probe against.
	tree string
	// base is the ref and tip the checkout is measured against, nil when baseErr says why it
	// could not be read.
	base    *git.Base
	baseErr error
	// fingerprint is the worktree as git status saw it this pass, nil when it could not be read.
	fingerprint *git.Fingerprint
	// seen is what the last pass read of the worktree.
	seen seenWorktree
	// measureKey is the base tip and exclusions diffStat was measured against, empty when it was
	// not measured.
	measureKey string
	worktree   *worktreestore.Worktree
	standing   *worktreestore.Standing
	err        error
}

// seenWorktree is what one pass read of a task's worktree, reused by the next while the head and
// fingerprint hold.
type seenWorktree struct {
	head        string
	fingerprint git.Fingerprint
	// tree is the probe's hash, empty when there was no round to probe against.
	tree       string
	measureKey string
	diffStat   *taskstore.DiffStat
}

// isUnchanged is whether the worktree fingerprints as it did on the last pass.
func (cl *cwdLook) isUnchanged() bool {
	return cl.fingerprint != nil && cl.seen.head == cl.head.SHA && cl.seen.fingerprint.Key == cl.fingerprint.Key
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
			cs.readBase(ctx, &look, readBaseParams{Owned: params.Owned, Base: config.BaseBranch})
			cs.readStanding(ctx, &look, params.Owned)
			if look.removed || look.head.Root == "" {
				return look, nil
			}

			cs.walkLandings(ctx, &look, walkLandingsParams{
				Rounds: params.Unlanded[task.ID],
				Base:   config.BaseBranch,
			})
			cs.measure(ctx, &look, config)
			cs.remember(look)

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
	cs.readFingerprint(ctx, &look)

	probe, err := cs.probeTree(ctx, &look)
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

// writeLanded marks the round the look found landed and every earlier one of the task, then
// remembers where the walk stood so the next pass can skip it.
func (cs *CwdSyncer) writeLanded(ctx context.Context, look cwdLook) error {
	if look.landed > 0 {
		if _, err := cs.rounds.MarkRoundsLanded(ctx, look.task.ID, look.landed); err != nil {
			return fmt.Errorf("failed to land the rounds of task %q: %w", look.task.Slug, err)
		}
	}

	if look.walkedAt != "" {
		cs.walkedMu.Lock()
		cs.walked[look.task.ID] = look.walkedAt
		cs.walkedMu.Unlock()
	}

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

// walkLandings walks the task's unlanded rounds, recording on the look the newest the base holds
// whole.
func (cs *CwdSyncer) walkLandings(ctx context.Context, look *cwdLook, params walkLandingsParams) {
	if len(params.Rounds) == 0 || errors.Is(look.baseErr, git.ErrBaseMissing) {
		return
	}
	if look.baseErr != nil {
		look.err = errors.Join(look.err, fmt.Errorf("failed to read the base tip of %q: %w", look.task.Checkout.Cwd, look.baseErr))

		return
	}

	tip, err := cs.fetchMergedBase(ctx, look.task, params)
	if err != nil {
		look.err = errors.Join(look.err, err)

		return
	}
	if tip == "" {
		tip = look.base.Tip
	}

	landed, walkedAt, err := cs.walkLanding(ctx, walkLandingParams{
		TaskID: look.task.ID,
		Dir:    look.task.Checkout.Cwd,
		Rounds: params.Rounds,
		Base:   params.Base,
		Tip:    tip,
	})
	if err != nil {
		look.err = errors.Join(look.err, err)

		return
	}

	look.landed, look.walkedAt = landed, walkedAt
}

type walkLandingParams struct {
	TaskID string
	Dir    string
	Rounds []reviewstore.UnlandedRound
	Base   string
	// Tip is the commit the base points at.
	Tip string
}

// walkLanding finds the newest of a task's unlanded rounds the base holds whole, skipping the
// walk while neither the base tip nor the newest round has moved since the last one.
func (cs *CwdSyncer) walkLanding(ctx context.Context, params walkLandingParams) (int32, string, error) {
	ctx, cancel := context.WithTimeout(ctx, cwdLookTimeout)
	defer cancel()

	walkedAt := params.Tip + ":" + params.Rounds[0].SnapshotSHA
	cs.walkedMu.Lock()
	isWalked := cs.walked[params.TaskID] == walkedAt
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
			return 0, "", fmt.Errorf("failed to read whether round %d of task %q landed: %w", round.Ordinal, params.TaskID, err)
		}
		if landed {
			return round.Ordinal, walkedAt, nil
		}
	}

	return 0, walkedAt, nil
}

// fetchMergedBase fetches the base once for each merge of the task's pull request, so the walk
// reads a base that holds what merged, and answers the tip it fetched, empty when it fetched none.
func (cs *CwdSyncer) fetchMergedBase(ctx context.Context, task taskstore.Task, params walkLandingsParams) (string, error) {
	merged := params.Rounds[0].MergedAt
	if merged == nil {
		return "", nil
	}

	cs.walkedMu.Lock()
	isFetched := cs.fetched[task.ID].Equal(*merged)
	cs.walkedMu.Unlock()
	if isFetched {
		return "", nil
	}

	ctx, cancel := context.WithTimeout(ctx, baseFetchTimeout)
	defer cancel()

	_, tip, err := cs.trees.FetchBase(ctx, task.Checkout.Cwd, params.Base)
	if err != nil && !errors.Is(err, git.ErrBaseMissing) {
		return "", fmt.Errorf("failed to fetch the base of task %q: %w", task.Slug, err)
	}

	cs.walkedMu.Lock()
	cs.fetched[task.ID] = *merged
	cs.walkedMu.Unlock()

	return tip, nil
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

// readFingerprint records on the look what git status sees of the worktree and what the last
// pass read of it.
func (cs *CwdSyncer) readFingerprint(ctx context.Context, look *cwdLook) {
	if look.head.Root == "" {
		return
	}

	cs.seenMu.Lock()
	seen, ok := cs.seen[look.task.ID]
	cs.seenMu.Unlock()

	params := git.ReadFingerprintParams{Root: look.head.Root, GitDir: look.head.GitDir}
	if ok {
		look.seen = seen
		params.Hidden = &seen.fingerprint.Hidden
	}

	ctx, cancel := context.WithTimeout(ctx, cwdLookTimeout)
	defer cancel()

	fingerprint, err := cs.trees.ReadFingerprint(ctx, params)
	if err != nil {
		return
	}
	look.fingerprint = fingerprint
}

// probeTree hashes the worktree, reusing the last hash while the worktree is unchanged, and not
// at all when there is no round to measure it against.
func (cs *CwdSyncer) probeTree(ctx context.Context, look *cwdLook) (string, error) {
	if look.head.Root == "" || look.head.SnapshotTree == "" {
		return "", nil
	}
	if look.isUnchanged() && look.seen.tree != "" {
		return look.seen.tree, nil
	}

	ctx, cancel := context.WithTimeout(ctx, cwdLookTimeout)
	defer cancel()

	return cs.trees.ProbeTree(ctx, look.head.Root)
}

// remember keeps what the pass read of the worktree for the next pass to reuse.
func (cs *CwdSyncer) remember(look cwdLook) {
	if look.fingerprint == nil {
		return
	}

	cs.seenMu.Lock()
	defer cs.seenMu.Unlock()
	cs.seen[look.task.ID] = seenWorktree{
		head:        look.head.SHA,
		fingerprint: *look.fingerprint,
		tree:        look.tree,
		measureKey:  look.measureKey,
		diffStat:    look.diffStat,
	}
}

// measure records on the look the lines the checkout adds and removes since it left its base.
func (cs *CwdSyncer) measure(ctx context.Context, look *cwdLook, config workflowstore.WorktreeConfig) {
	if errors.Is(look.baseErr, git.ErrBaseMissing) {
		return
	}
	if look.baseErr != nil {
		look.keepDiffStat(look.baseErr)

		return
	}

	measureKey := strings.Join(append([]string{look.base.Tip}, config.DiffExclude...), "\x00")
	if look.isUnchanged() && look.seen.measureKey == measureKey {
		look.diffStat = look.seen.diffStat
		look.measureKey = measureKey

		return
	}

	ctx, cancel := context.WithTimeout(ctx, cwdLookTimeout)
	defer cancel()

	stat, err := cs.trees.ReadDiffStat(ctx, git.ReadDiffStatParams{
		Dir:     look.head.Root,
		Ref:     look.base.Ref,
		Tree:    look.tree,
		Exclude: config.DiffExclude,
	})
	if errors.Is(err, git.ErrBaseUnrelated) {
		look.measureKey = measureKey

		return
	}
	if err != nil {
		look.keepDiffStat(err)

		return
	}

	look.diffStat = &taskstore.DiffStat{Added: stat.Added, Removed: stat.Removed}
	look.measureKey = measureKey
}

type readBaseParams struct {
	Owned map[string]worktreestore.Worktree
	Base  string
}

// readBase records on the look the ref and tip its checkout is measured against.
func (cs *CwdSyncer) readBase(ctx context.Context, look *cwdLook, params readBaseParams) {
	_, owned := params.Owned[filepath.Clean(look.task.Checkout.Cwd)]
	if look.removed || (look.head.Root == "" && !owned) {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, cwdLookTimeout)
	defer cancel()

	look.base, look.baseErr = cs.trees.ReadBase(ctx, look.task.Checkout.Cwd, params.Base)
}

func (cs *CwdSyncer) readStanding(ctx context.Context, look *cwdLook, owned map[string]worktreestore.Worktree) {
	worktree, ok := owned[filepath.Clean(look.task.Checkout.Cwd)]
	if !ok || look.removed {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, standingReadTimeout)
	defer cancel()

	standing := cs.checkouts.ReadStanding(ctx, worktrees.ReadStandingParams{
		Dir:      worktree.Dir,
		Readable: worktree.Readable,
		Base:     look.base,
		BaseErr:  look.baseErr,
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
