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

type Cursor struct {
	ID string `json:"i"`
}

// Validate checks that the cursor contains a mail ID.
func (c Cursor) Validate() error {
	if !store.IsID(c.ID) {
		return fmt.Errorf("cursor id %q is not an id", c.ID)
	}

	return nil
}

// Kind is what a piece of mail is. Everything an agent sends is KindPlain; a kind other
// than that marks a notice weave raises itself, of which an agent holds at most one
// unclaimed at a time.
type Kind string

const (
	// KindPlain is ordinary mail, never deduplicated.
	KindPlain Kind = Kind(models.MailKindPlain)
	// KindReviewActivity says the agent's diff has review it has not read.
	KindReviewActivity Kind = Kind(models.MailKindReviewActivity)
)

type Mail struct {
	ID          string
	FromAgentID *string
	FromName    string
	ToAgentID   string
	Body        string
	Kind        Kind
	ReplyTo     *string
	Hops        int32
	ClaimedAt   *time.Time
	ExpiresAt   time.Time
	CreatedAt   time.Time
}

type Store struct {
	queries *models.Queries
}

func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

type SendParams struct {
	FromAgentID *string
	FromName    string
	ToAgentID   string
	Body        string
	// Kind decides whether a second copy can queue up behind the first. A kind other than
	// KindPlain is refused while one of the same kind is still unclaimed, since a notice
	// waiting to be read already says what another copy of it would say.
	Kind    Kind
	ReplyTo *string
	Hops    int32
	// How long the mail stays deliverable, counted from the database's clock rather than
	// the caller's, since every read of expires_at compares it against NOW().
	Deadline time.Duration
}

// Send writes one piece of mail. It answers nil when the recipient is already holding an
// unclaimed notice of this kind, which is not a failure: the thing that would have been
// said is already waiting to be read.
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
		Hops:           params.Hops,
		DeadlineMicros: params.Deadline.Microseconds(),
	})
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to send mail to agent %q: %w", params.ToAgentID, err)
	}

	sent := mailFromRow(*row)

	return &sent, nil
}

func (s *Store) GetByID(ctx context.Context, id string) (*Mail, error) {
	if !store.IsID(id) {
		return nil, nil
	}

	row, err := s.queries.GetMail(ctx, id)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get mail %q: %w", id, err)
	}

	got := mailFromRow(*row)

	return &got, nil
}

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

// ListPage returns one page of the mail addressed to an agent, newest first, whether or
// not it has been claimed. Unlike Claim, this never marks anything as read.
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

// ListByID returns whichever of the given ids hold mail, in no particular order. A caller
// checking a batch for completeness compares its length against the ids it asked for.
func (s *Store) ListByID(ctx context.Context, ids []string) ([]Mail, error) {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.GetMailByID(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to get %d mail: %w", len(storable), err)
	}

	return xslice.Map(rows, func(row *models.Mail) Mail {
		return mailFromRow(*row)
	}), nil
}

// Delete permanently removes mail by id. It does not check that every id given holds
// mail; a caller that needs the batch to go whole or not at all checks with ListByID first.
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
// expired, so the sweep finds a mailbox whose own delivery job is gone as well as one whose
// only mail timed out unseen. Deliver tells the two cases apart itself via CountPending, which
// excludes expired mail. Once ListExpired has reported a mailbox's expired mail, it drops out
// of this list on its own.
func (s *Store) ListPendingMailboxes(ctx context.Context) ([]string, error) {
	rows, err := s.queries.ListMailboxesWithPendingMail(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the mailboxes holding mail: %w", err)
	}

	return rows, nil
}

// ListUnreadMailboxes is every agent holding mail it could claim right now: unclaimed
// and not yet expired. Unlike ListPendingMailboxes, a mailbox whose only mail has timed
// out drops off this list, since read_mail could not hand that mail back either.
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
		ID:          row.ID,
		FromAgentID: row.FromAgentID,
		FromName:    row.FromName,
		ToAgentID:   row.ToAgentID,
		Body:        row.Body,
		Kind:        Kind(row.Kind),
		ReplyTo:     row.ReplyTo,
		Hops:        row.Hops,
		ClaimedAt:   row.ClaimedAt,
		ExpiresAt:   row.ExpiresAt,
		CreatedAt:   row.CreatedAt,
	}
}
