package reviews

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/mail"
	mailstore "komrad/internal/store/mail"
	reviewstore "komrad/internal/store/review"
	"komrad/lib/clock"
	"komrad/lib/xslice"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenRound pins that a round is taken only of a tree that has moved.
func TestOpenRound(t *testing.T) {
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

			round, err := fx.service.OpenRound(t.Context(), fx.worker.ID, personAuthor)
			require.NoError(t, err)

			assert.Equal(t, tt.wantOrdinal, round.Ordinal)
			assert.Equal(t, tt.snapshot, round.SnapshotSHA)
		})
	}
}

// TestOpenRoundReleasesTheRefOfARoundItFailedToWrite pins that a round whose insert fails leaves
// no ref behind.
func TestOpenRoundReleasesTheRefOfARoundItFailedToWrite(t *testing.T) {
	fx := newReviewFixture(t)
	fx.snapshots.SHA = secondSnapshot
	fx.snapshots.Taking = func() {
		_, err := fx.stores.Agents.Delete(t.Context(), []string{fx.worker.ID})
		require.NoError(t, err)
	}

	_, err := fx.service.OpenRound(t.Context(), fx.worker.ID, personAuthor)
	require.Error(t, err)

	assert.Equal(t, []string{reviewstore.RoundRef(fx.worker.ID, secondSnapshot)}, fx.snapshots.Released)
}

// TestOpenRoundTakesOneRoundOfATreeOpenedTwiceAtOnce pins that two callers racing on the same
// tree share one round.
func TestOpenRoundTakesOneRoundOfATreeOpenedTwiceAtOnce(t *testing.T) {
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
			round, err := fx.service.OpenRound(t.Context(), fx.worker.ID, personAuthor)
			errs[i] = err
			if err == nil {
				ordinals[i] = round.Ordinal
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
			name: "the person sending with nothing written",
			write: func(t *testing.T, fx *reviewFixture) *ReleaseReviewResult {
				return fx.releaseReview(t, personAuthor, "")
			},
			wantSaid:    []string{"Comments to act on", "round 1", "the person running komrad"},
			wantNotSaid: []string{"Approved", "Changes requested"},
		},
		{
			name: "a reviewer's comment",
			write: func(t *testing.T, fx *reviewFixture) *ReleaseReviewResult {
				fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")

				return fx.releaseReview(t, newAgentAuthor(fx.reviewer), reviewstore.VerdictChangesRequested)
			},
			wantSaid: []string{"Changes requested", "this needs a nil check", "greet.go:41", "worker", "reviewer"},
		},
		{
			name: "a comment on a file the round holds",
			write: func(t *testing.T, fx *reviewFixture) *ReleaseReviewResult {
				fx.snapshots.Files["greet.go"] = buildGreetSource()
				_, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
					AgentID:  fx.worker.ID,
					FilePath: "greet.go",
					NewStart: 12,
					NewEnd:   13,
					Body:     "this needs a nil check",
					Author:   personAuthor,
				})
				require.NoError(t, err)

				return fx.releaseReview(t, personAuthor, "")
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
			name: "a comment on a file the round cannot hand back",
			write: func(t *testing.T, fx *reviewFixture) *ReleaseReviewResult {
				fx.writeComment(t, personAuthor, "this needs a nil check")

				return fx.releaseReview(t, personAuthor, "")
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

			for _, said := range append(tt.wantSaid, fx.worker.ID) {
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

	first := fx.releaseReview(t, reviewer, reviewstore.VerdictChangesRequested)
	second := fx.releaseReview(t, personAuthor, "")

	assert.Equal(t, 3, first.Comments)
	rung := fx.listMail(t, fx.worker.ID)
	require.Len(t, rung, 2, "one mail per release however many comments it holds")
	for i, released := range []*ReleaseReviewResult{first, second} {
		stored, err := fx.reviews.GetReview(t.Context(), released.Review.ID)
		require.NoError(t, err)
		assert.Equal(t, &rung[i].ID, stored.DoorbellMailID)
		assert.Equal(t, mailstore.KindReviewActivity, rung[i].Kind)
		assert.Contains(t, rung[i].Body, "get_review with review id "+released.Review.ID)
		assert.Contains(t, rung[i].Body, "or read "+released.Path)
	}
	assert.Contains(t, rung[0].Body, "changes requested")
	assert.Contains(t, rung[1].Body, "released: 0 comment(s).")
	assert.NotContains(t, rung[0].Body, "nil check", "the mail carries no feedback")
}

// TestReleaseReviewIntoAFullMailbox pins that a release stands while its mail waits for room.
func TestReleaseReviewIntoAFullMailbox(t *testing.T) {
	fx := newReviewFixture(t)
	fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")
	fillMailbox(t, fx)

	_, err := fx.service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: fx.worker.ID,
		Verdict: reviewstore.VerdictChangesRequested,
		Author:  newAgentAuthor(fx.reviewer),
	})
	require.NoError(t, err)

	errs := fx.events.TakeErrors()
	require.Len(t, errs, 1)
	require.ErrorIs(t, errs[0], mail.ErrMailboxFull)

	listed, err := fx.service.ListReviews(t.Context(), fx.worker.ID, personAuthor)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.NotNil(t, listed[0].ReleasedAt)
	assert.Nil(t, listed[0].DoorbellMailID)
}

