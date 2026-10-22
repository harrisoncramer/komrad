package reviews

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/eventprompts"
	"komrad/internal/jobs/internaljobs"
	"komrad/internal/mail"
	mailstore "komrad/internal/store/mail"
	reviewstore "komrad/internal/store/review"
	"komrad/lib/clock"
	"komrad/lib/xslice"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTakeSnapshot pins that a snapshot is taken only of a tree that has moved.
func TestTakeSnapshot(t *testing.T) {
	tests := []struct {
		name        string
		snapshot    string
		wantOrdinal int32
	}{
		{
			name:        "a tree that has not moved",
			snapshot:    firstSnapshot,
			wantOrdinal: 1,
		},
		{
			name:        "a tree the agent has written more in",
			snapshot:    secondSnapshot,
			wantOrdinal: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)
			fx.snapshots.SHA = tt.snapshot

			snapshot, err := fx.service.TakeSnapshot(t.Context(), taskByID(*fx.writer.TaskID), personAuthor)
			require.NoError(t, err)

			assert.Equal(t, tt.wantOrdinal, snapshot.Ordinal)
			assert.Equal(t, tt.snapshot, snapshot.SHA)
		})
	}
}

// TestTakeSnapshotReleasesTheRefOfASnapshotItFailedToWrite pins that a snapshot whose insert fails leaves
// no ref behind.
func TestTakeSnapshotReleasesTheRefOfASnapshotItFailedToWrite(t *testing.T) {
	fx := newReviewFixture(t)
	fx.snapshots.SHA = secondSnapshot
	fx.snapshots.Taking = func() {
		_, err := fx.stores.Agents.Delete(t.Context(), []string{fx.writer.ID, fx.reviewer.ID})
		require.NoError(t, err)
		require.NoError(t, fx.stores.Workflows.Delete(t.Context(), fx.writer.WorkflowID))
	}

	_, err := fx.service.TakeSnapshot(t.Context(), taskByID(*fx.writer.TaskID), personAuthor)
	require.Error(t, err)

	assert.Equal(t, []string{reviewstore.SnapshotRef(*fx.writer.TaskID, secondSnapshot)}, fx.snapshots.Released)
}

// TestTakeSnapshotTakesOneSnapshotOfATreeAskedForTwiceAtOnce pins that two callers racing on the same
// tree share one snapshot.
func TestTakeSnapshotTakesOneSnapshotOfATreeAskedForTwiceAtOnce(t *testing.T) {
	fx := newReviewFixture(t)
	fx.snapshots.SHA = secondSnapshot
	fx.snapshots.Taking = func() {
		clock.Sleep(t.Context(), 250*time.Millisecond)
	}

	var group sync.WaitGroup
	ordinals := make([]int32, 2)
	errs := make([]error, 2)
	for i := range ordinals {
		group.Go(func() {
			snapshot, err := fx.service.TakeSnapshot(t.Context(), taskByID(*fx.writer.TaskID), personAuthor)
			errs[i] = err
			if err == nil {
				ordinals[i] = snapshot.Ordinal
			}
		})
	}
	group.Wait()

	require.NoError(t, errors.Join(errs...))
	assert.Equal(t, []int32{2, 2}, ordinals)
}

// TestReleaseReviewWritesTheFile pins what the file a release writes says.
func TestReleaseReviewWritesTheFile(t *testing.T) {
	tests := []struct {
		name        string
		write       func(t *testing.T, fx *reviewFixture) *ReleaseReviewResult
		wantSaid    []string
		wantNotSaid []string
	}{
		{
			name: "a reviewer approving with nothing written",
			write: func(t *testing.T, fx *reviewFixture) *ReleaseReviewResult {
				return fx.releaseReview(t, newAgentAuthor(fx.reviewer), reviewstore.VerdictApproved)
			},
			wantSaid:    []string{"Approved", "snapshot 1", "reviewer"},
			wantNotSaid: []string{"Changes requested"},
		},
		{
			name: "a reviewer's comment",
			write: func(t *testing.T, fx *reviewFixture) *ReleaseReviewResult {
				fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")

				return fx.releaseReview(t, newAgentAuthor(fx.reviewer), reviewstore.VerdictChangesRequested)
			},
			wantSaid: []string{"Changes requested", "this needs a nil check", "greet.go:41", "reviewer"},
		},
		{
			name: "a comment on a file the snapshot holds",
			write: func(t *testing.T, fx *reviewFixture) *ReleaseReviewResult {
				fx.snapshots.Files["greet.go"] = buildGreetSource()
				_, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
					Task:     taskByID(*fx.writer.TaskID),
					FilePath: "greet.go",
					NewStart: 12,
					NewEnd:   13,
					Body:     "this needs a nil check",
					Author:   newAgentAuthor(fx.reviewer),
				})
				require.NoError(t, err)

				return fx.releaseReview(t, newAgentAuthor(fx.reviewer), reviewstore.VerdictChangesRequested)
			},
			wantSaid: []string{
				">> 12  line 12 of greet.go",
				">> 13  line 13 of greet.go",
				"    6  line 6 of greet.go",
				"   19  line 19 of greet.go",
			},
			wantNotSaid: []string{"line 5 of greet.go", "line 20 of greet.go"},
		},
		{
			name: "a comment on a file the snapshot cannot hand back",
			write: func(t *testing.T, fx *reviewFixture) *ReleaseReviewResult {
				fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")

				return fx.releaseReview(t, newAgentAuthor(fx.reviewer), reviewstore.VerdictChangesRequested)
			},
			wantSaid: []string{"+\tgreet(nil)", "this needs a nil check"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)

			released := tt.write(t, fx)

			written, err := os.ReadFile(released.Path)
			require.NoError(t, err)

			for _, said := range append(tt.wantSaid, *fx.writer.TaskID) {
				assert.Contains(t, string(written), said)
			}
			for _, unsaid := range tt.wantNotSaid {
				assert.NotContains(t, string(written), unsaid)
			}
		})
	}
}

