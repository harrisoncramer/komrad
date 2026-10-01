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

// NewEventHandler builds the handler the router is given for rebases.
func NewEventHandler() events.HandlerFor[Rebased] {
	return handleRebased
}

// handleRebased tells the agent what the latest rebase of its worktree came to.
func handleRebased(ctx context.Context, tx store.DBTX, mailer *mail.MailService, rebased Rebased) ([]river.JobArgs, error) {
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

const pushedTopic = "worktree_pushed"

// Pushed is an agent's branch having been force-pushed to origin from the panel.
type Pushed struct {
	AgentID string `json:"agent_id"`
	Branch  string `json:"branch"`
	SHA     string `json:"sha"`
}

// Topic names the handler that tells the agent.
func (Pushed) Topic() string {
	return pushedTopic
}

// NewPushedHandler builds the handler the router is given for force-pushes.
func NewPushedHandler() events.HandlerFor[Pushed] {
	return handlePushed
}

// handlePushed tells the agent its branch on origin was overwritten.
func handlePushed(ctx context.Context, _ store.DBTX, mailer *mail.MailService, pushed Pushed) ([]river.JobArgs, error) {
	return nil, events.Send(ctx, mailer, mail.SendFromKomradParams{
		ToAgentID: pushed.AgentID,
		Kind:      mailstore.KindWorktreePushed,
		SubjectID: pushed.AgentID,
		Body:      pushedMail(pushed),
	})
}
