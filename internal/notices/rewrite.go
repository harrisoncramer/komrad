package notices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"komrad/internal/mail"
	"komrad/internal/store"
	mailstore "komrad/internal/store/mail"
)

// Key names the one unread notice a recipient may hold of a kind about a subject.
type Key struct {
	ToAgentID string
	Kind      mailstore.Kind
	SubjectID string
}

// Draft is what a notice says and the state kept for the next rewrite to read.
type Draft[S any] struct {
	Body  string
	State S
}

// Rewrite holds the unread notice named by key while decide says what it should now say.
func Rewrite[S any](ctx context.Context, mailer *mail.MailService, key Key, decide func(earlier S, held bool) (Draft[S], bool)) error {
	if err := mailer.LockNotice(ctx, key.ToAgentID, key.Kind, key.SubjectID); err != nil {
		return err
	}

	var earlier S
	pending, err := mailer.GetPendingNotice(ctx, key.ToAgentID, key.Kind, key.SubjectID)
	held := err == nil
	switch {
	case errors.Is(err, store.ErrNotFound):
	case err != nil:
		return err
	case len(pending.SubjectState) > 0:
		if err := json.Unmarshal(pending.SubjectState, &earlier); err != nil {
			return fmt.Errorf("failed to read what notice %s last said: %w", pending.ID, err)
		}
	}

	draft, send := decide(earlier, held)
	if !send {
		if !held {
			return nil
		}

		return mailer.WithdrawNotice(ctx, key.ToAgentID, key.Kind, key.SubjectID)
	}

	state, err := json.Marshal(draft.State)
	if err != nil {
		return fmt.Errorf("failed to record what the notice says: %w", err)
	}

	_, err = mailer.SendNotice(ctx, mail.SendNoticeParams{
		ToAgentID:    key.ToAgentID,
		Body:         draft.Body,
		Kind:         key.Kind,
		SubjectID:    key.SubjectID,
		SubjectState: state,
	})

	return DropUnreachable(ctx, key.ToAgentID, err)
}
