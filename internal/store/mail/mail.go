// Package mail stores the messages agents send each other, and which of them the recipient has
// claimed, so the delivery loop can find what is still waiting and hand it over.
package mail

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"
)

// Cursor pages this listing, which resumes from one row's id.
type Cursor = store.IDCursor

// Kind tells an agent's own mail, always KindPlain, from a notice weave raises itself.
type Kind string

const (
	// KindPlain is ordinary mail.
	KindPlain Kind = Kind(models.MailKindPlain)
	// KindReviewActivity is a doorbell for review waiting on the agent's diff.
	KindReviewActivity Kind = Kind(models.MailKindReviewActivity)
	// KindExchangeCapped tells an agent that mail written to it was refused because the pair
	// had reached the cap on how much they may exchange.
	KindExchangeCapped Kind = Kind(models.MailKindExchangeCapped)
)

type Mail struct {
	ID          string
	FromAgentID *string
	FromName    string
	ToAgentID   string
	Body        string
	Kind        Kind
	ReplyTo     *string
	// AboutAgentID is the other agent a notice weave raised is about, unset on ordinary mail.
	AboutAgentID *string
	ClaimedAt    *time.Time
	ExpiresAt    time.Time
	CreatedAt    time.Time
}

type Store struct {
	queries *models.Queries
}

// New builds the mail store over a database handle or a transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

type SendParams struct {
	FromAgentID  *string
	FromName     string
	ToAgentID    string
	Body         string
	Kind         Kind
	ReplyTo      *string
	AboutAgentID *string
	// How long the mail stays deliverable, counted from the database's clock rather than
	// the caller's, since every read of expires_at compares it against NOW().
	Deadline time.Duration
}

// Send writes one piece of mail.
func (s *Store) Send(ctx context.Context, params SendParams) (*Mail, error) {
	kind := params.Kind
	if kind == "" {
		kind = KindPlain
	}

	row, err := s.queries.InsertMail(ctx, models.InsertMailParams{
		FromAgentID:    params.FromAgentID,
		FromName:       params.FromName,
		ToAgentID:      params.ToAgentID,
		Body:           params.Body,
		Kind:           models.MailKind(kind),
		ReplyTo:        params.ReplyTo,
		AboutAgentID:   params.AboutAgentID,
		DeadlineMicros: params.Deadline.Microseconds(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to send mail to agent %q: %w", params.ToAgentID, err)
	}

	sent := mailFromRow(*row)

	return &sent, nil
}

// GetPendingOfKind is the notice of a kind, about the same agent, that its recipient is already
// holding unread, ErrNotFound when it holds none, which is what a caller joins rather than
// queueing an identical one behind it.
func (s *Store) GetPendingOfKind(ctx context.Context, toAgentID string, kind Kind, about *string) (*Mail, error) {
	if !store.IsID(toAgentID) {
		return nil, noPendingOfKind(toAgentID, kind)
	}

	row, err := s.queries.GetPendingMailOfKind(ctx, models.GetPendingMailOfKindParams{
		ToAgentID:    toAgentID,
		Kind:         models.MailKind(kind),
		AboutAgentID: about,
	})
	if db.IsNoRows(err) {
		return nil, noPendingOfKind(toAgentID, kind)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the unread %q notice of agent %q: %w", kind, toAgentID, err)
	}

	held := mailFromRow(*row)

	return &held, nil
}

func noPendingOfKind(toAgentID string, kind Kind) error {
	return fmt.Errorf("%w: an unread %q notice for agent %q", store.ErrNotFound, kind, toAgentID)
}

// GetByID returns one piece of mail, claimed or not.
func (s *Store) GetByID(ctx context.Context, id string) (*Mail, error) {
	if !store.IsID(id) {
		return nil, fmt.Errorf("%w: mail %q", store.ErrNotFound, id)
	}

	row, err := s.queries.GetMail(ctx, id)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: mail %q", store.ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get mail %q: %w", id, err)
	}

	got := mailFromRow(*row)

	return &got, nil
}

