package review

import (
	"context"
	"fmt"
	"time"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xslice"
)

// Verdict is what a reviewer concluded about a snapshot.
type Verdict string

const (
	// VerdictApproved is the reviewer saying this snapshot is good as it stands.
	VerdictApproved Verdict = Verdict(models.ReviewVerdictApproved)
	// VerdictChangesRequested is the reviewer saying the agent should keep working.
	VerdictChangesRequested Verdict = Verdict(models.ReviewVerdictChangesRequested)
	// VerdictCommented is notes without a conclusion either way.
	VerdictCommented Verdict = Verdict(models.ReviewVerdictCommented)
)

// SnapshotRef is the ref a snapshot's commit is held by.
func SnapshotRef(taskID, snapshotSHA string) string {
	return "refs/komrad/snapshots/" + taskID + "/" + snapshotSHA
}

// Snapshot is one frozen copy of a task's checkout, which every review and comment
// is anchored to.
type Snapshot struct {
	ID        string
	TaskID    string
	Ordinal   int32
	BaseSHA   string
	SHA       string
	CreatedAt time.Time
	// LandedAt is when the base branch was first seen holding this snapshot or a later one whole,
	// nil until then.
	LandedAt *time.Time
}

// Review is one reviewer's batch of comments on one snapshot, released all at once.
type Review struct {
	ID              string
	TaskID          string
	SnapshotID      string
	SnapshotOrdinal int32
	AuthorKind      AuthorKind
	AuthorAgentID   *string
	// ExternalLogin is the forge login of whoever wrote a review on the forge, nil for one written in komrad.
	ExternalLogin *string

	// SnapshotSHA is the commit the snapshot froze the checkout as, empty when it was not read.
	SnapshotSHA string
	// SnapshotLandedAt is when the base branch was first seen holding the snapshot whole, nil until then.
	SnapshotLandedAt *time.Time
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

// isOpen reports whether the review is still being written.
func (r Review) isOpen() bool {
	return r.ReleasedAt == nil
}

// isSettled reports whether a released review has nothing left owed an answer.
func (r Review) isSettled() bool {
	return !r.isOpen() && r.OpenThreads == 0
}

type CreateSnapshotParams struct {
	TaskID  string
	BaseSHA string
	SHA     string
}

// CreateSnapshot records the next snapshot of a task, numbered after the snapshots
// already taken.
func (s *ReviewStore) CreateSnapshot(ctx context.Context, params CreateSnapshotParams) (*Snapshot, error) {
	row, err := s.queries.InsertReviewSnapshot(ctx, models.InsertReviewSnapshotParams{
		TaskID:      params.TaskID,
		BaseSha:     params.BaseSHA,
		SnapshotSha: params.SHA,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open a review snapshot on task %q: %w", params.TaskID, err)
	}

	snapshot := snapshotFromRow(*row)

	return &snapshot, nil
}

// GetSnapshot is one snapshot, ErrNotFound for an id that names nothing.
func (s *ReviewStore) GetSnapshot(ctx context.Context, id string) (*Snapshot, error) {
	if !store.IsID(id) {
		return nil, snapshotNotFound(id)
	}

	row, err := s.queries.GetReviewSnapshot(ctx, id)
	if db.IsNoRows(err) {
		return nil, snapshotNotFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the review snapshot %q: %w", id, err)
	}

	snapshot := snapshotFromRow(*row)

	return &snapshot, nil
}

// GetLatestSnapshot is the newest snapshot taken of a task, ErrNotFound when none has been.
func (s *ReviewStore) GetLatestSnapshot(ctx context.Context, taskID string) (*Snapshot, error) {
	if !store.IsID(taskID) {
		return nil, snapshotNotFound(taskID)
	}

	row, err := s.queries.GetLatestReviewSnapshot(ctx, taskID)
	if db.IsNoRows(err) {
		return nil, snapshotNotFound(taskID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the newest review snapshot of task %q: %w", taskID, err)
	}

	snapshot := snapshotFromRow(*row)

	return &snapshot, nil
}

// GetSnapshotBySHA is the snapshot of a task saved as a commit, ErrNotFound when no snapshot
// holds that commit.
func (s *ReviewStore) GetSnapshotBySHA(ctx context.Context, taskID, snapshotSHA string) (*Snapshot, error) {
	if !store.IsID(taskID) || snapshotSHA == "" {
		return nil, snapshotNotFound(taskID)
	}

	row, err := s.queries.GetReviewSnapshotBySnapshot(ctx, models.GetReviewSnapshotBySnapshotParams{
		TaskID:      taskID,
		SnapshotSha: snapshotSHA,
	})
	if db.IsNoRows(err) {
		return nil, snapshotNotFound(snapshotSHA)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the review snapshot of task %q holding %q: %w", taskID, snapshotSHA, err)
	}

	snapshot := snapshotFromRow(*row)

	return &snapshot, nil
}

// ListSnapshots is every snapshot of a task, oldest first.
func (s *ReviewStore) ListSnapshots(ctx context.Context, taskID string) ([]Snapshot, error) {
	if !store.IsID(taskID) {
		return nil, nil
	}

	rows, err := s.queries.ListReviewSnapshots(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the review snapshots of task %q: %w", taskID, err)
	}

	snapshots := xslice.Map(rows, func(row *models.ReviewSnapshot) Snapshot {
		return snapshotFromRow(*row)
	})

	return snapshots, nil
}

type OpenReviewParams struct {
	TaskID        string
	SnapshotID    string
	AuthorKind    AuthorKind
	AuthorAgentID *string
}

// OpenReview starts a reviewer's batch against a snapshot, which a unique index allows only one
// of at a time.
func (s *ReviewStore) OpenReview(ctx context.Context, params OpenReviewParams) (*Review, error) {
	row, err := s.queries.InsertReview(ctx, models.InsertReviewParams{
		TaskID:        params.TaskID,
		SnapshotID:    params.SnapshotID,
		AuthorKind:    models.ReviewAuthorKind(params.AuthorKind),
		AuthorAgentID: params.AuthorAgentID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open a review on task %q: %w", params.TaskID, err)
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

type MoveOpenReviewParams struct {
	ID         string
	SnapshotID string
}

// MoveOpenReview files an unreleased review under another snapshot, ErrNotFound once it is released.
func (s *ReviewStore) MoveOpenReview(ctx context.Context, params MoveOpenReviewParams) (*Review, error) {
	if !store.IsID(params.ID) {
		return nil, reviewNotFound(params.ID)
	}

	row, err := s.queries.MoveOpenReview(ctx, models.MoveOpenReviewParams{
		ID:         params.ID,
		SnapshotID: params.SnapshotID,
	})
	if db.IsNoRows(err) {
		return nil, reviewNotFound(params.ID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to move the review %q: %w", params.ID, err)
	}

	review := reviewFromRow(*row)

	return &review, nil
}

type OpenReviewOfParams struct {
	TaskID        string
	AuthorKind    AuthorKind
	AuthorAgentID *string
}

// OpenReviewOf is the review a reviewer is still writing against a task, ErrNotFound when
// they have none open.
func (s *ReviewStore) OpenReviewOf(ctx context.Context, params OpenReviewOfParams) (*Review, error) {
	if !store.IsID(params.TaskID) {
		return nil, reviewNotFound(params.TaskID)
	}

	row, err := s.queries.GetOpenReview(ctx, models.GetOpenReviewParams{
		TaskID:        params.TaskID,
		AuthorKind:    models.ReviewAuthorKind(params.AuthorKind),
		AuthorAgentID: params.AuthorAgentID,
	})
	if db.IsNoRows(err) {
		return nil, reviewNotFound(params.TaskID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the open review on task %q: %w", params.TaskID, err)
	}

	review := reviewFromRow(*row)

	return &review, nil
}

// ListReviews is every review of a task, in the order their snapshots were taken.
func (s *ReviewStore) ListReviews(ctx context.Context, taskID string) ([]Review, error) {
	if !store.IsID(taskID) {
		return nil, nil
	}

	rows, err := s.queries.ListReviews(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the reviews of task %q: %w", taskID, err)
	}

	reviews := make([]Review, 0, len(rows))
	for _, row := range rows {
		review := reviewFromRow(row.Review)
		review.SnapshotOrdinal = row.SnapshotOrdinal
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

// ReleasedReview is a review just released and the count of resolved threads its release reopened.
type ReleasedReview struct {
	Review
	Reopens int
}

type ReleaseReviewParams struct {
	ID      string
	Verdict Verdict
	// Body is what the reviewer said about the change as a whole, empty when they said nothing.
	Body string
	Path string
}

// ReleaseReview releases a review, stamping its verdict and the file it was written to.
func (s *ReviewStore) ReleaseReview(ctx context.Context, params ReleaseReviewParams) (*ReleasedReview, error) {
	if !store.IsID(params.ID) {
		return nil, reviewNotFound(params.ID)
	}

	row, err := s.queries.ReleaseReview(ctx, models.ReleaseReviewParams{
		ID:      params.ID,
		Body:    params.Body,
		Verdict: models.NullReviewVerdict{ReviewVerdict: models.ReviewVerdict(params.Verdict), Valid: params.Verdict != ""},
		Path:    params.Path,
	})
	if db.IsNoRows(err) {
		return nil, reviewNotFound(params.ID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to release the review %q: %w", params.ID, err)
	}

	reopens, err := s.queries.ReopenRepliedThreads(ctx, params.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to reopen the threads review %q replied to: %w", params.ID, err)
	}

	return &ReleasedReview{
		Review:  reviewFromRow(*row),
		Reopens: int(reopens),
	}, nil
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

func snapshotFromRow(row models.ReviewSnapshot) Snapshot {
	return Snapshot{
		ID:        row.ID,
		TaskID:    row.TaskID,
		Ordinal:   row.Ordinal,
		BaseSHA:   row.BaseSha,
		SHA:       row.Sha,
		CreatedAt: row.CreatedAt,
		LandedAt:  row.LandedAt,
	}
}

func reviewFromRow(row models.Review) Review {
	review := Review{
		ID:             row.ID,
		TaskID:         row.TaskID,
		SnapshotID:     row.SnapshotID,
		AuthorKind:     AuthorKind(row.AuthorKind),
		AuthorAgentID:  row.AuthorAgentID,
		ExternalLogin:  row.ExternalLogin,
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

func snapshotNotFound(id string) error {
	return fmt.Errorf("%w: review snapshot %q", store.ErrNotFound, id)
}

func reviewNotFound(id string) error {
	return fmt.Errorf("%w: review %q", store.ErrNotFound, id)
}

// tally is where one task's work stands with the reviews released of it.
type tally struct {
	TaskID    string
	Snapshots int32
	// Verdict is the last conclusion released about the task's work, nil when every released
	// review only commented.
	Verdict *Verdict
	// SnapshotSHA is the snapshot that conclusion was given on, empty when there is none.
	SnapshotSHA string
}

// listTallies is where every task's work stands, leaving out the tasks no review has been released of.
func (s *ReviewStore) listTallies(ctx context.Context) ([]tally, error) {
	rows, err := s.queries.ListReviewTallies(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to count the snapshots of review: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListReviewTalliesRow) tally {
		tally := tally{TaskID: row.TaskID, Snapshots: row.Snapshots}
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

// newestSnapshot is the commit of the newest review snapshot of a task's agents.
type newestSnapshot struct {
	TaskID      string
	SnapshotSHA string
}

// ListNewestSnapshots is the newest snapshot of every task whose agents have one.
func (s *ReviewStore) ListNewestSnapshots(ctx context.Context) ([]newestSnapshot, error) {
	rows, err := s.queries.ListNewestTaskReviewSnapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the newest review snapshots: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListNewestTaskReviewSnapshotsRow) newestSnapshot {
		return newestSnapshot{TaskID: row.TaskID, SnapshotSHA: row.SnapshotSha}
	}), nil
}

// UnlandedSnapshot is a snapshot with a released review that the base branch has not yet been seen
// holding whole.
type UnlandedSnapshot struct {
	Ordinal int32
	BaseSHA string
	SHA     string
	// MergedAt is when the task's newest merged pull request merged, nil while none has.
	MergedAt *time.Time
}

// ListUnlandedSnapshots is the unlanded snapshots with a released review of every task with a live
// agent, newest first, keyed by task.
func (s *ReviewStore) ListUnlandedSnapshots(ctx context.Context) (map[string][]UnlandedSnapshot, error) {
	rows, err := s.queries.ListUnlandedReviewSnapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the unlanded review snapshots: %w", err)
	}

	byTask := map[string][]UnlandedSnapshot{}
	for _, row := range rows {
		byTask[row.TaskID] = append(byTask[row.TaskID], UnlandedSnapshot{
			Ordinal:  row.Ordinal,
			BaseSHA:  row.BaseSha,
			SHA:      row.Sha,
			MergedAt: row.MergedAt,
		})
	}

	return byTask, nil
}

// MarkSnapshotsLanded marks a task's snapshot and every earlier one as landed, leaving snapshots that already
// landed alone, and returns how many it marked.
func (s *ReviewStore) MarkSnapshotsLanded(ctx context.Context, taskID string, ordinal int32) (int64, error) {
	landed, err := s.queries.LandReviewSnapshots(ctx, models.LandReviewSnapshotsParams{
		TaskID:  taskID,
		Ordinal: ordinal,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to land snapshot %d of task %q: %w", ordinal, taskID, err)
	}

	return landed, nil
}

// ListLiveReviewers is the ids of a task's reviewers still owed an answer, oldest first, leaving
// out one whose last released verdict was an approval.
func (s *ReviewStore) ListLiveReviewers(ctx context.Context, taskID string) ([]string, error) {
	ids, err := s.queries.ListLiveReviewers(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the reviewers still reading task %q: %w", taskID, err)
	}

	return ids, nil
}

// SetDoorbell records the mail that told the agent its review was released.
func (s *ReviewStore) SetDoorbell(ctx context.Context, reviewID, mailID string) error {
	if err := s.queries.SetReviewDoorbell(ctx, models.SetReviewDoorbellParams{ID: reviewID, DoorbellMailID: &mailID}); err != nil {
		return fmt.Errorf("failed to record the mail that told of review %q: %w", reviewID, err)
	}

	return nil
}
