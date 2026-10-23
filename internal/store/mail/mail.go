// Package mail stores the messages agents send each other and which of them the recipient has
// claimed.
package mail

import (
	"context"
	"fmt"
	"slices"
	"time"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/protoenum"
	eventv1 "komrad/proto/gen/event/v1"

	"github.com/google/uuid"
)

// Cursor pages this listing, which resumes from one row's id.
type Cursor = store.IDCursor

// Kind separates mail an agent sent, always KindPlain, from mail komrad wrote itself.
type Kind string

const (
	// KindPlain is ordinary mail.
	KindPlain Kind = Kind(models.MailKindPlain)
	// KindReviewActivity tells an agent that a review of its diff is waiting.
	KindReviewActivity Kind = Kind(models.MailKindReviewActivity)
	// KindExchangeCapped tells an agent that mail written to it was refused because the pair had
	// reached the cap on how much they may exchange.
	KindExchangeCapped Kind = Kind(models.MailKindExchangeCapped)
	// KindDocumentEdited tells an agent that a person wrote or edited one of its documents in the panel.
	KindDocumentEdited Kind = Kind(models.MailKindDocumentEdited)
	// KindPullRequestChanged tells an agent that a pull request on its branch changed at the forge.
	KindPullRequestChanged Kind = Kind(models.MailKindPullRequestChanged)
	// KindWorktreeRebased tells an agent that komrad rebased its worktree, or tried to.
	KindWorktreeRebased Kind = Kind(models.MailKindWorktreeRebased)
	// KindWorktreePushed tells an agent that its branch was force-pushed to origin from the panel.
	KindWorktreePushed Kind = Kind(models.MailKindWorktreePushed)
	// KindWorktreeAborted tells an agent that the rebase, merge or pick part-way through its worktree was aborted.
	KindWorktreeAborted Kind = Kind(models.MailKindWorktreeAborted)
	// KindReviewComment carries a person's comment on the recipient's work.
	KindReviewComment Kind = Kind(models.MailKindReviewComment)
)

type Mail struct {
	ID          uuid.UUID
	FromAgentID *uuid.UUID
	FromName    string
	ToAgentID   uuid.UUID
	Body        string
	Kind        Kind
	ReplyTo     *uuid.UUID
	// SubjectID is the thing mail from komrad is about, unset on ordinary mail.
	SubjectID *uuid.UUID
	// Events are the events mail from komrad named, whose prompts its body carries.
	Events    []eventv1.Event
	ClaimedAt *time.Time
	ExpiresAt time.Time
	CreatedAt time.Time
}

type MailStore struct {
	queries *models.Queries
}

// NewMailStore builds the mail store over a database handle or a transaction.
func NewMailStore(db store.DBTX) *MailStore {
	return &MailStore{queries: models.New(db)}
}

type SendParams struct {
	FromAgentID    *uuid.UUID
	FromName       string
	ToAgentID      uuid.UUID
	Body           string
	Kind           Kind
	ReplyTo        *uuid.UUID
	IdempotencyKey *string
	// Deadline is how long the mail stays deliverable, counted from the database's clock rather than
	// the caller's.
	Deadline time.Duration
}

// Send writes one piece of mail.
func (s *MailStore) Send(ctx context.Context, params SendParams) (*Mail, error) {
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
		IdempotencyKey: params.IdempotencyKey,
		DeadlineMicros: params.Deadline.Microseconds(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to send mail to agent %q: %w", params.ToAgentID, err)
	}

	return fromRow(row)
}

type UpsertFromKomradParams struct {
	FromName  string
	ToAgentID uuid.UUID
	Body      string
	Kind      Kind
	SubjectID uuid.UUID
	Events    []eventv1.Event
	Deadline  time.Duration
}