// Claim marks up to limit of an agent's waiting mail as read and returns it, oldest first.
func (s *Store) Claim(ctx context.Context, toAgentID string, limit int32) ([]Mail, error) {
	if !store.IsID(toAgentID) {
		return nil, nil
	}

	rows, err := s.queries.ClaimMail(ctx, models.ClaimMailParams{
		ToAgentID: toAgentID,
		Lim:       limit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to claim the mail for agent %q: %w", toAgentID, err)
	}

	claimed := xslice.Map(rows, func(row *models.Mail) Mail {
		return mailFromRow(*row)
	})

	slices.SortFunc(claimed, func(a, b Mail) int {
		return strings.Compare(a.ID, b.ID)
	})

	return claimed, nil
}

// ListPage returns one page of the mail addressed to an agent, newest first, and never marks
// anything as read.
func (s *Store) ListPage(ctx context.Context, toAgentID string, limit int32, cursor *Cursor) ([]Mail, error) {
	if !store.IsID(toAgentID) {
		return nil, nil
	}

	params := models.ListMailPageParams{ToAgentID: toAgentID, Lim: limit}
	if cursor != nil {
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListMailPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of mail for agent %q: %w", toAgentID, err)
	}

	return xslice.Map(rows, func(row *models.Mail) Mail {
		return mailFromRow(*row)
	}), nil
}

// ListSentPage returns one page of the mail an agent sent, newest first.
func (s *Store) ListSentPage(ctx context.Context, fromAgentID string, limit int32, cursor *Cursor) ([]Mail, error) {
	if !store.IsID(fromAgentID) {
		return nil, nil
	}

	params := models.ListMailSentPageParams{FromAgentID: &fromAgentID, Lim: limit}
	if cursor != nil {
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListMailSentPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of the mail agent %q sent: %w", fromAgentID, err)
	}

	return xslice.Map(rows, func(row *models.Mail) Mail {
		return mailFromRow(*row)
	}), nil
}

// ListByID returns whichever of the given ids hold mail, so a caller checking a batch for
// completeness compares the length against what it asked for.
func (s *Store) ListByID(ctx context.Context, ids []string) ([]Mail, error) {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListMailByID(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to get %d mail: %w", len(storable), err)
	}

	return xslice.Map(rows, func(row *models.Mail) Mail {
		return mailFromRow(*row)
	}), nil
}

// Delete permanently removes mail by id without checking that every id holds any, so a
// caller needing the batch to go whole checks with ListByID first.
func (s *Store) Delete(ctx context.Context, ids []string) error {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.DeleteMailByID(ctx, storable); err != nil {
		return fmt.Errorf("failed to delete %d mail: %w", len(storable), err)
	}

	return nil
}

// CountExchanged is how much plain mail two agents have written to each other, in either
// direction and however many threads it was spread over, leaving out the notices weave raised
// itself since neither agent sent those.
func (s *Store) CountExchanged(ctx context.Context, one, other string) (int64, error) {
	if !store.IsID(one) || !store.IsID(other) {
		return 0, nil
	}

	count, err := s.queries.CountMailExchangedBetweenAgents(ctx, models.CountMailExchangedBetweenAgentsParams{
		One:   one,
		Other: other,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to count the mail agents %q and %q have exchanged: %w", one, other, err)
	}

	return count, nil
}

// CountPending returns how much unclaimed mail is waiting for an agent.
func (s *Store) CountPending(ctx context.Context, toAgentID string) (int64, error) {
	if !store.IsID(toAgentID) {
		return 0, nil
	}

	count, err := s.queries.CountPendingMail(ctx, toAgentID)
	if err != nil {
		return 0, fmt.Errorf("failed to count the mail waiting for agent %q: %w", toAgentID, err)
	}

	return count, nil
}

// ListPendingMailboxes is every agent holding unclaimed mail, live or not yet reported as
// expired, so the sweep finds a mailbox whose delivery job is gone as well as one whose mail
// timed out unseen.
func (s *Store) ListPendingMailboxes(ctx context.Context) ([]string, error) {
	rows, err := s.queries.ListMailboxesWithPendingMail(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the mailboxes holding mail: %w", err)
	}

	return rows, nil
}

// ListUnreadMailboxes is every agent holding mail it could claim right now, so a mailbox
// whose only mail has timed out drops off it.
func (s *Store) ListUnreadMailboxes(ctx context.Context) ([]string, error) {
	rows, err := s.queries.ListMailboxesWithUnreadMail(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the mailboxes holding unread mail: %w", err)
	}

	return rows, nil
}

// ListExpired reports the mail that ran out of time before the recipient claimed it, and marks
// it reported so the next call does not hand back the same mail again.
func (s *Store) ListExpired(ctx context.Context, toAgentID string) ([]Mail, error) {
	if !store.IsID(toAgentID) {
		return nil, nil
	}

	rows, err := s.queries.ReportExpiredMail(ctx, toAgentID)
	if err != nil {
		return nil, fmt.Errorf("failed to report the expired mail for agent %q: %w", toAgentID, err)
	}

	return xslice.Map(rows, func(row *models.Mail) Mail {
		return mailFromRow(*row)
	}), nil
}

func keepStorableIDs(ids []string) []string {
	return xslice.Filter(ids, store.IsID)
}

func mailFromRow(row models.Mail) Mail {
	return Mail{
		ID:           row.ID,
		FromAgentID:  row.FromAgentID,
		FromName:     row.FromName,
		ToAgentID:    row.ToAgentID,
		Body:         row.Body,
		Kind:         Kind(row.Kind),
		ReplyTo:      row.ReplyTo,
		AboutAgentID: row.AboutAgentID,
		ClaimedAt:    row.ClaimedAt,
		ExpiresAt:    row.ExpiresAt,
		CreatedAt:    row.CreatedAt,
	}
}
