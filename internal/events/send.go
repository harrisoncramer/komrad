package events

import (
	"context"
	"errors"

	"komrad/internal/mail"
	mailstore "komrad/internal/store/mail"
)

// Send writes mail as komrad and returns it, or nil when the mail had nobody to reach or nothing to rewrite.
func Send(ctx context.Context, mailer *mail.MailService, params mail.SendFromKomradParams) (*mailstore.Mail, error) {
	sent, err := mailer.SendFromKomrad(ctx, params)
	if errors.Is(err, mail.ErrNothingUnread) {
		err = nil
	}
	if err != nil {
		return nil, dropUnreachable(ctx, params.ToAgentID, err)
	}

	return sent, nil
}