// UpsertFromKomrad writes mail from komrad, rewriting the recipient's unread mail of the
// same kind about the same subject when it holds one.
func (s *MailStore) UpsertFromKomrad(ctx context.Context, params UpsertFromKomradParams) (*Mail, error) {
	row, err := s.queries.UpsertFromKomrad(ctx, models.UpsertFromKomradParams{
		FromName:       params.FromName,
		ToAgentID:      params.ToAgentID,
		Body:           params.Body,
		Kind:           models.MailKind(params.Kind),
		SubjectID:      params.SubjectID,
		Events:         protoenum.Names(params.Events),
		DeadlineMicros: params.Deadline.Microseconds(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to write %q mail for agent %q: %w", params.Kind, params.ToAgentID, err)
	}

	return fromRow(row)
}

// GetUnreadAbout is the unread mail of a kind about a subject that its recipient holds,
// expired or not, ErrNotFound when it holds none.
func (s *MailStore) GetUnreadAbout(ctx context.Context, toAgentID uuid.UUID, kind Kind, subjectID uuid.UUID) (*Mail, error) {
	row, err := s.queries.GetUnreadAbout(ctx, models.GetUnreadAboutParams{
		ToAgentID: toAgentID,
		Kind:      models.MailKind(kind),
		SubjectID: subjectID,
	})
	if db.IsNoRows(err) {
		return nil, noUnreadAbout(toAgentID, kind)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the unread %q mail of agent %q: %w", kind, toAgentID, err)
	}

	return fromRow(row)
}

// ExistsReadAbout says whether the recipient has read any mail of a kind about a subject.
func (s *MailStore) ExistsReadAbout(ctx context.Context, toAgentID uuid.UUID, kind Kind, subjectID uuid.UUID) (bool, error) {
	read, err := s.queries.ExistsReadAbout(ctx, models.ExistsReadAboutParams{
		ToAgentID: toAgentID,
		Kind:      models.MailKind(kind),
		SubjectID: subjectID,
	})
	if err != nil {
		return false, fmt.Errorf("failed to read whether agent %q read its %q mail: %w", toAgentID, kind, err)
	}

	return read, nil
}

func noUnreadAbout(toAgentID uuid.UUID, kind Kind) error {
	return fmt.Errorf("%w: unread %q mail for agent %q", store.ErrNotFound, kind, toAgentID)
}

// GetByIdempotencyKey is the mail a send with that key already wrote, ErrNotFound when none did.
func (s *MailStore) GetByIdempotencyKey(ctx context.Context, key string) (*Mail, error) {
	row, err := s.queries.GetMailByIdempotencyKey(ctx, &key)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: mail for idempotency key %q", store.ErrNotFound, key)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the mail for idempotency key %q: %w", key, err)
	}

	return fromRow(row)
}

// GetByID returns one piece of mail, claimed or not.
func (s *MailStore) GetByID(ctx context.Context, id uuid.UUID) (*Mail, error) {
	row, err := s.queries.GetMail(ctx, id)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: mail %q", store.ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get mail %q: %w", id, err)
	}

	return fromRow(row)
}

