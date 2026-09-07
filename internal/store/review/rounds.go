package review

import (
	"context"
	"fmt"
	"time"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"
)

// Verdict is what an author concluded about a round.
type Verdict string

const (
	// VerdictApproved is the author saying this round is good as it stands.
	VerdictApproved Verdict = Verdict(models.ReviewVerdictApproved)
	// VerdictChangesRequested is the author saying the agent should keep working.
	VerdictChangesRequested Verdict = Verdict(models.ReviewVerdictChangesRequested)
	// VerdictCommented is notes without a conclusion either way.
	VerdictCommented Verdict = Verdict(models.ReviewVerdictCommented)
)

// Round is one immutable snapshot of what an agent has written, which is what a review is
// given against and what a comment is anchored to.
type Round struct {
	ID          string
	AgentID     string
	Ordinal     int32
	BaseSHA     string
	SnapshotSHA string
	CreatedAt   time.Time
}

// Review is one author's batch of comments against one round, open while it is being written
// and released as a whole.
type Review struct {
	ID            string
	AgentID       string
	RoundID       string
	RoundOrdinal  int32
	AuthorKind    AuthorKind
	AuthorAgentID *string

	// Verdict is what the author concluded, nil while the review is still open.
	Verdict *Verdict
	// ReleasedAt is when the review was let go of, nil while nothing in it has reached the agent.
	ReleasedAt *time.Time
	// Body is what the author said about the work as a whole rather than about any one line,
	// empty when they said nothing.
	Body string
	// Path is the absolute path the released review was written to, empty while it is still open.
	Path           string
	DoorbellMailID *string
	// DeliveredAt is when the agent claimed the mail that told it the review was waiting.
	DeliveredAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// IsOpen reports whether the review is still being written.
func (r Review) IsOpen() bool { return r.ReleasedAt == nil }

type CreateRoundParams struct {
	AgentID     string
	BaseSHA     string
	SnapshotSHA string
}

// CreateRound records a snapshot as the next round of an agent, numbered after the rounds already
// taken.
func (s *Store) CreateRound(ctx context.Context, params CreateRoundParams) (*Round, error) {
	row, err := s.queries.InsertReviewRound(ctx, models.InsertReviewRoundParams{
		AgentID:     params.AgentID,
		BaseSha:     params.BaseSHA,
		SnapshotSha: params.SnapshotSHA,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open a review round on agent %q: %w", params.AgentID, err)
	}

	round := roundFromRow(*row)

	return &round, nil
}

// GetRound is one round, ErrNotFound for an id that names nothing.
func (s *Store) GetRound(ctx context.Context, id string) (*Round, error) {
	if !store.IsID(id) {
		return nil, roundNotFound(id)
	}

	row, err := s.queries.GetReviewRound(ctx, id)
	if db.IsNoRows(err) {
		return nil, roundNotFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the review round %q: %w", id, err)
	}

	round := roundFromRow(*row)

	return &round, nil
}

// LatestRound is the newest round taken of an agent, ErrNotFound when none has been.
func (s *Store) LatestRound(ctx context.Context, agentID string) (*Round, error) {
	if !store.IsID(agentID) {
		return nil, roundNotFound(agentID)
	}

	row, err := s.queries.GetLatestReviewRound(ctx, agentID)
	if db.IsNoRows(err) {
		return nil, roundNotFound(agentID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the newest review round of agent %q: %w", agentID, err)
	}

	round := roundFromRow(*row)

	return &round, nil
}

// ListRounds is every round of an agent, oldest first.
func (s *Store) ListRounds(ctx context.Context, agentID string) ([]Round, error) {
	if !store.IsID(agentID) {
		return nil, nil
	}

	rows, err := s.queries.ListReviewRounds(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the review rounds of agent %q: %w", agentID, err)
	}

	rounds := make([]Round, 0, len(rows))
	for _, row := range rows {
		rounds = append(rounds, roundFromRow(*row))
	}

	return rounds, nil
}

type OpenReviewParams struct {
	AgentID       string
	RoundID       string
	AuthorKind    AuthorKind
	AuthorAgentID *string
}

// OpenReview starts an author's batch against a round, which the one-open-review indexes refuse a
// second of, and that is what keeps an author reviewing one round at a time.
func (s *Store) OpenReview(ctx context.Context, params OpenReviewParams) (*Review, error) {
	row, err := s.queries.InsertReview(ctx, models.InsertReviewParams{
		AgentID:       params.AgentID,
		RoundID:       params.RoundID,
		AuthorKind:    models.ReviewAuthorKind(params.AuthorKind),
		AuthorAgentID: params.AuthorAgentID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open a review on agent %q: %w", params.AgentID, err)
	}

	review := reviewFromRow(*row)

	return &review, nil
}

// GetReview is one review, ErrNotFound for an id that names nothing.
func (s *Store) GetReview(ctx context.Context, id string) (*Review, error) {
	if !store.IsID(id) {
		return nil, reviewNotFound(id)
	}

	row, err := s.queries.GetReview(ctx, id)
	if db.IsNoRows(err) {
		return nil, reviewNotFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the review %q: %w", id, err)
	}

	review := reviewFromRow(*row)

	return &review, nil
}

type AuthorParams struct {
	AgentID       string
	AuthorKind    AuthorKind
	AuthorAgentID *string
}

// OpenReviewOf is the batch an author is still writing against an agent, ErrNotFound when they
// have none open.
func (s *Store) OpenReviewOf(ctx context.Context, params AuthorParams) (*Review, error) {
	if !store.IsID(params.AgentID) {
		return nil, reviewNotFound(params.AgentID)
	}

	row, err := s.queries.GetOpenReview(ctx, models.GetOpenReviewParams{
		AgentID:       params.AgentID,
		AuthorKind:    models.ReviewAuthorKind(params.AuthorKind),
		AuthorAgentID: params.AuthorAgentID,
	})
	if db.IsNoRows(err) {
		return nil, reviewNotFound(params.AgentID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the open review on agent %q: %w", params.AgentID, err)
	}

	review := reviewFromRow(*row)

	return &review, nil
}

// LastReleasedReviewOf is the newest batch an author has let go of against an agent, which is what
// says where they last looked, ErrNotFound when they have released none.
func (s *Store) LastReleasedReviewOf(ctx context.Context, params AuthorParams) (*Review, error) {
	if !store.IsID(params.AgentID) {
		return nil, reviewNotFound(params.AgentID)
	}

	row, err := s.queries.GetLastReleasedReview(ctx, models.GetLastReleasedReviewParams{
		AgentID:       params.AgentID,
		AuthorKind:    models.ReviewAuthorKind(params.AuthorKind),
		AuthorAgentID: params.AuthorAgentID,
	})
	if db.IsNoRows(err) {
		return nil, reviewNotFound(params.AgentID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the last released review on agent %q: %w", params.AgentID, err)
	}

	review := reviewFromRow(*row)

	return &review, nil
}

// ListReviews is every review of an agent, in the order their rounds were taken.
func (s *Store) ListReviews(ctx context.Context, agentID string) ([]Review, error) {
	if !store.IsID(agentID) {
		return nil, nil
	}

	rows, err := s.queries.ListReviews(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the reviews of agent %q: %w", agentID, err)
	}

	reviews := make([]Review, 0, len(rows))
	for _, row := range rows {
		review := reviewFromRow(models.Review{
			ID:             row.ID,
			AgentID:        row.AgentID,
			RoundID:        row.RoundID,
			AuthorKind:     row.AuthorKind,
			AuthorAgentID:  row.AuthorAgentID,
			Verdict:        row.Verdict,
			ReleasedAt:     row.ReleasedAt,
			Body:           row.Body,
			Path:           row.Path,
			DoorbellMailID: row.DoorbellMailID,
			CreatedAt:      row.CreatedAt,
			UpdatedAt:      row.UpdatedAt,
		})
		review.RoundOrdinal = row.RoundOrdinal
		review.DeliveredAt = row.DeliveredAt

		reviews = append(reviews, review)
	}

	return reviews, nil
}

// CountOpenComments is how many comments an open review is holding, which the caller reads before
// releasing so that a review with nothing in it rings nobody.
func (s *Store) CountOpenComments(ctx context.Context, reviewID string) (int, error) {
	if !store.IsID(reviewID) {
		return 0, nil
	}

	count, err := s.queries.CountOpenReviewComments(ctx, &reviewID)
	if err != nil {
		return 0, fmt.Errorf("failed to count the comments in the review %q: %w", reviewID, err)
	}

	return int(count), nil
}

type ReleaseReviewParams struct {
	ID      string
	Verdict Verdict
	// Body is what the author said about the work as a whole, empty when they said nothing.
	Body           string
	Path           string
	DoorbellMailID *string
}

// ReleaseReview lets a batch go, stamping the verdict it went out with, where it was written for
// the agent, and the mail that told the agent it was there.
func (s *Store) ReleaseReview(ctx context.Context, params ReleaseReviewParams) (*Review, error) {
	if !store.IsID(params.ID) {
		return nil, reviewNotFound(params.ID)
	}

	row, err := s.queries.ReleaseReview(ctx, models.ReleaseReviewParams{
		ID:             params.ID,
		Body:           params.Body,
		Verdict:        models.NullReviewVerdict{ReviewVerdict: models.ReviewVerdict(params.Verdict), Valid: params.Verdict != ""},
		Path:           params.Path,
		DoorbellMailID: params.DoorbellMailID,
	})
	if db.IsNoRows(err) {
		return nil, reviewNotFound(params.ID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to release the review %q: %w", params.ID, err)
	}

	review := reviewFromRow(*row)

	return &review, nil
}

// DeleteOpenReviewsBy removes the batches these authors were still writing, which is what an
// agent being deleted leaves behind: a draft nobody has ever seen, with no history in it, whose
// author is about to stop existing.
func (s *Store) DeleteOpenReviewsBy(ctx context.Context, authorAgentIDs []string) error {
	storable := xslice.Filter(authorAgentIDs, store.IsID)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.DeleteOpenReviewsByAuthor(ctx, storable); err != nil {
		return fmt.Errorf("failed to delete the open reviews of %d agent(s): %w", len(storable), err)
	}

	return nil
}

// DeleteReview removes a batch and everything pinned under it, which is what abandoning an open
// review does.
func (s *Store) DeleteReview(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.DeleteReview(ctx, id); err != nil {
		return fmt.Errorf("failed to delete the review %q: %w", id, err)
	}

	return nil
}

func roundFromRow(row models.ReviewRound) Round {
	return Round{
		ID:          row.ID,
		AgentID:     row.AgentID,
		Ordinal:     row.Ordinal,
		BaseSHA:     row.BaseSha,
		SnapshotSHA: row.SnapshotSha,
		CreatedAt:   row.CreatedAt,
	}
}

func reviewFromRow(row models.Review) Review {
	review := Review{
		ID:             row.ID,
		AgentID:        row.AgentID,
		RoundID:        row.RoundID,
		AuthorKind:     AuthorKind(row.AuthorKind),
		AuthorAgentID:  row.AuthorAgentID,
		ReleasedAt:     row.ReleasedAt,
		Body:           row.Body,
		Path:           row.Path,
		DoorbellMailID: row.DoorbellMailID,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}

	if row.Verdict.Valid {
		verdict := Verdict(row.Verdict.ReviewVerdict)
		review.Verdict = &verdict
	}

	return review
}

func roundNotFound(id string) error {
	return fmt.Errorf("%w: review round %q", store.ErrNotFound, id)
}

func reviewNotFound(id string) error {
	return fmt.Errorf("%w: review %q", store.ErrNotFound, id)
}

// Tally is how many rounds of review one agent has taken part in, counted in both directions
// since a loop shows on the agent being reviewed and on the one reviewing it.
type Tally struct {
	AgentID string
	// Received is how many of its own rounds an author has released a review against.
	Received int32
	// Sent is how many rounds of another agent's work it has released a review against.
	Sent int32
}

// ListTallies is the rounds of review every agent has taken part in, leaving out the agents
// that have taken part in none.
func (s *Store) ListTallies(ctx context.Context) ([]Tally, error) {
	rows, err := s.queries.ListReviewRoundTallies(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to count the rounds of review: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListReviewRoundTalliesRow) Tally {
		return Tally{AgentID: row.AgentID, Received: row.Received, Sent: row.Sent}
	}), nil
}
