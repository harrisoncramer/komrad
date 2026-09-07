package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"weave/internal/store"
	agentstore "weave/internal/store/agent"
)

// ErrReviewNeedsOwnWork says the agent asked for is one with no work of its own to be read,
// which a reviewer and a child both are.
var ErrReviewNeedsOwnWork = errors.New("this agent has no work of its own to have read")

// reviewerNameSuffix is what a reviewer is called after the agent it reads, so the panel's
// sidebar says whose work a row is for without anything having to be looked up.
const reviewerNameSuffix = "-reviewer"

type RequestReviewParams struct {
	AgentID        string
	Prompt         string
	Title          string
	IdempotencyKey string
}

// RequestReview spawns the reviewer for an agent's own work and hands back the rest of the loop
// that agent is in, which is the half of the review briefing it was not launched with.
func (s *LaunchService) RequestReview(ctx context.Context, p RequestReviewParams) (*agentstore.Agent, string, error) {
	author, err := s.agents.GetByID(ctx, p.AgentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, "", fmt.Errorf("%w: %q", ErrAgentNotFound, p.AgentID)
	}
	if err != nil {
		return nil, "", err
	}

	if author.ArchivedAt != nil {
		return nil, "", fmt.Errorf("%w: %q", ErrAgentArchived, author.Name)
	}

	// A child reads its parent's checkout rather than writing one of its own, so neither a
	// reviewer nor any other child has a change of its own for a second agent to be shown.
	switch {
	case author.Role == agentstore.RoleReviewer:
		return nil, "", fmt.Errorf("%w: %q was launched to review", ErrReviewNeedsOwnWork, author.Name)
	case author.ParentAgentID != nil:
		return nil, "", fmt.Errorf("%w: %q is a child, writing in a checkout it does not own", ErrReviewNeedsOwnWork, author.Name)
	}

	running, err := liveReviewerFor(ctx, s.agents, *author)
	if err != nil {
		return nil, "", err
	}
	if running != nil {
		return nil, "", fmt.Errorf("%w: %q (%s) is reading it, and answering its threads is what tells it there is more to read",
			ErrReviewerAlreadyRunning, running.Name, running.ID)
	}

	reviewer, err := s.NewAgent(ctx, NewAgentParams{
		Title:          reviewerTitle(p.Title, author.Name),
		Prompt:         reviewerPrompt(*author, p.Prompt),
		WorkflowID:     author.WorkflowID,
		ParentAgentID:  author.ID,
		Role:           agentstore.RoleReviewer,
		IdempotencyKey: p.IdempotencyKey,
	})
	if err != nil && !errors.Is(err, ErrAlreadyLaunched) {
		return nil, "", err
	}

	return reviewer, implementerDoc, nil
}

// liveReviewerFor is the reviewer an agent already has, which is what keeps one reviewer on a
// change for all of its rounds rather than a second one arriving at the round after the first.
func liveReviewerFor(ctx context.Context, agents childLister, author agentstore.Agent) (*agentstore.Agent, error) {
	children, err := agents.ListChildren(ctx, []string{author.ID})
	if err != nil {
		return nil, err
	}

	for _, child := range children {
		if child.Role != agentstore.RoleReviewer || child.ArchivedAt != nil || child.State.IsFailed() {
			continue
		}

		return &child, nil
	}

	return nil, nil
}

func reviewerTitle(title, authorName string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}

	return authorName + reviewerNameSuffix
}

// reviewerPrompt says whose work this is and where it sits before handing over what the agent
// asking wrote, since the branch is the one fact weave holds that the asker tends to leave out.
func reviewerPrompt(author agentstore.Agent, prompt string) string {
	opening := fmt.Sprintf("You are the reviewer for agent %s (%s). Read the work in %s and release a review of it.",
		author.Name, author.ID, author.Cwd)
	if author.Branch != nil && *author.Branch != "" {
		opening = fmt.Sprintf("%s The change is on branch %s.", opening, *author.Branch)
	}

	return opening + "\n\n" + strings.TrimSpace(prompt)
}