// TestReleaseReviewRingsTheAgent pins that each release is one piece of mail naming it and none of its comments.
func TestReleaseReviewRingsTheAgent(t *testing.T) {
	fx := newReviewFixture(t)
	reviewer := newAgentAuthor(fx.reviewer)
	fx.writeComment(t, reviewer, "this needs a nil check")
	fx.writeComment(t, reviewer, "and the caller above it")
	fx.writeComment(t, reviewer, "and the test")

	released := fx.releaseReview(t, reviewer, reviewstore.VerdictChangesRequested)

	assert.Equal(t, 3, released.Comments)
	rung := fx.listMail(t, fx.writer.ID)
	require.Len(t, rung, 1, "one mail per release however many comments it holds")
	assert.Equal(t, mailstore.KindReviewActivity, rung[0].Kind)
	assert.Contains(t, rung[0].Body, "get_review with review id "+released.Review.ID)
	assert.Contains(t, rung[0].Body, "or read "+released.Path)
	assert.Contains(t, rung[0].Body, "changes requested")
	assert.Equal(t, []string{eventprompts.ReviewChangesRequested}, rung[0].Events)
	assert.NotContains(t, rung[0].Body, "nil check", "the mail carries no feedback")
}

// TestReleaseReviewRingsNobodyOnceTheWriterIsArchived pins that a release on a task whose writer is archived sends no mail.
func TestReleaseReviewRingsNobodyOnceTheWriterIsArchived(t *testing.T) {
	fx := newReviewFixture(t)
	_, err := fx.stores.Agents.Archive(t.Context(), []string{fx.writer.ID})
	require.NoError(t, err)

	fx.releaseReview(t, newAgentAuthor(fx.reviewer), reviewstore.VerdictApproved)

	assert.Empty(t, fx.listMail(t, fx.writer.ID))
}

// TestReleaseReviewArchivesAnApprovingReviewer pins that only a reviewer's approval puts the reviewer away.
func TestReleaseReviewArchivesAnApprovingReviewer(t *testing.T) {
	tests := []struct {
		name    string
		keep    bool
		author  func(fx *reviewFixture) ReviewAuthor
		verdict reviewstore.Verdict
		want    func(fx *reviewFixture) []river.JobArgs
	}{
		{
			name: "a reviewer approving",
			author: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.reviewer)
			},
			verdict: reviewstore.VerdictApproved,
			want: func(fx *reviewFixture) []river.JobArgs {
				return []river.JobArgs{internaljobs.ArchiveAgentArgs{AgentID: fx.reviewer.ID}}
			},
		},
		{
			name: "a reviewer approving with archiving on approval turned off",
			keep: true,
			author: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.reviewer)
			},
			verdict: reviewstore.VerdictApproved,
		},
		{
			name: "a reviewer asking for changes",
			author: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.reviewer)
			},
			verdict: reviewstore.VerdictChangesRequested,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixtureReleasing(t, NewReleasedHandlerParams{ArchiveOnApproval: !tt.keep})

			fx.releaseReview(t, tt.author(fx), tt.verdict)

			if tt.want == nil {
				assert.Empty(t, fx.events.ListFollowUps())

				return
			}
			assert.Equal(t, tt.want(fx), fx.events.ListFollowUps())
		})
	}
}

