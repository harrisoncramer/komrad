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

type Mail struct {
	ID          string
	FromAgentID *string
	FromName    string
	ToAgentID   string
	Body        string
	ReplyTo     *string
	Hops        int32
	// Kind is set on the notices weave sends itself and only wants one of waiting at a time,
	// such as the review doorbell, and unset on the mail agents write to each other.
	Kind      *string
	ClaimedAt *time.Time
	ExpiresAt time.Time
	CreatedAt time.Time
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
	ReplyTo     *string
	Hops        int32
	// How long the mail stays deliverable, counted from the database's clock rather than
	// the caller's, since every read of expires_at compares it against NOW().
	Deadline time.Duration
}

func (s *Store) Send(ctx context.Context, params SendParams) (*Mail, error) {
	row, err := s.queries.InsertMail(ctx, models.InsertMailParams{
		FromAgentID:    params.FromAgentID,
		FromName:       params.FromName,
		ToAgentID:      params.ToAgentID,
		Body:           params.Body,
		ReplyTo:        params.ReplyTo,
		Hops:           params.Hops,
		DeadlineMicros: params.Deadline.Microseconds(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to send mail to agent %q: %w", params.ToAgentID, err)
	}

	sent := mailFromRow(*row)

	return &sent, nil
}

type SendNoticeParams struct {
	FromName  string
	ToAgentID string
	Body      string
	// Kind is what makes the notice one of a kind: an agent holds at most one unclaimed
	// notice of each, so the tenth edit to a review does not become the tenth doorbell.
	Kind string
	// Deadline is how long the notice stays deliverable, counted from the database's clock.
	Deadline time.Duration
}

// SendNotice puts one notice of a kind in an agent's mailbox, and answers nil when the agent
// is already holding an unclaimed one. The uniqueness is the partial index's rather than a
// read followed by a write, so two callers racing produce one notice instead of two.
func (s *Store) SendNotice(ctx context.Context, params SendNoticeParams) (*Mail, error) {
	if !store.IsID(params.ToAgentID) {
		return nil, nil
	}

	row, err := s.queries.InsertMailNotice(ctx, models.InsertMailNoticeParams{
		FromName:       params.FromName,
		ToAgentID:      params.ToAgentID,
		Body:           params.Body,
		Kind:           &params.Kind,
		DeadlineMicros: params.Deadline.Microseconds(),
	})
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to send a %q notice to agent %q: %w", params.Kind, params.ToAgentID, err)
	}

	sent := mailFromRow(*row)

	return &sent, nil
}

// DeleteExpiredNotices clears the notices of a kind an agent never claimed and can no longer
// be delivered, which is what lets the next one be created: an expired doorbell still holds
// the one unclaimed slot its kind is allowed.
func (s *Store) DeleteExpiredNotices(ctx context.Context, toAgentID, kind string) error {
	if !store.IsID(toAgentID) {
		return nil
	}

	if err := s.queries.DeleteExpiredMailNotices(ctx, models.DeleteExpiredMailNoticesParams{
		ToAgentID: toAgentID,
		Kind:      &kind,
	}); err != nil {
		return fmt.Errorf("failed to clear the expired %q notices of agent %q: %w", kind, toAgentID, err)
	}

	return nil
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
		ReplyTo:     row.ReplyTo,
		Hops:        row.Hops,
		Kind:        row.Kind,
		ClaimedAt:   row.ClaimedAt,
		ExpiresAt:   row.ExpiresAt,
		CreatedAt:   row.CreatedAt,
	}
}
