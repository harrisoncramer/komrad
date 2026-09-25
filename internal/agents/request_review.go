package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"komrad/internal/fail"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"
)

// ErrReviewNeedsOwnWork says the agent asked for has no work of its own to be read, which is
// true of a reviewer and of any child.
var ErrReviewNeedsOwnWork = fail.Precondition("this agent has no work of its own to have read")

// liveReviewerFinder reads the reviewer still owed an answer about an agent's work.
type liveReviewerFinder interface {
	GetLiveReviewerOf(ctx context.Context, agentID string) (*reviewstore.LiveReviewer, error)
}

// reviewerNameSuffix is what a reviewer is called after the agent it reads, so the sidebar says
// whose work a row is for.
const reviewerNameSuffix = "-reviewer"

type RequestReviewParams struct {
	AgentID        string
	Prompt         string
	Title          string
	Harness        string
	Flags          string
	IdempotencyKey string
}

// RequestReview spawns the reviewer for an agent's own work and hands back the half of the
// review briefing that agent was not launched with.
func (s *LaunchService) RequestReview(ctx context.Context, params RequestReviewParams) (*agentstore.Agent, string, error) {
	author, err := s.agents.GetByID(ctx, params.AgentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, "", ErrAgentNotFound.Saying("no agent with id %q", params.AgentID)
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
	case author.Role == agentstore.RoleOrchestrator:
		return nil, "", fmt.Errorf("%w: %q writes no code, so it has no change to be read", ErrReviewNeedsOwnWork, author.Name)
	case author.ParentAgentID != nil:
		return nil, "", fmt.Errorf("%w: %q is a child, writing in a checkout it does not own", ErrReviewNeedsOwnWork, author.Name)
	}

	running, err := s.liveReviewerFor(ctx, *author)
	if err != nil && !errors.Is(err, ErrNoLiveReviewer) {
		return nil, "", err
	}
	if running != nil {
		// A key spent on the reviewer that exists is a retry of the call that made it, which is
		// asking for that reviewer rather than for a second one.
		if params.IdempotencyKey != "" && running.IdempotencyKey != nil && *running.IdempotencyKey == params.IdempotencyKey {
			return running, implementerDoc, nil
		}

		return nil, "", fmt.Errorf(
			"%w: %q (%s) is reading it, so answer its threads rather than asking again, and resume it with resume_agent if its pane is gone",
			ErrReviewerAlreadyRunning, running.Name, running.ID)
	}

	reviewer, err := s.NewAgent(ctx, NewAgentParams{
		Title:          reviewerTitle(params.Title, author.Name),
		Prompt:         reviewerPrompt(*author, params.Prompt),
		Harness:        params.Harness,
		Flags:          params.Flags,
		WorkflowID:     author.WorkflowID,
		ParentAgentID:  author.ID,
		Role:           agentstore.RoleReviewer,
		IdempotencyKey: params.IdempotencyKey,
	})
	if err != nil && !errors.Is(err, ErrAlreadyLaunched) {
		return nil, "", err
	}

	return reviewer, implementerDoc, nil
}

// liveReviewerFor is the reviewer an agent still has to answer to, skipping one that has
// approved.
func (s *LaunchService) liveReviewerFor(ctx context.Context, author agentstore.Agent) (*agentstore.Agent, error) {
	reading, err := s.reviews.GetLiveReviewerOf(ctx, author.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrNoLiveReviewer, author.Name)
	}
	if err != nil {
		return nil, err
	}

	return s.agents.GetByID(ctx, reading.ReviewerID)
}

func reviewerTitle(title, authorName string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}

	return authorName + reviewerNameSuffix
}

// reviewerPrompt says whose work this is and where it sits before handing over what the agent
// asking wrote.
func reviewerPrompt(author agentstore.Agent, prompt string) string {
	opening := fmt.Sprintf("You are the reviewer for agent %s (%s). Read the work in %s and release a review of it.",
		author.Name, author.ID, author.Cwd)
	if author.Branch != nil && *author.Branch != "" {
		opening = fmt.Sprintf("%s The change is on branch %s.", opening, *author.Branch)
	}

	return opening + "\n\n" + strings.TrimSpace(prompt)
}
