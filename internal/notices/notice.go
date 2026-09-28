package notices

import (
	"context"
	"errors"

	"komrad/internal/mail"
	"komrad/internal/store"
	mailstore "komrad/internal/store/mail"
)

// Notice is the one unread notice a recipient may hold of a kind about a subject.
type Notice struct {
	ToAgentID string
	Kind      mailstore.Kind
	SubjectID string
	Body      string
	// OnlyRewrite leaves a recipient holding no unread notice without one.
	OnlyRewrite bool
}

// Write writes the notice, rewriting the unread one the recipient already holds.
func Write(ctx context.Context, mailer *mail.MailService, notice Notice) error {
	if notice.OnlyRewrite {
		_, err := mailer.GetPendingNotice(ctx, notice.ToAgentID, notice.Kind, notice.SubjectID)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
	}

	_, err := mailer.SendNotice(ctx, mail.SendNoticeParams{
		ToAgentID: notice.ToAgentID,
		Body:      notice.Body,
		Kind:      notice.Kind,
		SubjectID: notice.SubjectID,
	})

	return DropUnreachable(ctx, notice.ToAgentID, err)
}