// ListWaiting lists up to limit of an agent's waiting mail, oldest first, without claiming it.
func (s *MailStore) ListWaiting(ctx context.Context, toAgentID uuid.UUID, limit int32) ([]Mail, error) {
	rows, err := s.queries.ListWaitingMail(ctx, models.ListWaitingMailParams{
		ToAgentID: toAgentID,
		Lim:       limit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list the mail waiting for agent %q: %w", toAgentID, err)
	}

	return fromRows(rows)
}

// ClaimByID marks the given mail waiting for an agent as read and returns it, oldest first,
// leaving out any already claimed or expired.
func (s *MailStore) ClaimByID(ctx context.Context, toAgentID uuid.UUID, ids []uuid.UUID) ([]Mail, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ClaimMailByID(ctx, models.ClaimMailByIDParams{
		ToAgentID: toAgentID,
		Ids:       ids,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to claim the mail for agent %q: %w", toAgentID, err)
	}

	claimed, err := fromRows(rows)
	if err != nil {
		return nil, err
	}

	slices.SortFunc(claimed, func(a, b Mail) int {
		return store.CompareIDs(a.ID, b.ID)
	})

	return claimed, nil
}

// ListPage returns one page of the mail addressed to an agent, newest first, and never marks
// anything as read.
func (s *MailStore) ListPage(ctx context.Context, toAgentID uuid.UUID, limit int32, cursor *Cursor) ([]Mail, error) {
	params := models.ListMailPageParams{ToAgentID: toAgentID, Lim: limit}
	if cursor != nil {
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListMailPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of mail for agent %q: %w", toAgentID, err)
	}

	return fromRows(rows)
}

// ListSentPage returns one page of the mail an agent sent, newest first.
func (s *MailStore) ListSentPage(ctx context.Context, fromAgentID uuid.UUID, limit int32, cursor *Cursor) ([]Mail, error) {
	params := models.ListMailSentPageParams{FromAgentID: &fromAgentID, Lim: limit}
	if cursor != nil {
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListMailSentPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of the mail agent %q sent: %w", fromAgentID, err)
	}

	return fromRows(rows)
}

// ListByID returns whichever of the given ids hold mail.
func (s *MailStore) ListByID(ctx context.Context, ids []uuid.UUID) ([]Mail, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListMailByID(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to get %d mail: %w", len(ids), err)
	}

	return fromRows(rows)
}

// Delete permanently removes mail by id without checking that every id holds any.
func (s *MailStore) Delete(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}

	if err := s.queries.DeleteMailByID(ctx, ids); err != nil {
		return fmt.Errorf("failed to delete %d mail: %w", len(ids), err)
	}

	return nil
}

// CountExchanged is how much plain mail two agents have written to each other, in either
// direction, leaving out the mail komrad wrote itself.
func (s *MailStore) CountExchanged(ctx context.Context, one, other uuid.UUID) (int64, error) {
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
func (s *MailStore) CountPending(ctx context.Context, toAgentID uuid.UUID) (int64, error) {
	count, err := s.queries.CountPendingMail(ctx, toAgentID)
	if err != nil {
		return 0, fmt.Errorf("failed to count the mail waiting for agent %q: %w", toAgentID, err)
	}

	return count, nil
}

// ClaimNudge takes the right to tell an agent about its unread mail, which only mail it has not
// been told about gives.
func (s *MailStore) ClaimNudge(ctx context.Context, toAgentID uuid.UUID) (bool, error) {
	claimed, err := s.queries.ClaimMailNudge(ctx, toAgentID)
	if err != nil {
		return false, fmt.Errorf("failed to claim the nudge for agent %q: %w", toAgentID, err)
	}

	return len(claimed) > 0, nil
}

// ClearNudge gives back a claim whose nudge reached nothing, so the next pass tries again.
func (s *MailStore) ClearNudge(ctx context.Context, toAgentID uuid.UUID) error {
	if err := s.queries.ClearMailNudge(ctx, toAgentID); err != nil {
		return fmt.Errorf("failed to clear the nudge claim for agent %q: %w", toAgentID, err)
	}

	return nil
}

// ListPendingMailboxes is every unarchived agent holding unclaimed mail, whether that mail is
// still live or has expired without being reported.
func (s *MailStore) ListPendingMailboxes(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.queries.ListMailboxesWithPendingMail(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the mailboxes holding mail: %w", err)
	}

	return rows, nil
}

// ListExpired reports the mail that ran out of time before the recipient claimed it, and marks
// it reported so the next call does not hand back the same mail.
func (s *MailStore) ListExpired(ctx context.Context, toAgentID uuid.UUID) ([]Mail, error) {
	rows, err := s.queries.ReportExpiredMail(ctx, toAgentID)
	if err != nil {
		return nil, fmt.Errorf("failed to report the expired mail for agent %q: %w", toAgentID, err)
	}

	return fromRows(rows)
}

func fromRows(rows []*models.Mail) ([]Mail, error) {
	mail := make([]Mail, len(rows))
	for i, row := range rows {
		read, err := fromRow(row)
		if err != nil {
			return nil, err
		}
		mail[i] = *read
	}

	return mail, nil
}

func fromRow(row *models.Mail) (*Mail, error) {
	events, err := protoenum.ParseAll[eventv1.Event](row.Events)
	if err != nil {
		return nil, fmt.Errorf("failed to read the events of mail %q: %w", row.ID, err)
	}

	return &Mail{
		ID:          row.ID,
		FromAgentID: row.FromAgentID,
		FromName:    row.FromName,
		ToAgentID:   row.ToAgentID,
		Body:        row.Body,
		Kind:        Kind(row.Kind),
		ReplyTo:     row.ReplyTo,
		SubjectID:   row.SubjectID,
		Events:      events,
		ClaimedAt:   row.ClaimedAt,
		ExpiresAt:   row.ExpiresAt,
		CreatedAt:   row.CreatedAt,
	}, nil
}
