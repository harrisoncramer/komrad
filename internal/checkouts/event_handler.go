package checkouts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"komrad/internal/events"
	"komrad/internal/mail"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"

	"github.com/riverqueue/river"
)

const rebasedTopic = "worktree_rebased"

// Rebased is an agent's worktree having been rebased, or a rebase of it having failed.
type Rebased struct {
	AgentID string `json:"agent_id"`
}

// Topic names the handler that tells the agent.
func (Rebased) Topic() string {
	return rebasedTopic
}

// rebaseOutcome is what the latest rebase of an agent's worktree came to.
type rebaseOutcome struct {
	Result  *RebaseResult `json:"result,omitempty"`
	Failure string        `json:"failure,omitempty"`
}

// EventHandler keeps one unread mail per agent saying what the last rebase of its worktree did.
type EventHandler struct{}

// NewEventHandler builds the handler the router is given for rebases.
func NewEventHandler() *EventHandler {
	return &EventHandler{}
}

// Topic is the topic of Rebased.
func (*EventHandler) Topic() string {
	return rebasedTopic
}

// Handle tells the agent what the latest rebase of its worktree came to.
func (*EventHandler) Handle(ctx context.Context, tx store.DBTX, mailer *mail.MailService, payload json.RawMessage) ([]river.JobArgs, error) {
	var rebased Rebased
	if err := json.Unmarshal(payload, &rebased); err != nil {
		return nil, fmt.Errorf("failed to read a rebase: %w", err)
	}

	raw, err := agentstore.NewAgentStore(tx).GetLastRebaseByID(ctx, rebased.AgentID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && raw == nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var outcome rebaseOutcome
	if err := json.Unmarshal(raw, &outcome); err != nil {
		return nil, fmt.Errorf("failed to read the last rebase of agent %q: %w", rebased.AgentID, err)
	}

	body := cutMail(outcome.Failure)
	if outcome.Result != nil {
		body = rebaseMail(*outcome.Result)
	}

	return nil, events.Send(ctx, mailer, mail.SendFromKomradParams{
		ToAgentID: rebased.AgentID,
		Kind:      mailstore.KindWorktreeRebased,
		SubjectID: rebased.AgentID,
		Body:      body,
	})
}
