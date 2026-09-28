package events

import (
	"context"
	"errors"

	"komrad/internal/mail"
)

// Send writes mail as komrad, dropping mail that has nobody to reach or nothing to rewrite.
func Send(ctx context.Context, mailer *mail.MailService, params mail.SendFromKomradParams) error {
	_, err := mailer.SendFromKomrad(ctx, params)
	if errors.Is(err, mail.ErrNothingUnread) {
		return nil
	}

	return DropUnreachable(ctx, params.ToAgentID, err)
}
