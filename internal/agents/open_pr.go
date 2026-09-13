package agents

import (
	"weave/internal/fail"

	"context"
	"errors"
	"fmt"

	"weave/internal/browser"
	"weave/internal/store"
)

var ErrNoPullRequest = fail.Precondition("no pull request is recorded on this agent")

type NewPullRequestOpenerParams struct {
	Agents agentGetter
	Opener browser.Opener
}

// PullRequestOpener opens the pull request an agent recorded for itself.
type PullRequestOpener struct {
	agents agentGetter
	opener browser.Opener
}

// NewPullRequestOpener builds the opener over the agent registry.
func NewPullRequestOpener(params NewPullRequestOpenerParams) *PullRequestOpener {
	return &PullRequestOpener{
		agents: params.Agents,
		opener: params.Opener,
	}
}

// OpenPullRequest reads the URL an agent recorded and waits for the opener, so whoever pressed
// the key is told when it fails.
func (o *PullRequestOpener) OpenPullRequest(ctx context.Context, agentID string) error {
	agent, err := o.agents.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrAgentNotFound.Saying("no agent with id %q", agentID)
	}
	if err != nil {
		return fmt.Errorf("failed to read agent %q: %w", agentID, err)
	}

	recorded := metadataFor(*agent, prMetadataKey)
	if recorded == "" {
		return ErrNoPullRequest
	}

	if err := o.opener.OpenURL(ctx, recorded); err != nil {
		return fmt.Errorf("failed to open %s: %w", recorded, err)
	}

	return nil
}