// TestReleaseReviewApprovalMail pins when an approval sends the agent to read the review.
func TestReleaseReviewApprovalMail(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantSaid    string
		wantNotSaid string
	}{
		{
			name:        "an approval with nothing in it sends the agent nowhere",
			wantSaid:    "There is nothing to read or answer.",
			wantNotSaid: "get_review",
		},
		{
			name:        "an approval with a note sends the agent to read it",
			body:        "keep this report on your row",
			wantSaid:    "get_review with review id",
			wantNotSaid: "nothing to read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)

			_, err := fx.service.ReleaseReview(t.Context(), ReleaseReviewParams{
				Task:    taskByID(*fx.writer.TaskID),
				Verdict: reviewstore.VerdictApproved,
				Body:    tt.body,
				Author:  newAgentAuthor(fx.reviewer),
			})
			require.NoError(t, err)

			rung := fx.listMail(t, fx.writer.ID)
			require.Len(t, rung, 1)
			assert.Contains(t, rung[0].Body, tt.wantSaid)
			assert.NotContains(t, rung[0].Body, tt.wantNotSaid)
		})
	}
}

// TestReleaseReviewIntoAFullMailbox pins that a release stands while its mail waits for room.
func TestReleaseReviewIntoAFullMailbox(t *testing.T) {
	fx := newReviewFixture(t)
	fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")
	fillMailbox(t, fx)

	_, err := fx.service.ReleaseReview(t.Context(), ReleaseReviewParams{
		Task:    taskByID(*fx.writer.TaskID),
		Verdict: reviewstore.VerdictChangesRequested,
		Author:  newAgentAuthor(fx.reviewer),
	})
	require.NoError(t, err)

	errs := fx.events.TakeErrors()
	require.Len(t, errs, 1)
	require.ErrorIs(t, errs[0], mail.ErrMailboxFull)

	listed, err := fx.service.ListReviews(t.Context(), taskByID(*fx.writer.TaskID), personAuthor)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.NotNil(t, listed[0].ReleasedAt)
}

// TestGetReview pins that a released review reads back as what went out with it, around the code it was about.
func TestGetReview(t *testing.T) {
	fx := newReviewFixture(t)
	reviewer := newAgentAuthor(fx.reviewer)
	fx.snapshots.Files["greet.go"] = buildGreetSource()
	opened, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		Task:     taskByID(*fx.writer.TaskID),
		FilePath: "greet.go",
		NewStart: 12,
		NewEnd:   13,
		Body:     "this needs a nil check",
		Author:   reviewer,
	})
	require.NoError(t, err)

	gone, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		Task:        taskByID(*fx.writer.TaskID),
		FilePath:    "gone.go",
		QuotedLines: []string{"+\tgreet(nil)"},
		Body:        "and this file",
		Author:      reviewer,
	})
	require.NoError(t, err)

	released, err := fx.service.ReleaseReview(t.Context(), ReleaseReviewParams{
		Task:    taskByID(*fx.writer.TaskID),
		Verdict: reviewstore.VerdictChangesRequested,
		Body:    "two things before it goes",
		Author:  reviewer,
	})
	require.NoError(t, err)

	fx.writeAnswer(t, opened.Thread.ID)
	_, err = fx.service.AddReviewMessage(t.Context(), AddReviewMessageParams{
		ThreadID: opened.Thread.ID,
		Body:     "not quite, the other branch too",
		Author:   personAuthor,
	})
	require.NoError(t, err)

	whole, err := fx.service.GetReview(t.Context(), released.Review.ID, newAgentAuthor(fx.writer))
	require.NoError(t, err)

	assert.Equal(t, "two things before it goes", whole.Review.Body)
	assert.Empty(t, whole.Review.Path, "the agent is not told where the file is")
	assert.Equal(t, firstSnapshot, whole.Snapshot.SHA)

	require.Len(t, whole.Threads, 2)
	byFile := xslice.KeyBy(whole.Threads, func(thread ReviewThreadInContext) string {
		return thread.Thread.FilePath
	})
	assert.Len(t, byFile["greet.go"].Context, 14, "six lines either side of the two commented")
	assert.Contains(t, byFile["greet.go"].Context, ">> 12  line 12 of greet.go")
	assert.Empty(t, byFile["gone.go"].Context, "a file the snapshot cannot hand back leaves its quoted lines")
	assert.Equal(t, gone.Thread.QuotedLines, byFile["gone.go"].Thread.QuotedLines)
	assert.Equal(t, []string{"this needs a nil check"}, readBodies(byFile["greet.go"].Messages),
		"the answer and the person's reply belong to no review")
}

