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
	SetTreeStanding(ctx context.Context, id string, standing git.Standing, snapshotSHA string) error
	SetMovedSinceRound(ctx context.Context, id, snapshotSHA string) error
}

// headReader reads a checkout's HEAD.
type headReader interface {
	ReadHeadState(ctx context.Context, params git.ReadHeadStateParams) git.HeadState
}

type treeReader interface {
	ProbeTree(ctx context.Context, root string) (string, error)
	ReadStanding(ctx context.Context, params git.ReadStandingParams) (git.Standing, error)
	ReadBaseTip(ctx context.Context, dir, base string) (string, error)
	HasLanded(ctx context.Context, params git.HasLandedParams) (bool, error)
	FetchBase(ctx context.Context, dir, base string) (ref, sha string, err error)
}

type workflowLister interface {
	List(ctx context.Context) ([]workflowstore.Workflow, error)
}

type roundLister interface {
	ListTallies(ctx context.Context) ([]reviewstore.Tally, error)
	ListNewestRounds(ctx context.Context) ([]reviewstore.NewestRound, error)
	ListUnlandedRounds(ctx context.Context) (map[string][]reviewstore.UnlandedRound, error)
	MarkRoundsLanded(ctx context.Context, agentID string, ordinal int32) (int64, error)
}

type pullRequestObserver interface {
	Observe(ctx context.Context) (int64, error)
}

// NewCwdSyncerParams is what a cwd syncer is built over.
type NewCwdSyncerParams struct {
	Agents       cwdSetter
	Heads        headReader
	Trees        treeReader
	Rounds       roundLister
	Bases        workflowLister
	PullRequests pullRequestObserver
}

// CwdSyncer keeps what the panel shows of an agent's working directory in step with the
// directory itself.
type CwdSyncer struct {
	agents       cwdSetter
	heads        headReader
	trees        treeReader
	rounds       roundLister
	bases        workflowLister
	pullRequests pullRequestObserver
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
		agents:       params.Agents,
		heads:        params.Heads,
		trees:        params.Trees,
		rounds:       params.Rounds,
		bases:        params.Bases,
		pullRequests: params.PullRequests,
		walked:       map[string]string{},
		fetched:      map[string]time.Time{},
	}
}

// cwdLook is what one pass found at an agent's directory.
type cwdLook struct {
	agent   agentstore.Agent
	removed bool
	head    git.HeadState
	// round is the snapshot the worktree was measured against, empty when no verdict names one.
	round    string
	standing git.Standing
	// movedSinceRound is the newest round's snapshot while the worktree no longer hashes to it.
	movedSinceRound string
	// landed is the newest round the base holds whole, zero when it holds none.
	landed int32
	// walkedAt is the base tip and newest round the landing walk read, empty when it did not walk.
	walkedAt string
	err      error
}

