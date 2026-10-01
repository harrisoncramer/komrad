package checkouts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"komrad/internal/git"
	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/xptr"

	"github.com/harrisoncramer/streamer"
)

// cwdLookTimeout bounds each look at a directory.
const cwdLookTimeout = 5 * time.Second

// baseFetchTimeout bounds the fetch of a base a merge has moved.
const baseFetchTimeout = 30 * time.Second

// cwdLookWorkers is how many directories are looked at together.
const cwdLookWorkers = 8

type cwdSetter interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	SetCheckout(ctx context.Context, id string, params agentstore.SetCheckoutParams) error
	SetMovedSinceRound(ctx context.Context, id, snapshotSHA string) error
	SetDiffStat(ctx context.Context, id string, stat *agentstore.DiffStat) error
}

// headReader reads a checkout's HEAD.
type headReader interface {
	ReadHeadState(ctx context.Context, params git.ReadHeadStateParams) git.HeadState
}

type treeReader interface {
	ProbeTree(ctx context.Context, root string) (string, error)
	ReadBaseTip(ctx context.Context, dir, base string) (string, error)
	HasLanded(ctx context.Context, params git.HasLandedParams) (bool, error)
	FetchBase(ctx context.Context, dir, base string) (ref, sha string, err error)
	ResolveBase(ctx context.Context, dir, base string) (string, error)
	ReadDiffStat(ctx context.Context, params git.ReadDiffStatParams) (*git.DiffStat, error)
}

type workflowLister interface {
	List(ctx context.Context) ([]workflowstore.Workflow, error)
}

type roundLister interface {
	ListNewestRounds(ctx context.Context) ([]reviewstore.NewestRound, error)
	ListUnlandedRounds(ctx context.Context) (map[string][]reviewstore.UnlandedRound, error)
	MarkRoundsLanded(ctx context.Context, agentID string, ordinal int32) (int64, error)
}

type pullRequestObserver interface {
	Observe(ctx context.Context) (int64, error)
}

// NewCwdSyncerParams is what a cwd syncer is built over.
type NewCwdSyncerParams struct {
	Agents          cwdSetter
	Heads           headReader
	Trees           treeReader
	Rounds          roundLister
	Workflows       workflowLister
	PullRequests    pullRequestObserver
	PullRequestSync loopWaker
}

// CwdSyncer keeps what the panel shows of an agent's working directory in step with the
// directory itself.
type CwdSyncer struct {
	agents          cwdSetter
	heads           headReader
	trees           treeReader
	rounds          roundLister
	workflows       workflowLister
	pullRequests    pullRequestObserver
	pullRequestSync loopWaker
	// walkedMu guards walked and fetched.
	walkedMu sync.Mutex
	// walked is the base tip and newest unlanded round each agent's landing was last walked at,
	// so a walk that would find what the last one did is skipped.
	walked map[string]string
	// fetched is the merge each agent's base was last fetched for.
	fetched map[string]time.Time
}

// NewCwdSyncer builds a syncer over the agent store.
func NewCwdSyncer(params NewCwdSyncerParams) *CwdSyncer {
	return &CwdSyncer{
		agents:          params.Agents,
		heads:           params.Heads,
		trees:           params.Trees,
		rounds:          params.Rounds,
		workflows:       params.Workflows,
		pullRequests:    params.PullRequests,
		pullRequestSync: params.PullRequestSync,
		walked:          map[string]string{},
		fetched:         map[string]time.Time{},
	}
}

// cwdLook is what one pass found at an agent's directory.
type cwdLook struct {
	agent   agentstore.Agent
	removed bool
	head    git.HeadState
	// movedSinceRound is the newest round's snapshot while the worktree no longer hashes to it.
	movedSinceRound string
	// landed is the newest round the base holds whole, zero when it holds none.
	landed int32
	// walkedAt is the base tip and newest round the landing walk read, empty when it did not walk.
	walkedAt string
	// diffStat is the lines the checkout adds and removes since its base, nil where there is none.
	diffStat *agentstore.DiffStat
	// tree is the worktree as the probe hashed it, empty when there was no round to probe against.
	tree string
	err  error
}

