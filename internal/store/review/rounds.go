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

// Verdict is what a reviewer concluded about a round.
type Verdict string

const (
	// VerdictApproved is the reviewer saying this round is good as it stands.
	VerdictApproved Verdict = Verdict(models.ReviewVerdictApproved)
	// VerdictChangesRequested is the reviewer saying the agent should keep working.
	VerdictChangesRequested Verdict = Verdict(models.ReviewVerdictChangesRequested)
	// VerdictCommented is notes without a conclusion either way.
	VerdictCommented Verdict = Verdict(models.ReviewVerdictCommented)
)

// RoundRef is the ref a round's snapshot is held by.
func RoundRef(agentID, snapshotSHA string) string {
	return "refs/weave/rounds/" + agentID + "/" + snapshotSHA
}

// Round is one frozen snapshot of what an agent has written, which every review and comment
// is anchored to.
type Round struct {
	ID          string
	AgentID     string
	Ordinal     int32
	BaseSHA     string
	SnapshotSHA string
	CreatedAt   time.Time
}

// Review is one reviewer's batch of comments on one round, released all at once.
type Review struct {
	ID            string
	AgentID       string
	RoundID       string
	RoundOrdinal  int32
	AuthorKind    AuthorKind
	AuthorAgentID *string

	// Verdict is what the reviewer concluded, nil while the review is still open.
	Verdict *Verdict
	// ReleasedAt is when the review was released, nil while nothing in it has reached the agent.
	ReleasedAt *time.Time
	// Body is what the reviewer said about the change as a whole rather than about any one line,
	// empty when they said nothing.
	Body string
	// Path is the absolute path the released review was written to, empty while it is still open.
	Path           string
	DoorbellMailID *string
	// DeliveredAt is when the agent claimed the mail that told it the review was waiting.
	DeliveredAt *time.Time
	// OpenThreads is how many of this review's conversations nobody has settled.
	OpenThreads int32
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// IsOpen reports whether the review is still being written.
func (r Review) IsOpen() bool {
	return r.ReleasedAt == nil
}

// IsSettled reports whether a released review has nothing left owed an answer.
func (r Review) IsSettled() bool {
	return !r.IsOpen() && r.OpenThreads == 0
}

type CreateRoundParams struct {
	AgentID     string
	BaseSHA     string
	SnapshotSHA string
}

// CreateRound records a snapshot as the next round of an agent, numbered after the rounds
// already taken.
func (s *ReviewStore) CreateRound(ctx context.Context, params CreateRoundParams) (*Round, error) {
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
func (s *ReviewStore) GetRound(ctx context.Context, id string) (*Round, error) {
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

// GetLatestRound is the newest round taken of an agent, ErrNotFound when none has been.
func (s *ReviewStore) GetLatestRound(ctx context.Context, agentID string) (*Round, error) {
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

// GetRoundBySnapshot is the round an agent's snapshot was frozen as, ErrNotFound when no round
// holds that snapshot.
func (s *ReviewStore) GetRoundBySnapshot(ctx context.Context, agentID, snapshotSHA string) (*Round, error) {
	if !store.IsID(agentID) || snapshotSHA == "" {
		return nil, roundNotFound(agentID)
	}

	row, err := s.queries.GetReviewRoundBySnapshot(ctx, models.GetReviewRoundBySnapshotParams{
		AgentID:     agentID,
		SnapshotSha: snapshotSHA,
	})
	if db.IsNoRows(err) {
		return nil, roundNotFound(snapshotSHA)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the review round of agent %q holding %q: %w", agentID, snapshotSHA, err)
	}

	round := roundFromRow(*row)

	return &round, nil
}

// ListRounds is every round of an agent, oldest first.
func (s *ReviewStore) ListRounds(ctx context.Context, agentID string) ([]Round, error) {
	if !store.IsID(agentID) {
		return nil, nil
	}

	rows, err := s.queries.ListReviewRounds(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the review rounds of agent %q: %w", agentID, err)
	}

	rounds := xslice.Map(rows, func(row *models.ReviewRound) Round {
		return roundFromRow(*row)
	})

	return rounds, nil
}

type OpenReviewParams struct {
	AgentID       string
	RoundID       string
	AuthorKind    AuthorKind
	AuthorAgentID *string
}

// OpenReview starts a reviewer's batch against a round, which a unique index allows only one
// of at a time.
func (s *ReviewStore) OpenReview(ctx context.Context, params OpenReviewParams) (*Review, error) {
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
func (s *ReviewStore) GetReview(ctx context.Context, id string) (*Review, error) {
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

// OpenReviewOf is the review a reviewer is still writing against an agent, ErrNotFound when
// they have none open.
func (s *ReviewStore) OpenReviewOf(ctx context.Context, params AuthorParams) (*Review, error) {
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

// GetLastReleasedReview is the newest review a reviewer has released against an agent,
// ErrNotFound when they have released none.
func (s *ReviewStore) GetLastReleasedReview(ctx context.Context, params AuthorParams) (*Review, error) {
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

	review := reviewFromRow(row.Review)
	review.OpenThreads = row.OpenThreads

	return &review, nil
}

// ListReviews is every review of an agent, in the order their rounds were taken.
func (s *ReviewStore) ListReviews(ctx context.Context, agentID string) ([]Review, error) {
	if !store.IsID(agentID) {
		return nil, nil
	}

	rows, err := s.queries.ListReviews(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the reviews of agent %q: %w", agentID, err)
	}

	reviews := make([]Review, 0, len(rows))
	for _, row := range rows {
		review := reviewFromRow(row.Review)
		review.RoundOrdinal = row.RoundOrdinal
		review.DeliveredAt = row.DeliveredAt
		review.OpenThreads = row.OpenThreads

		reviews = append(reviews, review)
	}

	return reviews, nil
}

// CountOpenComments is how many comments an open review is holding.
func (s *ReviewStore) CountOpenComments(ctx context.Context, reviewID string) (int, error) {
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
	// Body is what the reviewer said about the change as a whole, empty when they said nothing.
	Body           string
	Path           string
	DoorbellMailID *string
}

// ReleaseReview releases a review, stamping its verdict, the file it was written to and the
// mail that told the agent it was there.
func (s *ReviewStore) ReleaseReview(ctx context.Context, params ReleaseReviewParams) (*Review, error) {
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

// DeleteOpenReviewsBy removes the unreleased reviews these reviewers were still writing.
func (s *ReviewStore) DeleteOpenReviewsBy(ctx context.Context, authorAgentIDs []string) error {
	storable := xslice.Filter(authorAgentIDs, store.IsID)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.DeleteOpenReviewsByAuthor(ctx, storable); err != nil {
		return fmt.Errorf("failed to delete the open reviews of %d agent(s): %w", len(storable), err)
	}

	return nil
}

// DeleteReview removes a review and everything pinned under it.
func (s *ReviewStore) DeleteReview(ctx context.Context, id string) error {
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

// Tally is where one agent's work stands with the reviews released of it, counted against the
// agent holding the diff rather than whoever read it.
type Tally struct {
	AgentID string
	Rounds  int32
	// Verdict is the last conclusion released about the agent's work, nil when every released
	// review only commented.
	Verdict *Verdict
	// SnapshotSHA is the round that conclusion was given on, empty when there is none.
	SnapshotSHA string
}

// ListTallies is where every agent's work stands, leaving out the agents no review has been
// released of.
func (s *ReviewStore) ListTallies(ctx context.Context) ([]Tally, error) {
	rows, err := s.queries.ListReviewTallies(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to count the rounds of review: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListReviewTalliesRow) Tally {
		tally := Tally{AgentID: row.AgentID, Rounds: row.Rounds}
		if row.Verdict.Valid {
			verdict := Verdict(row.Verdict.ReviewVerdict)
			tally.Verdict = &verdict
		}
		if row.SnapshotSha != nil {
			tally.SnapshotSHA = *row.SnapshotSha
		}

		return tally
	}), nil
}

// NewestRound is the snapshot of an agent's newest review round.
type NewestRound struct {
	AgentID     string
	SnapshotSHA string
}

// ListNewestRounds is the newest round of every agent that has one.
func (s *ReviewStore) ListNewestRounds(ctx context.Context) ([]NewestRound, error) {
	rows, err := s.queries.ListNewestReviewRounds(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the newest review rounds: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListNewestReviewRoundsRow) NewestRound {
		return NewestRound{AgentID: row.AgentID, SnapshotSHA: row.SnapshotSha}
	}), nil
}

// LiveReviewer is a reviewer still owed an answer about the work of the agent it is attached to.
type LiveReviewer struct {
	AgentID    string
	ReviewerID string
}

// ListLiveReviewers is every unarchived reviewer child whose last released verdict on its
// parent's work was not an approval.
func (s *ReviewStore) ListLiveReviewers(ctx context.Context) ([]LiveReviewer, error) {
	rows, err := s.queries.ListLiveReviewers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the reviewers still reading: %w", err)
	}

	live := make([]LiveReviewer, 0, len(rows))
	for _, row := range rows {
		if row.AgentID == nil {
			continue
		}
		live = append(live, LiveReviewer{AgentID: *row.AgentID, ReviewerID: row.ReviewerID})
	}

	return live, nil
}

// GetLiveReviewerOf is the reviewer still owed an answer about one agent's work, ErrNotFound when
// it has none.
func (s *ReviewStore) GetLiveReviewerOf(ctx context.Context, agentID string) (*LiveReviewer, error) {
	live, err := s.ListLiveReviewers(ctx)
	if err != nil {
		return nil, err
	}

	for _, reading := range live {
		if reading.AgentID == agentID {
			return &reading, nil
		}
	}

	return nil, fmt.Errorf("%w: no reviewer is reading %q", store.ErrNotFound, agentID)
}
