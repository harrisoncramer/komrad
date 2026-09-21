package checkouts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"weave/internal/git"
	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xptr"

	"github.com/harrisoncramer/streamer"
)

// cwdLookTimeout bounds each look at a directory.
const cwdLookTimeout = 5 * time.Second

// cwdLookWorkers is how many directories are looked at together.
const cwdLookWorkers = 8

type cwdSetter interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	SetCheckout(ctx context.Context, id string, params agentstore.SetCheckoutParams) error
}

// headReader reads a checkout's HEAD.
type headReader interface {
	ReadHeadState(ctx context.Context, dir string) git.HeadState
}

type standingReader interface {
	ReadStanding(ctx context.Context, params git.ReadStandingParams) (git.Standing, error)
}

type workflowLister interface {
	List(ctx context.Context) ([]workflowstore.Workflow, error)
}

type tallyLister interface {
	ListTallies(ctx context.Context) ([]reviewstore.Tally, error)
}

// CwdSyncer keeps what the panel shows of an agent's working directory in step with the
// directory itself.
type CwdSyncer struct {
	agents    cwdSetter
	heads     headReader
	trees     standingReader
	tallies   tallyLister
	bases     workflowLister
	mu        sync.Mutex
	standings map[string]roundStanding
}

// roundStanding is where one pass found an agent's worktree against the round it compared it to.
type roundStanding struct {
	snapshotSHA string
	standing    git.Standing
}

// NewCwdSyncerParams is what a cwd syncer is built over.
type NewCwdSyncerParams struct {
	Agents  cwdSetter
	Heads   headReader
	Trees   standingReader
	Tallies tallyLister
	Bases   workflowLister
}

// NewCwdSyncer builds a syncer over the agent store.
func NewCwdSyncer(params NewCwdSyncerParams) *CwdSyncer {
	return &CwdSyncer{
		agents:    params.Agents,
		heads:     params.Heads,
		trees:     params.Trees,
		tallies:   params.Tallies,
		bases:     params.Bases,
		standings: map[string]roundStanding{},
	}
}

// cwdLook is what one pass found at an agent's directory.
type cwdLook struct {
	agent    agentstore.Agent
	removed  bool
	head     git.HeadState
	standing git.Standing
	err      error
}

// GetStanding is where the last pass found an agent's worktree against the given round.
func (cs *CwdSyncer) GetStanding(agentID, snapshotSHA string) git.Standing {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if found, ok := cs.standings[agentID]; ok && snapshotSHA != "" && found.snapshotSHA == snapshotSHA {
		return found.standing
	}

	return git.StandingSame
}

// Sync looks at every live agent's cwd once and writes what changed, clearing the branch and
// repository when the cwd names no git checkout and leaving an agent with no cwd alone.
func (cs *CwdSyncer) Sync(ctx context.Context) error {
	agents, err := cs.agents.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents for cwd sync: %w", err)
	}

	tallied, err := cs.tallies.ListTallies(ctx)
	if err != nil {
		return fmt.Errorf("failed to list the review tallies for cwd sync: %w", err)
	}
	rounds := map[string]string{}
	for _, tally := range tallied {
		if tally.Verdict != nil {
			rounds[tally.AgentID] = tally.SnapshotSHA
		}
	}

	bases, err := cs.listBases(ctx)
	if err != nil {
		return err
	}

	looks, err := cs.lookAtAll(ctx, agents, rounds, bases)
	if err != nil {
		return err
	}

	standings := map[string]roundStanding{}
	var failures []error
	for _, look := range looks {
		if round := rounds[look.agent.ID]; round != "" {
			standings[look.agent.ID] = roundStanding{snapshotSHA: round, standing: look.standing}
		}
		if look.err != nil {
			failures = append(failures, look.err)
		}
		if err := cs.write(ctx, look); err != nil {
			failures = append(failures, err)
		}
	}

	cs.mu.Lock()
	cs.standings = standings
	cs.mu.Unlock()

	return errors.Join(failures...)
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

func (cs *CwdSyncer) lookAtAll(ctx context.Context, agents []agentstore.Agent, rounds, bases map[string]string) ([]cwdLook, error) {
	looker, err := streamer.NewStreamer(streamer.NewStreamerParams[agentstore.Agent, cwdLook]{
		WorkerCount: cwdLookWorkers,
		Work: func(ctx context.Context, agent agentstore.Agent) (cwdLook, error) {
			return cs.look(ctx, agent, rounds[agent.ID], bases[agent.WorkflowID]), nil
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

func (cs *CwdSyncer) look(ctx context.Context, agent agentstore.Agent, round, base string) cwdLook {
	look := cwdLook{agent: agent, removed: isCwdRemoved(ctx, agent.Cwd)}

	if !look.removed {
		look.head = cs.readHead(ctx, agent.Cwd)
		look.standing, look.err = cs.readStanding(ctx, agent, round, base)
	}

	return look
}

// write records what the look found, when any of it differs from the row.
func (cs *CwdSyncer) write(ctx context.Context, look cwdLook) error {
	agent, head := look.agent, look.head
	checkout := agentstore.SetCheckoutParams{
		Branch:             xptr.NonEmpty(head.Branch),
		RepositoryURL:      xptr.NonEmpty(head.RepositoryURL),
		WorktreeRemoved:    look.removed,
		SequenceInProgress: head.SequenceInProgress,
	}
	unchanged := xptr.Equal(agent.Branch, checkout.Branch) &&
		xptr.Equal(agent.RepositoryURL, checkout.RepositoryURL) &&
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

func (cs *CwdSyncer) readHead(ctx context.Context, dir string) git.HeadState {
	if dir == "" {
		return git.HeadState{}
	}

	ctx, cancel := context.WithTimeout(ctx, cwdLookTimeout)
	defer cancel()

	return cs.heads.ReadHeadState(ctx, dir)
}

func (cs *CwdSyncer) readStanding(ctx context.Context, agent agentstore.Agent, round, base string) (git.Standing, error) {
	if agent.Cwd == "" || round == "" {
		return git.StandingSame, nil
	}

	ctx, cancel := context.WithTimeout(ctx, cwdLookTimeout)
	defer cancel()

	standing, err := cs.trees.ReadStanding(ctx, git.ReadStandingParams{Dir: agent.Cwd, Base: base, SnapshotSHA: round})
	if err != nil {
		return standing, fmt.Errorf("failed to compare the tree of %q against its reviewed round: %w", agent.Name, err)
	}

	return standing, nil
}
