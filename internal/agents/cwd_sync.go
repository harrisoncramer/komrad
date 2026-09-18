package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"weave/internal/git"
	agentstore "weave/internal/store/agent"
	"weave/lib/xptr"

	"github.com/harrisoncramer/streamer"
)

// cwdLookTimeout bounds each look at a directory, since a dead network mount hangs rather than
// failing.
const cwdLookTimeout = 5 * time.Second

// cwdLookWorkers is how many directories are looked at together.
const cwdLookWorkers = 8

type cwdSetter interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	SetBranchByID(ctx context.Context, id string, branch *string) error
	SetRepositoryURLByID(ctx context.Context, id string, url *string) error
	SetWorktreeRemovedByID(ctx context.Context, id string, removed bool) error
	SetSequenceInProgressByID(ctx context.Context, id string, inProgress bool) error
}

// headReader reads a checkout's HEAD.
type headReader interface {
	ReadHeadState(ctx context.Context, dir string) git.HeadState
}

// CwdSyncer keeps what the panel shows of an agent's working directory in step with the
// directory itself.
type CwdSyncer struct {
	agents cwdSetter
	heads  headReader
}

// NewCwdSyncer builds a syncer over the agent store.
func NewCwdSyncer(agents cwdSetter, heads headReader) *CwdSyncer {
	return &CwdSyncer{agents: agents, heads: heads}
}

// cwdLook is what one pass found at an agent's directory.
type cwdLook struct {
	agent   agentstore.Agent
	removed bool
	head    git.HeadState
}

// Sync looks at every live agent's cwd once and writes what changed, clearing the branch and
// repository when the cwd names no git checkout and leaving an agent with no cwd alone.
func (cs *CwdSyncer) Sync(ctx context.Context) error {
	agents, err := cs.agents.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents for cwd sync: %w", err)
	}

	looks, err := cs.lookAtAll(ctx, agents)
	if err != nil {
		return err
	}

	var failures []error
	for _, look := range looks {
		failures = append(failures, cs.write(ctx, look)...)
	}

	return errors.Join(failures...)
}

func (cs *CwdSyncer) lookAtAll(ctx context.Context, agents []agentstore.Agent) ([]cwdLook, error) {
	looker, err := streamer.NewStreamer(streamer.NewStreamerParams[agentstore.Agent, cwdLook]{
		WorkerCount: cwdLookWorkers,
		Work: func(ctx context.Context, agent agentstore.Agent) (cwdLook, error) {
			return cs.look(ctx, agent), nil
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

func (cs *CwdSyncer) look(ctx context.Context, agent agentstore.Agent) cwdLook {
	look := cwdLook{agent: agent, removed: isCwdRemoved(ctx, agent.Cwd)}

	// A directory that is gone has no head, so the git call is skipped rather than
	// run against it and waited out.
	if !look.removed {
		look.head = cs.readHead(ctx, agent.Cwd)
	}

	return look
}

func (cs *CwdSyncer) write(ctx context.Context, look cwdLook) []error {
	agent, head := look.agent, look.head

	var failures []error
	if look.removed != agent.WorktreeRemoved {
		if err := cs.agents.SetWorktreeRemovedByID(ctx, agent.ID, look.removed); err != nil {
			failures = append(failures, fmt.Errorf("failed to set worktree_removed for %q: %w", agent.Name, err))
		}
	}

	if branch := xptr.NonEmpty(head.Branch); !xptr.Equal(agent.Branch, branch) {
		if err := cs.agents.SetBranchByID(ctx, agent.ID, branch); err != nil {
			failures = append(failures, fmt.Errorf("failed to set branch for %q: %w", agent.Name, err))
		}
	}

	if url := xptr.NonEmpty(head.RepositoryURL); !xptr.Equal(agent.RepositoryURL, url) {
		if err := cs.agents.SetRepositoryURLByID(ctx, agent.ID, url); err != nil {
			failures = append(failures, fmt.Errorf("failed to set repository_url for %q: %w", agent.Name, err))
		}
	}

	if head.SequenceInProgress != agent.SequenceInProgress {
		if err := cs.agents.SetSequenceInProgressByID(ctx, agent.ID, head.SequenceInProgress); err != nil {
			failures = append(failures, fmt.Errorf("failed to set sequence_in_progress for %q: %w", agent.Name, err))
		}
	}

	return failures
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