// TestGetReview pins that a released review reads back as what went out with it, around the code it was about.
func TestGetReview(t *testing.T) {
	fx := newReviewFixture(t)
	fx.snapshots.Files["greet.go"] = buildGreetSource()
	opened, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		AgentID:  fx.worker.ID,
		FilePath: "greet.go",
		NewStart: 12,
		NewEnd:   13,
		Body:     "this needs a nil check",
		Author:   personAuthor,
	})
	require.NoError(t, err)

	gone, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		AgentID:     fx.worker.ID,
		FilePath:    "gone.go",
		QuotedLines: []string{"+\tgreet(nil)"},
		Body:        "and this file",
		Author:      personAuthor,
	})
	require.NoError(t, err)

	first, err := fx.service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: fx.worker.ID,
		Body:    "two things before it goes",
		Author:  personAuthor,
	})
	require.NoError(t, err)

	fx.writeAnswer(t, opened.Thread.ID)
	_, err = fx.service.AddReviewMessage(t.Context(), AddReviewMessageParams{
		ThreadID: opened.Thread.ID,
		Body:     "not quite, the other branch too",
		Author:   personAuthor,
	})
	require.NoError(t, err)

	second := fx.releaseReview(t, personAuthor, "")

	whole, err := fx.service.GetReview(t.Context(), first.Review.ID, newAgentAuthor(fx.worker))
	require.NoError(t, err)

	assert.Equal(t, "two things before it goes", whole.Review.Body)
	assert.Empty(t, whole.Review.Path, "the agent is not told where the file is")
	assert.Equal(t, firstSnapshot, whole.Round.SnapshotSHA)

	require.Len(t, whole.Threads, 2)
	byFile := xslice.KeyBy(whole.Threads, func(thread ReviewThreadInContext) string {
		return thread.Thread.FilePath
	})
	assert.Len(t, byFile["greet.go"].Context, 14, "six lines either side of the two commented")
	assert.Contains(t, byFile["greet.go"].Context, ">> 12  line 12 of greet.go")
	assert.Empty(t, byFile["gone.go"].Context, "a file the round cannot hand back leaves its quoted lines")
	assert.Equal(t, gone.Thread.QuotedLines, byFile["gone.go"].Thread.QuotedLines)
	assert.Equal(t, []string{"this needs a nil check"}, readBodies(byFile["greet.go"].Messages),
		"the answer and the second release belong elsewhere")
	assert.Nil(t, byFile["greet.go"].Messages[0].SentAt, "and none of the panel's record of when")

	again, err := fx.service.GetReview(t.Context(), second.Review.ID, newAgentAuthor(fx.worker))
	require.NoError(t, err)
	require.Len(t, again.Threads, 1)
	assert.Equal(t, []string{"not quite, the other branch too"}, readBodies(again.Threads[0].Messages))
}

// TestAReviewCarriedAcrossRounds pins that a person's draft goes out on the newest round they commented on, each comment read against its own.
func TestAReviewCarriedAcrossRounds(t *testing.T) {
	fx := newReviewFixture(t)
	fx.snapshots.FilesAt = map[string]map[string][]string{
		firstSnapshot:  {"greet.go": {"before the edit"}},
		secondSnapshot: {"greet.go": {"after the edit"}},
	}

	commentOn := func(snapshot string) *ReviewThreadWithMessages {
		written, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
			AgentID:     fx.worker.ID,
			SnapshotSHA: snapshot,
			FilePath:    "greet.go",
			NewStart:    1,
			NewEnd:      1,
			Body:        "why two sources here?",
			Author:      personAuthor,
		})
		require.NoError(t, err)

		return written
	}

	older := commentOn(firstSnapshot)
	fx.advanceTree(t)
	newer := commentOn(secondSnapshot)

	released := fx.releaseReview(t, personAuthor, "")

	whole, err := fx.service.GetReview(t.Context(), released.Review.ID, newAgentAuthor(fx.worker))
	require.NoError(t, err)
	assert.Equal(t, secondSnapshot, whole.Round.SnapshotSHA, "the review is of the newest work the person read")

	byID := xslice.KeyBy(whole.Threads, func(thread ReviewThreadInContext) string {
		return thread.Thread.ID
	})
	require.Len(t, byID, 2)
	assert.Equal(t, []string{">> 1  before the edit"}, byID[older.Thread.ID].Context)
	assert.Equal(t, []string{">> 1  after the edit"}, byID[newer.Thread.ID].Context)
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
				return newAgentAuthor(fx.worker)
			},
			wantErr: ErrReviewNotFound,
		},
		{
			name: "an agent with no business in it",
			reader: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.stranger)
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

			_, err := fx.service.GetReview(t.Context(), written.Thread.ReviewID, tt.reader(fx))
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
				return newAgentAuthor(fx.worker)
			},
			wantPaths: []bool{false},
		},
		{
			name: "the reviewer sees its own draft and not the person's path",
			reader: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.reviewer)
			},
			wantPaths:  []bool{false, false},
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
			fx.releaseFirstReview(t)
			fx.writeComment(t, newAgentAuthor(fx.reviewer), "and the caller above it")

			listed, err := fx.service.ListReviews(t.Context(), fx.worker.ID, tt.reader(fx))
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

// fillMailbox sends the worker mail until its mailbox refuses another.
func fillMailbox(t *testing.T, fx *reviewFixture) {
	t.Helper()

	mailer := mail.NewMailService(mail.NewMailServiceParams{
		MailReadWriter: fx.stores.Mail,
		Agents:         fx.stores.Agents,
		Config:         config.Mail{},
	})
	for {
		_, err := mailer.SendFromKomrad(t.Context(), mail.SendFromKomradParams{
			ToAgentID: fx.worker.ID,
			Body:      "waiting",
			Kind:      mailstore.KindReviewActivity,
			SubjectID: uuid.NewString(),
		})
		if errors.Is(err, mail.ErrMailboxFull) {
			return
		}
		require.NoError(t, err)
	}
}

// buildGreetSource is twenty numbered lines of the file a round holds.
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