// Sync looks at every live agent's cwd once and writes what changed, clearing the branch and
// repository when the cwd names no git checkout, leaving an agent with no cwd alone, and
// observing the pull requests when any agent moved onto a branch and waking their sync when
// any agent's branch was pushed.
func (cs *CwdSyncer) Sync(ctx context.Context) error {
	agents, err := cs.agents.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents for cwd sync: %w", err)
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

	looks, err := cs.lookAtAll(ctx, lookAtAllParams{
		Agents:    agents,
		Rounds:    rounds,
		Unlanded:  unlanded,
		Worktrees: worktrees,
	})
	if err != nil {
		return err
	}

	var failures []error
	isObserving, isPushed := false, false
	for _, look := range looks {
		isObserving = isObserving || look.hasMovedBranch()
		isPushed = isPushed || look.hasPushed()
		if look.err != nil {
			failures = append(failures, look.err)
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
	}

	if isObserving {
		if _, err := cs.pullRequests.Observe(ctx); err != nil {
			failures = append(failures, err)
		}
	}

	if isPushed {
		cs.pullRequestSync.Wake()
	}

	return errors.Join(failures...)
}

// listNewestRounds is the snapshot of every agent's newest round, by agent id.
func (cs *CwdSyncer) listNewestRounds(ctx context.Context) (map[string]string, error) {
	newest, err := cs.rounds.ListNewestRounds(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the newest review rounds for cwd sync: %w", err)
	}

	rounds := map[string]string{}
	for _, round := range newest {
		rounds[round.AgentID] = round.SnapshotSHA
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

type lookAtAllParams struct {
	Agents    []agentstore.Agent
	Rounds    map[string]string
	Unlanded  map[string][]reviewstore.UnlandedRound
	Worktrees map[string]workflowstore.WorktreeConfig
}

func (cs *CwdSyncer) lookAtAll(ctx context.Context, params lookAtAllParams) ([]cwdLook, error) {
	agents := params.Agents
	looker, err := streamer.NewStreamer(streamer.NewStreamerParams[agentstore.Agent, cwdLook]{
		WorkerCount: cwdLookWorkers,
		Work: func(ctx context.Context, agent agentstore.Agent) (cwdLook, error) {
			look := cs.look(ctx, agent, params.Rounds[agent.ID])
			if look.removed || look.head.Root == "" {
				return look, nil
			}

			config := params.Worktrees[agent.WorkflowID]
			landed, walkedAt, err := cs.walkLanding(ctx, walkLandingParams{
				Agent:  agent,
				Rounds: params.Unlanded[agent.ID],
				Base:   config.BaseBranch,
			})
			look.landed, look.walkedAt = landed, walkedAt
			look.err = errors.Join(look.err, err)

			cs.measure(ctx, &look, config)

			return look, nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build the cwd looker: %w", err)
	}

	input := make(chan agentstore.Agent, len(agents))
	for _, agent := range agents {
		input <- agent
	}
	close(input)

	results, workErrs, err := looker.Stream(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to look at agent directories: %w", err)
	}

	looks := make([]cwdLook, 0, len(agents))
	for look := range results {
		looks = append(looks, look)
	}
	for err := range workErrs {
		return nil, fmt.Errorf("failed to look at an agent directory: %w", err)
	}

	return looks, nil
}

func (cs *CwdSyncer) look(ctx context.Context, agent agentstore.Agent, newest string) cwdLook {
	look := cwdLook{
		agent:   agent,
		removed: isCwdRemoved(ctx, agent.Cwd),
	}
	if look.removed {
		return look
	}

	look.head = cs.readHead(ctx, agent.Cwd, newest)

	probe, err := cs.probeTree(ctx, look.head)
	if err != nil {
		look.movedSinceRound = agent.MovedSinceRound
		look.err = fmt.Errorf("failed to hash the worktree of %q: %w", agent.Name, err)

		return look
	}

	look.tree = probe
	if look.head.SnapshotTree != "" && probe != look.head.SnapshotTree {
		look.movedSinceRound = newest
	}

	return look
}

// hasMovedBranch is whether the look found the agent on a branch or repository its row does not hold.
func (cl cwdLook) hasMovedBranch() bool {
	branch := xptr.NonEmpty(cl.head.Branch)
	if branch == nil {
		return false
	}

	return !xptr.Equal(cl.agent.Branch, branch) || !xptr.Equal(cl.agent.RepositoryURL, xptr.NonEmpty(cl.head.RepositoryURL))
}

// hasPushed is whether the look found origin's copy of the branch at a commit the row does not hold.
func (cl cwdLook) hasPushed() bool {
	pushed := xptr.NonEmpty(cl.head.PushedSHA)

	return pushed != nil && !xptr.Equal(cl.agent.PushedSHA, pushed)
}

// write records what the look found, when any of it differs from the row.
func (cs *CwdSyncer) write(ctx context.Context, look cwdLook) error {
	agent, head := look.agent, look.head
	checkout := agentstore.SetCheckoutParams{
		Branch:          xptr.NonEmpty(head.Branch),
		RepositoryURL:   xptr.NonEmpty(head.RepositoryURL),
		HeadSHA:         xptr.NonEmpty(head.SHA),
		PushedSHA:       xptr.NonEmpty(head.PushedSHA),
		WorktreeRemoved: look.removed,
		Sequence:        sequenceOf(head.Sequence),
	}
	unchanged := xptr.Equal(agent.Branch, checkout.Branch) &&
		xptr.Equal(agent.RepositoryURL, checkout.RepositoryURL) &&
		xptr.Equal(agent.HeadSHA, checkout.HeadSHA) &&
		xptr.Equal(agent.PushedSHA, checkout.PushedSHA) &&
		agent.WorktreeRemoved == checkout.WorktreeRemoved &&
		agent.Sequence == checkout.Sequence
	if unchanged {
		return nil
	}

	if err := cs.agents.SetCheckout(ctx, agent.ID, checkout); err != nil {
		return fmt.Errorf("failed to set the checkout for %q: %w", agent.Name, err)
	}

	return nil
}

// writeMovedSinceRound records the newest round the worktree has moved away from, when the row
// says otherwise.
func (cs *CwdSyncer) writeMovedSinceRound(ctx context.Context, look cwdLook) error {
	agent := look.agent
	if agent.MovedSinceRound == look.movedSinceRound {
		return nil
	}

	if err := cs.agents.SetMovedSinceRound(ctx, agent.ID, look.movedSinceRound); err != nil {
		return fmt.Errorf("failed to set the round the worktree of %q moved since: %w", agent.Name, err)
	}

	return nil
}

// writeLanded marks the round the look found landed, and every earlier one, then remembers where
// the walk stood so the next pass can skip it.
func (cs *CwdSyncer) writeLanded(ctx context.Context, look cwdLook) error {
	if look.landed > 0 {
		if _, err := cs.rounds.MarkRoundsLanded(ctx, look.agent.ID, look.landed); err != nil {
			return fmt.Errorf("failed to land the rounds of %q: %w", look.agent.Name, err)
		}
	}

	if look.walkedAt != "" {
		cs.walkedMu.Lock()
		cs.walked[look.agent.ID] = look.walkedAt
		cs.walkedMu.Unlock()
	}

	return nil
}

// writeDiffStat records the lines the look measured, when the row says otherwise.
func (cs *CwdSyncer) writeDiffStat(ctx context.Context, look cwdLook) error {
	agent := look.agent
	if xptr.Equal(agent.DiffStat, look.diffStat) {
		return nil
	}

	if err := cs.agents.SetDiffStat(ctx, agent.ID, look.diffStat); err != nil {
		return fmt.Errorf("failed to set the diff stat of %q: %w", agent.Name, err)
	}

	return nil
}

type walkLandingParams struct {
	Agent  agentstore.Agent
	Rounds []reviewstore.UnlandedRound
	Base   string
}

// walkLanding finds the newest of an agent's unlanded rounds the base holds whole, skipping the
// walk while neither the base tip nor the newest round has moved since the last one.
func (cs *CwdSyncer) walkLanding(ctx context.Context, params walkLandingParams) (int32, string, error) {
	agent := params.Agent
	if len(params.Rounds) == 0 {
		return 0, "", nil
	}

	if err := cs.fetchMergedBase(ctx, params); err != nil {
		return 0, "", err
	}

	ctx, cancel := context.WithTimeout(ctx, cwdLookTimeout)
	defer cancel()

	tip, err := cs.trees.ReadBaseTip(ctx, agent.Cwd, params.Base)
	if errors.Is(err, git.ErrBaseMissing) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("failed to read the base tip of %q: %w", agent.Name, err)
	}

	walkedAt := tip + ":" + params.Rounds[0].SnapshotSHA
	cs.walkedMu.Lock()
	isWalked := cs.walked[agent.ID] == walkedAt
	cs.walkedMu.Unlock()
	if isWalked {
		return 0, "", nil
	}

	for _, round := range params.Rounds {
		landed, err := cs.trees.HasLanded(ctx, git.HasLandedParams{
			Dir:         agent.Cwd,
			Base:        params.Base,
			BaseSHA:     round.BaseSHA,
			SnapshotSHA: round.SnapshotSHA,
		})
		if err != nil {
			return 0, "", fmt.Errorf("failed to read whether round %d of %q landed: %w", round.Ordinal, agent.Name, err)
		}
		if landed {
			return round.Ordinal, walkedAt, nil
		}
	}

	return 0, walkedAt, nil
}

// fetchMergedBase fetches the base once for each merge of the agent's pull request, so the walk
// reads a base that holds what merged.
func (cs *CwdSyncer) fetchMergedBase(ctx context.Context, params walkLandingParams) error {
	merged := params.Rounds[0].MergedAt
	if merged == nil {
		return nil
	}

	cs.walkedMu.Lock()
	isFetched := cs.fetched[params.Agent.ID].Equal(*merged)
	cs.fetched[params.Agent.ID] = *merged
	cs.walkedMu.Unlock()
	if isFetched {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, baseFetchTimeout)
	defer cancel()

	_, _, err := cs.trees.FetchBase(ctx, params.Agent.Cwd, params.Base)
	if err != nil && !errors.Is(err, git.ErrBaseMissing) {
		return fmt.Errorf("failed to fetch the base of %q: %w", params.Agent.Name, err)
	}

	return nil
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

func (cs *CwdSyncer) readHead(ctx context.Context, dir, snapshotSHA string) git.HeadState {
	if dir == "" {
		return git.HeadState{}
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

	root := look.head.Root
	ref, err := cs.trees.ResolveBase(ctx, root, config.BaseBranch)
	if errors.Is(err, git.ErrBaseMissing) {
		return
	}
	if err != nil {
		look.keepDiffStat(err)

		return
	}

	stat, err := cs.trees.ReadDiffStat(ctx, git.ReadDiffStatParams{
		Dir:     root,
		Ref:     ref,
		Tree:    look.tree,
		Exclude: config.DiffExclude,
	})
	if errors.Is(err, git.ErrBaseUnrelated) {
		return
	}
	if err != nil {
		look.keepDiffStat(err)

		return
	}

	look.diffStat = &agentstore.DiffStat{Added: stat.Added, Removed: stat.Removed}
}

// keepDiffStat leaves the row's diff stat standing when git could not measure the checkout.
func (cl *cwdLook) keepDiffStat(err error) {
	cl.diffStat = cl.agent.DiffStat
	cl.err = errors.Join(cl.err, fmt.Errorf("failed to measure the checkout of %q: %w", cl.agent.Name, err))
}

// sequenceOf is a sequence git read, as the agent row stores it.
func sequenceOf(sequence git.Sequence) agentstore.Sequence {
	if !sequence.IsInProgress() {
		return agentstore.SequenceNone
	}

	return agentstore.Sequence(sequence)
}
