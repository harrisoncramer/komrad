package mail

import (
	"fmt"

	"komrad/db/models"
	agentv1 "komrad/proto/gen/agent/v1"
)

// fromServiceToDBMailKind is the stored value of a mail kind.
func fromServiceToDBMailKind(kind agentv1.MailKind) (models.MailKind, error) {
	switch kind {
	case agentv1.MailKind_MAIL_KIND_PLAIN:
		return models.MailKindPlain, nil
	case agentv1.MailKind_MAIL_KIND_REVIEW_ACTIVITY:
		return models.MailKindReviewActivity, nil
	case agentv1.MailKind_MAIL_KIND_EXCHANGE_CAPPED:
		return models.MailKindExchangeCapped, nil
	case agentv1.MailKind_MAIL_KIND_DOCUMENT_EDITED:
		return models.MailKindDocumentEdited, nil
	case agentv1.MailKind_MAIL_KIND_PULL_REQUEST_CHANGED:
		return models.MailKindPullRequestChanged, nil
	case agentv1.MailKind_MAIL_KIND_WORKTREE_REBASED:
		return models.MailKindWorktreeRebased, nil
	case agentv1.MailKind_MAIL_KIND_WORKTREE_PUSHED:
		return models.MailKindWorktreePushed, nil
	case agentv1.MailKind_MAIL_KIND_WORKTREE_ABORTED:
		return models.MailKindWorktreeAborted, nil
	case agentv1.MailKind_MAIL_KIND_REVIEW_COMMENT:
		return models.MailKindReviewComment, nil
	case agentv1.MailKind_MAIL_KIND_UNSPECIFIED:
	}

	return "", fmt.Errorf("a mail kind %v has no stored value", kind)
}

// fromDBToServiceMailKind is the mail kind a stored value names.
func fromDBToServiceMailKind(kind models.MailKind) (agentv1.MailKind, error) {
	switch kind {
	case models.MailKindPlain:
		return agentv1.MailKind_MAIL_KIND_PLAIN, nil
	case models.MailKindReviewActivity:
		return agentv1.MailKind_MAIL_KIND_REVIEW_ACTIVITY, nil
	case models.MailKindExchangeCapped:
		return agentv1.MailKind_MAIL_KIND_EXCHANGE_CAPPED, nil
	case models.MailKindDocumentEdited:
		return agentv1.MailKind_MAIL_KIND_DOCUMENT_EDITED, nil
	case models.MailKindPullRequestChanged:
		return agentv1.MailKind_MAIL_KIND_PULL_REQUEST_CHANGED, nil
	case models.MailKindWorktreeRebased:
		return agentv1.MailKind_MAIL_KIND_WORKTREE_REBASED, nil
	case models.MailKindWorktreePushed:
		return agentv1.MailKind_MAIL_KIND_WORKTREE_PUSHED, nil
	case models.MailKindWorktreeAborted:
		return agentv1.MailKind_MAIL_KIND_WORKTREE_ABORTED, nil
	case models.MailKindReviewComment:
		return agentv1.MailKind_MAIL_KIND_REVIEW_COMMENT, nil
	}

	return agentv1.MailKind_MAIL_KIND_UNSPECIFIED, fmt.Errorf("stored mail kind %q has no service value", kind)
}