// TestAReviewStaysOnItsSnapshot pins that a reviewer's review stays on the snapshot it opened on while the writer moves.
func TestAReviewStaysOnItsSnapshot(t *testing.T) {
	fx := newReviewFixture(t)
	reviewer := newAgentAuthor(fx.reviewer)
	fx.snapshots.FilesAt = map[string]map[string][]string{
		firstSnapshot:  {"greet.go": {"before the edit"}},
		secondSnapshot: {"greet.go": {"after the edit"}},
	}

	commentOn := func(snapshot string) (*ReviewThreadWithMessages, error) {
		return fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
			Task:        taskByID(*fx.writer.TaskID),
			SnapshotSHA: snapshot,
			FilePath:    "greet.go",
			NewStart:    1,
			NewEnd:      1,
			Body:        "why two sources here?",
			Author:      reviewer,
		})
	}

	older, err := commentOn(firstSnapshot)
	require.NoError(t, err)
	fx.advanceTree(t)
	_, err = commentOn(secondSnapshot)
	require.ErrorIs(t, err, errReviewInFlight, "the review is pinned to the snapshot it opened on")

	released := fx.releaseReview(t, reviewer, reviewstore.VerdictChangesRequested)

	whole, err := fx.service.GetReview(t.Context(), released.Review.ID, newAgentAuthor(fx.writer))
	require.NoError(t, err)
	assert.Equal(t, firstSnapshot, whole.Snapshot.SHA)
	require.Len(t, whole.Threads, 1)
	assert.Equal(t, older.Thread.ID, whole.Threads[0].Thread.ID)
	assert.Equal(t, []string{">> 1  before the edit"}, whole.Threads[0].Context)
}

// TestGetReviewOfAnUnreleasedReview pins that a review still being written is found only by its author and the person.
func TestGetReviewOfAnUnreleasedReview(t *testing.T) {
	tests := []struct {
		name    string
		reader  func(fx *reviewFixture) ReviewAuthor
		wantErr error
	}{
		{
			name: "the agent under review",
			reader: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.writer)
			},
			wantErr: ErrReviewNotFound,
		},
		{
			name: "an agent with no business in it",
			reader: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.otherWriter)
			},
			wantErr: ErrReviewNotFound,
		},
		{
			name: "the person",
			reader: func(*reviewFixture) ReviewAuthor {
				return personAuthor
			},
		},
		{
			name: "the reviewer writing it",
			reader: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.reviewer)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)
			written := fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")

			_, err := fx.service.GetReview(t.Context(), *written.Thread.ReviewID, tt.reader(fx))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
		})
	}
}

// TestListReviews pins which reviews each reader is shown, and to whom the path of the file is given.
func TestListReviews(t *testing.T) {
	tests := []struct {
		name       string
		reader     func(fx *reviewFixture) ReviewAuthor
		wantPaths  []bool
		wantOpened int
	}{
		{
			name: "the agent under review is not told a review is open on it",
			reader: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.writer)
			},
			wantPaths: []bool{false},
		},
		{
			name: "the reviewer sees its own draft and the path of what it released",
			reader: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.reviewer)
			},
			wantPaths:  []bool{true, false},
			wantOpened: 1,
		},
		{
			name: "the person sees everything",
			reader: func(*reviewFixture) ReviewAuthor {
				return personAuthor
			},
			wantPaths:  []bool{true, false},
			wantOpened: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)
			fx.releaseFirstComment(t)
			fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")
			fx.releaseReview(t, newAgentAuthor(fx.reviewer), reviewstore.VerdictChangesRequested)
			fx.advanceTree(t)
			fx.writeComment(t, newAgentAuthor(fx.reviewer), "and the caller above it")

			listed, err := fx.service.ListReviews(t.Context(), taskByID(*fx.writer.TaskID), tt.reader(fx))
			require.NoError(t, err)

			paths := xslice.Map(listed, func(review reviewstore.Review) bool {
				return review.Path != ""
			})
			assert.Equal(t, tt.wantPaths, paths)
			assert.Equal(t, tt.wantOpened, xslice.Count(listed, func(review reviewstore.Review) bool {
				return review.ReleasedAt == nil
			}))
		})
	}
}

// fillMailbox sends the writer mail until its mailbox refuses another.
func fillMailbox(t *testing.T, fx *reviewFixture) {
	t.Helper()

	mailer := mail.NewMailService(mail.NewMailServiceParams{
		Mail:      fx.stores.Mail,
		Agents:    fx.stores.Agents,
		Workflows: fx.stores.Workflows,
		Config:    config.Mail{},
	})
	for {
		_, err := mailer.SendFromKomrad(t.Context(), mail.SendFromKomradParams{
			ToAgentID: fx.writer.ID,
			Mail:      eventprompts.ThreadAnswered{WriterName: "waiting"},
			SubjectID: uuid.NewString(),
		})
		if errors.Is(err, mail.ErrMailboxFull) {
			return
		}
		require.NoError(t, err)
	}
}

// buildGreetSource is twenty numbered lines of the file a snapshot holds.
func buildGreetSource() []string {
	source := make([]string, 0, 20)
	for line := 1; line <= 20; line++ {
		source = append(source, fmt.Sprintf("line %d of greet.go", line))
	}

	return source
}

func readBodies(messages []reviewstore.Message) []string {
	return xslice.Map(messages, func(message reviewstore.Message) string {
		return message.Body
	})
}