// Sync looks at every live agent's cwd once and writes what changed, clearing the branch and
// repository when the cwd names no git checkout, leaving an agent with no cwd alone, and
// observing the pull requests when any agent moved onto a branch.
func (cs *CwdSyncer) Sync(ctx context.Context) error {
	agents, err := cs.agents.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents for cwd sync: %w", err)
	}

	rounds, err := cs.listRounds(ctx)
	if err != nil {
		return err
	}

	bases, err := cs.listBases(ctx)
	if err != nil {
		return err
	}

	unlanded, err := cs.rounds.ListUnlandedRounds(ctx)
	if err != nil {
		return fmt.Errorf("failed to list the unlanded review rounds for cwd sync: %w", err)
	}

	looks, err := cs.lookAtAll(ctx, lookAtAllParams{
		Agents:   agents,
		Rounds:   rounds,
		Unlanded: unlanded,
		Bases:    bases,
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
		if err := cs.write(ctx, look); err != nil {
			failures = append(failures, err)
		}
		if err := cs.writeStanding(ctx, look); err != nil {
			failures = append(failures, err)
		}
		if err := cs.writeMovedSinceRound(ctx, look); err != nil {
			failures = append(failures, err)
		}
		if err := cs.writeLanded(ctx, look); err != nil {
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

// agentRounds are the snapshots one agent's worktree is measured against.
type agentRounds struct {
	// concluded is the round its last verdict was given on.
	concluded string
	newest    string
}

// listRounds is every agent's rounds, by agent id.
func (cs *CwdSyncer) listRounds(ctx context.Context) (map[string]agentRounds, error) {
	tallies, err := cs.rounds.ListTallies(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the review tallies for cwd sync: %w", err)
	}

	newest, err := cs.rounds.ListNewestRounds(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the newest review rounds for cwd sync: %w", err)
	}

	rounds := map[string]agentRounds{}
	for _, round := range newest {
		rounds[round.AgentID] = agentRounds{newest: round.SnapshotSHA}
	}
	for _, tally := range tallies {
		if tally.Verdict != nil {
			agent := rounds[tally.AgentID]
			agent.concluded = tally.SnapshotSHA
			rounds[tally.AgentID] = agent
		}
	}

	return rounds, nil
}

// listBases is the branch each workflow cuts its worktrees from, by workflow id.
func (cs *CwdSyncer) listBases(ctx context.Context) (map[string]string, error) {
	flows, err := cs.bases.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the workflows for cwd sync: %w", err)
	}

	bases := map[string]string{}
	for _, flow := range flows {
		bases[flow.ID] = flow.Worktree.BaseBranch
	}

	return bases, nil
}

type lookAtAllParams struct {
	Agents   []agentstore.Agent
	Rounds   map[string]agentRounds
	Unlanded map[string][]reviewstore.UnlandedRound
	Bases    map[string]string
}

func (cs *CwdSyncer) lookAtAll(ctx context.Context, params lookAtAllParams) ([]cwdLook, error) {
	agents := params.Agents
	looker, err := streamer.NewStreamer(streamer.NewStreamerParams[agentstore.Agent, cwdLook]{
		WorkerCount: cwdLookWorkers,
		Work: func(ctx context.Context, agent agentstore.Agent) (cwdLook, error) {
			look := cs.look(ctx, agent, params.Rounds[agent.ID], params.Bases[agent.WorkflowID])
			if look.removed || look.head.Root == "" {
				return look, nil
			}

			landed, walkedAt, err := cs.walkLanding(ctx, walkLandingParams{
				Agent:  agent,
				Rounds: params.Unlanded[agent.ID],
				Base:   params.Bases[agent.WorkflowID],
			})
			look.landed, look.walkedAt = landed, walkedAt
			look.err = errors.Join(look.err, err)

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

func (cs *CwdSyncer) look(ctx context.Context, agent agentstore.Agent, rounds agentRounds, base string) cwdLook {
	look := cwdLook{
		agent:   agent,
		round:   rounds.concluded,
		removed: isCwdRemoved(ctx, agent.Cwd),
	}
	if look.removed {
		return look
	}

	look.head = cs.readHead(ctx, agent.Cwd, rounds.newest)

	probe, err := cs.probeTree(ctx, look.head, rounds.concluded)
	if err != nil {
		look.movedSinceRound = agent.MovedSinceRound
		look.err = fmt.Errorf("failed to hash the worktree of %q: %w", agent.Name, err)

		return look
	}

	if look.head.SnapshotTree != "" && probe != look.head.SnapshotTree {
		look.movedSinceRound = rounds.newest
	}

	look.standing, look.err = cs.readStanding(ctx, readStandingParams{
		Agent: agent,
		Round: rounds.concluded,
		Base:  base,
		Probe: probe,
	})

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

// write records what the look found, when any of it differs from the row.
func (cs *CwdSyncer) write(ctx context.Context, look cwdLook) error {
	agent, head := look.agent, look.head
	checkout := agentstore.SetCheckoutParams{
		Branch:             xptr.NonEmpty(head.Branch),
		RepositoryURL:      xptr.NonEmpty(head.RepositoryURL),
		HeadSHA:            xptr.NonEmpty(head.SHA),
		WorktreeRemoved:    look.removed,
		SequenceInProgress: head.SequenceInProgress,
	}
	unchanged := xptr.Equal(agent.Branch, checkout.Branch) &&
		xptr.Equal(agent.RepositoryURL, checkout.RepositoryURL) &&
		xptr.Equal(agent.HeadSHA, checkout.HeadSHA) &&
		agent.WorktreeRemoved == checkout.WorktreeRemoved &&
		agent.SequenceInProgress == checkout.SequenceInProgress
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

// writeStanding records where the look found the worktree against its round, when the row says
// otherwise.
func (cs *CwdSyncer) writeStanding(ctx context.Context, look cwdLook) error {
	agent := look.agent
	if agent.TreeStanding == look.standing && agent.TreeStandingSHA == look.round {
		return nil
	}

	if err := cs.agents.SetTreeStanding(ctx, agent.ID, look.standing, look.round); err != nil {
		return fmt.Errorf("failed to set the tree standing for %q: %w", agent.Name, err)
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

// probeTree hashes the worktree once for both rounds, and not at all when neither needs it.
func (cs *CwdSyncer) probeTree(ctx context.Context, head git.HeadState, concluded string) (string, error) {
	if head.Root == "" || (head.SnapshotTree == "" && concluded == "") {
		return "", nil
	}

	ctx, cancel := context.WithTimeout(ctx, cwdLookTimeout)
	defer cancel()

	return cs.trees.ProbeTree(ctx, head.Root)
}

type readStandingParams struct {
	Agent agentstore.Agent
	Round string
	Base  string
	Probe string
}

func (cs *CwdSyncer) readStanding(ctx context.Context, params readStandingParams) (git.Standing, error) {
	agent := params.Agent
	if agent.Cwd == "" || params.Round == "" {
		return git.StandingSame, nil
	}

	ctx, cancel := context.WithTimeout(ctx, cwdLookTimeout)
	defer cancel()

	standing, err := cs.trees.ReadStanding(ctx, git.ReadStandingParams{
		Dir:         agent.Cwd,
		Base:        params.Base,
		SnapshotSHA: params.Round,
		Probe:       params.Probe,
	})
	if err != nil {
		return standing, fmt.Errorf("failed to compare the tree of %q against its reviewed round: %w", agent.Name, err)
	}

	return standing, nil
}
