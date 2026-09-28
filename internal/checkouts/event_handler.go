package checkouts

import (
	"context"
	"encoding/json"
	"fmt"

	"komrad/internal/events"
	"komrad/internal/mail"
	"komrad/internal/store"
	mailstore "komrad/internal/store/mail"

	"github.com/riverqueue/river"
)

const rebasedTopic = "worktree_rebased"

// Rebased is what became of a rebase of an agent's worktree.
type Rebased struct {
	AgentID string        `json:"agent_id"`
	Result  *RebaseResult `json:"result,omitempty"`
	Failure string        `json:"failure,omitempty"`
}

// Topic names the handler that tells the agent.
func (Rebased) Topic() string {
	return rebasedTopic
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

// Handle tells the agent what became of its worktree.
func (*EventHandler) Handle(ctx context.Context, _ store.DBTX, mailer *mail.MailService, payload json.RawMessage) ([]river.JobArgs, error) {
	var rebased Rebased
	if err := json.Unmarshal(payload, &rebased); err != nil {
		return nil, fmt.Errorf("failed to read a rebase: %w", err)
	}

	body := cutMail(rebased.Failure)
	if rebased.Result != nil {
		body = rebaseMail(*rebased.Result)
	}

	return nil, events.Send(ctx, mailer, mail.SendFromKomradParams{
		ToAgentID: rebased.AgentID,
		Kind:      mailstore.KindWorktreeRebased,
		SubjectID: rebased.AgentID,
		Body:      body,
	})
}
