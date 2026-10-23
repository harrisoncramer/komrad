package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"komrad/internal/agenterr"
	"komrad/internal/fail"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/oneofs"

	"github.com/google/uuid"
)

// reviewerNameSuffix is what a reviewer is called after the agent it reads, so the sidebar says
// whose work a row is for.
const reviewerNameSuffix = "-reviewer"

// errKeySpentElsewhere is a request_review retry key that already launched an agent other than this
// author's reviewer.
var errKeySpentElsewhere = fail.AlreadyExists("this idempotency key already launched an agent that is not this agent's reviewer")

type RequestReviewParams struct {
	AgentID        uuid.UUID
	Prompt         string
	Title          string
	Harness        string
	Flags          string
	IdempotencyKey string
}

// RequestReview spawns a reviewer for an agent's own work and hands back how to settle what it
// raises.
func (s *LaunchService) RequestReview(ctx context.Context, params RequestReviewParams) (*agentstore.Agent, string, error) {
	author, err := s.agents.GetByID(ctx, params.AgentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, "", agenterr.ErrAgentNotFound.Saying("no agent with id %q", params.AgentID)
	}
	if err != nil {
		return nil, "", err
	}

	if author.ArchivedAt != nil {
		return nil, "", fmt.Errorf("%w: %q", agenterr.ErrAgentArchived, author.Name)
	}

	// A reviewer reads its task's checkout rather than writing one, so it has no change of its own
	// for a second agent to be shown.
	switch {
	case author.Role == agentv1.AgentRole_AGENT_ROLE_REVIEWER:
		return nil, "", fmt.Errorf("%w: %q was launched to review", agenterr.ErrReviewNeedsOwnWork, author.Name)
	case author.Role == agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR:
		return nil, "", fmt.Errorf("%w: %q writes no code, so it has no change to be read", agenterr.ErrReviewNeedsOwnWork, author.Name)
	case author.TaskID == nil:
		return nil, "", fmt.Errorf("%w: %q works on no task, so it has no change to be read", agenterr.ErrReviewNeedsOwnWork, author.Name)
	}

	branch, err := s.tasks.GetAgentBranch(ctx, *author)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read the task of agent %q: %w", author.Name, err)
	}

	reviewer, err := s.NewAgent(ctx, NewAgentParams{
		Title:          reviewerTitle(params.Title, author.Name),
		Prompt:         reviewerPrompt(*author, branch, params.Prompt),
		Harness:        params.Harness,
		Flags:          params.Flags,
		WorkflowID:     author.WorkflowID,
		Task:           oneofs.Task{TaskId: author.TaskID},
		Role:           agentv1.AgentRole_AGENT_ROLE_REVIEWER,
		IdempotencyKey: params.IdempotencyKey,
	})
	if errors.Is(err, agenterr.ErrAlreadyLaunched) && !isReviewerOf(*reviewer, *author) {
		return nil, "", errKeySpentElsewhere.Saying("key %q launched %q, so ask with a key of its own", params.IdempotencyKey, reviewer.Name)
	}
	if err != nil && !errors.Is(err, agenterr.ErrAlreadyLaunched) {
		return nil, "", err
	}

	return reviewer, settlingDoc, nil
}

// isReviewerOf reports whether the agent reviews the author's task.
func isReviewerOf(agent, author agentstore.Agent) bool {
	return agent.Role == agentv1.AgentRole_AGENT_ROLE_REVIEWER &&
		agent.TaskID != nil && author.TaskID != nil && *agent.TaskID == *author.TaskID
}

func reviewerTitle(title, authorName string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}

	return authorName + reviewerNameSuffix
}

// reviewerPrompt says whose work this is and where it sits before handing over what the agent
// asking wrote.
func reviewerPrompt(author agentstore.Agent, branch, prompt string) string {
	opening := fmt.Sprintf("You are the reviewer for agent %s (%s). Read the work in %s and release a review of it.",
		author.Name, author.ID, author.Cwd)
	if branch != "" {
		opening = fmt.Sprintf("%s The change is on branch %s.", opening, branch)
	}

	return opening + "\n\n" + strings.TrimSpace(prompt)
}
