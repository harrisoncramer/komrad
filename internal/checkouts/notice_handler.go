package checkouts

import (
	"context"
	"encoding/json"
	"fmt"

	"komrad/internal/mail"
	"komrad/internal/notices"
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

// NoticeHandler keeps one unread notice per agent saying what the last rebase of its worktree did.
type NoticeHandler struct{}

// NewNoticeHandler builds the handler the router is given for rebases.
func NewNoticeHandler() *NoticeHandler {
	return &NoticeHandler{}
}

// Topic is the topic of Rebased.
func (*NoticeHandler) Topic() string {
	return rebasedTopic
}

// Handle tells the agent what became of its worktree.
func (*NoticeHandler) Handle(ctx context.Context, _ store.DBTX, mailer *mail.MailService, payload json.RawMessage) ([]river.JobArgs, error) {
	var rebased Rebased
	if err := json.Unmarshal(payload, &rebased); err != nil {
		return nil, fmt.Errorf("failed to read a rebase: %w", err)
	}

	body := cutMail(rebased.Failure)
	if rebased.Result != nil {
		body = rebaseMail(*rebased.Result)
	}
	key := notices.Key{ToAgentID: rebased.AgentID, Kind: mailstore.KindWorktreeRebased, SubjectID: rebased.AgentID}

	return nil, notices.Rewrite(ctx, mailer, key, func(struct{}, bool) (notices.Draft[struct{}], bool) {
		return notices.Draft[struct{}]{Body: body}, true
	})
}
