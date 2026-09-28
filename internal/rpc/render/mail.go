package render

import (
	mailstore "komrad/internal/store/mail"
	"komrad/lib/dates"
	agentv1 "komrad/proto/gen/agent/v1"
)

// Mail draws one piece of mail as the wire carries it.
func Mail(stored mailstore.Mail) *agentv1.Mail {
	return &agentv1.Mail{
		Id:          stored.ID,
		FromAgentId: stored.FromAgentID,
		FromName:    stored.FromName,
		ToAgentId:   stored.ToAgentID,
		Body:        stored.Body,
		ReplyTo:     stored.ReplyTo,
		CreatedAt:   dates.Stamp(stored.CreatedAt),
		ClaimedAt:   dates.StampPtr(stored.ClaimedAt),
		ExpiresAt:   dates.Stamp(stored.ExpiresAt),
		Kind:        mailKindOf(stored.Kind),
	}
}

func mailKindOf(kind mailstore.Kind) agentv1.MailKind {
	switch kind {
	case mailstore.KindPlain:
		return agentv1.MailKind_MAIL_KIND_PLAIN
	case mailstore.KindReviewActivity:
		return agentv1.MailKind_MAIL_KIND_REVIEW_ACTIVITY
	case mailstore.KindExchangeCapped:
		return agentv1.MailKind_MAIL_KIND_EXCHANGE_CAPPED
	case mailstore.KindDocumentEdited:
		return agentv1.MailKind_MAIL_KIND_DOCUMENT_EDITED
	case mailstore.KindPullRequestChanged:
		return agentv1.MailKind_MAIL_KIND_PULL_REQUEST_CHANGED
	case mailstore.KindWorktreeRebased:
		return agentv1.MailKind_MAIL_KIND_WORKTREE_REBASED
	}

	return agentv1.MailKind_MAIL_KIND_UNSPECIFIED
}
