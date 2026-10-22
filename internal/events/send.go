package events

import (
	"context"
	"errors"
	"log/slog"

	"komrad/internal/logging"
	"komrad/internal/mail"
	mailstore "komrad/internal/store/mail"

	"github.com/google/uuid"
)

// Send writes mail as komrad and returns it, or nil when the mail had nobody to reach or nothing to rewrite.
func Send(ctx context.Context, mailer *mail.MailService, params mail.SendFromKomradParams) (*mailstore.Mail, error) {
	sent, err := mailer.SendFromKomrad(ctx, params)
	if err != nil && !errors.Is(err, mail.ErrNothingUnread) {
		return nil, dropUnreachable(ctx, params.ToAgentID, err)
	}

	return sent, nil
}

// dropUnreachable returns err unless it says the recipient is archived or gone.
func dropUnreachable(ctx context.Context, recipient uuid.UUID, err error) error {
	if !errors.Is(err, mail.ErrAgentArchived) && !errors.Is(err, mail.ErrRecipientNotFound) {
		return err
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "dropped mail for an agent that is gone",
		append([]slog.Attr{slog.String(logging.AgentID, recipient.String())}, logging.Err(err)...)...)

	return nil
}
