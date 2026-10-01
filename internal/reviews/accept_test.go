package reviews

import (
	"testing"
	"time"

	"komrad/internal/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
	reviewstore "komrad/internal/store/review"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var forgeWrittenAt = time.Date(2026, time.September, 30, 9, 0, 0, 0, time.UTC)

func forgeComment(providerID, login, body string) forge.ReviewComment {
	return forge.ReviewComment{ProviderID: providerID, Login: login, Body: body, WrittenAt: forgeWrittenAt}
}

func forgeThreadOf(comments ...forge.ReviewComment) forge.ReviewThread {
	return forge.ReviewThread{
		ProviderID: "T_1",
		Path:       "greet.go",
		Line:       41,
		CommitSHA:  "abc",
		Quote:      "greet(nil)",
		Comments:   comments,
	}
}

func seenAll(offer pullrequeststore.ForgeThread) []SeenForgeComment {
	return xslice.Map(offer.Comments, func(comment pullrequeststore.ForgeComment) SeenForgeComment {
		return SeenForgeComment{ID: comment.ID, EditedAt: comment.EditedAt}
	})
}

func (fx *reviewFixture) accept(t *testing.T, offer pullrequeststore.ForgeThread, seen []SeenForgeComment, note string) (*ReviewThreadWithMessages, error) {
	t.Helper()

	return fx.service.AcceptForgeThread(t.Context(), AcceptForgeThreadParams{
		AgentID:       fx.worker.ID,
		ForgeThreadID: offer.ID,
		Seen:          seen,
		FilePath:      "greet.go",
		NewStart:      41,
		NewEnd:        41,
		QuotedLines:   []string{"+\tgreet(nil)"},
		Note:          note,
		Author:        personAuthor,
	})
}

func (fx *reviewFixture) offer(t *testing.T) pullrequeststore.ForgeThread {
	t.Helper()

	stored, err := fx.stores.ForgeThreads.GetForAgent(t.Context(), fx.offerID, fx.worker.ID)
	require.NoError(t, err)

	return *stored
}

func describeMessages(messages []reviewstore.Message) []string {
	return xslice.Map(messages, func(message reviewstore.Message) string {
		if message.External == nil {
			return "note: " + message.Body
		}

		return "@" + message.External.Login + ": " + message.Body
	})
}

// TestAcceptForgeThread pins which comments go into the thread and when the forge having moved refuses the accept.
func TestAcceptForgeThread(t *testing.T) {
	edited := forgeWrittenAt.Add(time.Hour)

	tests := []struct {
		name     string
		accept   func(t *testing.T, fx *reviewFixture) (*ReviewThreadWithMessages, error)
		wantErr  error
		wantSaid []string
	}{
		{
			name: "a comment edited since the person read it is refused",
			accept: func(t *testing.T, fx *reviewFixture) (*ReviewThreadWithMessages, error) {
				seen := seenAll(fx.offer(t))
				seen[0].EditedAt = &edited

				return fx.accept(t, fx.offer(t), seen, "")
			},
			wantErr: ErrForgeThreadMoved,
		},
		{
			name: "a comment deleted since the person read it is refused",
			accept: func(t *testing.T, fx *reviewFixture) (*ReviewThreadWithMessages, error) {
				seen := append(seenAll(fx.offer(t)), SeenForgeComment{ID: "01a0f025-00bd-7db3-90c0-a0198677e92e"})

				return fx.accept(t, fx.offer(t), seen, "")
			},
			wantErr: ErrForgeThreadMoved,
		},
		{
			name: "an agent is refused",
			accept: func(t *testing.T, fx *reviewFixture) (*ReviewThreadWithMessages, error) {
				return fx.service.AcceptForgeThread(t.Context(), AcceptForgeThreadParams{
					AgentID:       fx.worker.ID,
					ForgeThreadID: fx.offerID,
					Seen:          seenAll(fx.offer(t)),
					FilePath:      "greet.go",
					Author:        newAgentAuthor(fx.reviewer),
				})
			},
			wantErr: ErrReviewHumanOnly,
		},
		{
			name: "a forge thread on another agent's pull request is not found",
			accept: func(t *testing.T, fx *reviewFixture) (*ReviewThreadWithMessages, error) {
				return fx.service.AcceptForgeThread(t.Context(), AcceptForgeThreadParams{
					AgentID:       fx.stranger.ID,
					ForgeThreadID: fx.offerID,
					Seen:          seenAll(fx.offer(t)),
					FilePath:      "greet.go",
					Author:        personAuthor,
				})
			},
			wantErr: ErrForgeThreadNotFound,
		},
		{
			name: "every comment read and the note follow one another in a thread of the person's",
			accept: func(t *testing.T, fx *reviewFixture) (*ReviewThreadWithMessages, error) {
				return fx.accept(t, fx.offer(t), seenAll(fx.offer(t)), "agreed, close it")
			},
			wantSaid: []string{"@dana: this leaks the handle", "@lee: and the socket", "note: agreed, close it"},
		},
		{
			name: "a comment that came in after the person read the thread is left out",
			accept: func(t *testing.T, fx *reviewFixture) (*ReviewThreadWithMessages, error) {
				return fx.accept(t, fx.offer(t), seenAll(fx.offer(t))[:1], "")
			},
			wantSaid: []string{"@dana: this leaks the handle"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)
			fx.seedOffer(t, forgeComment("C_1", "dana", "this leaks the handle"), forgeComment("C_2", "lee", "and the socket"))

			got, err := tt.accept(t, fx)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, fx.offer(t).AcceptedThreadID)

				return
			}
			require.NoError(t, err)

			assert.Equal(t, tt.wantSaid, describeMessages(got.Messages))
			for _, message := range got.Messages {
				assert.Equal(t, reviewstore.DeliveryUnsent, message.Delivery)
			}
			assert.Equal(t, &got.Thread.ID, fx.offer(t).AcceptedThreadID)
		})
	}
}

// TestAcceptForgeThreadAgain pins that a later accept adds only what the forge added or edited, in the same thread.
func TestAcceptForgeThreadAgain(t *testing.T) {
	fx := newReviewFixture(t)
	fx.seedOffer(t, forgeComment("C_1", "dana", "this leaks the handle"))
	require.NoError(t, fx.stores.ForgeThreads.SetDismissed(t.Context(), fx.offerID, true))

	first, err := fx.accept(t, fx.offer(t), seenAll(fx.offer(t)), "")
	require.NoError(t, err)
	assert.False(t, fx.offer(t).Dismissed)

	_, err = fx.accept(t, fx.offer(t), seenAll(fx.offer(t)), "")
	require.ErrorIs(t, err, ErrForgeThreadNothingNew)

	edited := forgeWrittenAt.Add(time.Hour)
	revised := forgeComment("C_1", "dana", "this leaks the file handle")
	revised.EditedAt = &edited
	fx.seedOffer(t, revised, forgeComment("C_2", "lee", "and the socket"))

	again, err := fx.accept(t, fx.offer(t), seenAll(fx.offer(t)), "both, please")
	require.NoError(t, err)
	assert.Equal(t, first.Thread.ID, again.Thread.ID)
	assert.Equal(t, []string{
		"@dana: this leaks the handle",
		"@dana: this leaks the file handle",
		"@lee: and the socket",
		"note: both, please",
	}, describeMessages(again.Messages))
}

// TestAcceptForgeThreadAgainWaitsForOneInFlight covers a second accept of the same forge comment finding it copied by the first.
func TestAcceptForgeThreadAgainWaitsForOneInFlight(t *testing.T) {
	fx := newReviewFixture(t)
	fx.seedOffer(t, forgeComment("C_1", "dana", "this leaks the handle"))
	accepted, err := fx.accept(t, fx.offer(t), seenAll(fx.offer(t)), "")
	require.NoError(t, err)
	fx.seedOffer(t, forgeComment("C_1", "dana", "this leaks the handle"), forgeComment("C_2", "lee", "and the socket"))
	offer := fx.offer(t)

	tx, err := fx.stores.Pool.GetRawPool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	require.NoError(t, pullrequeststore.NewForgeThreadStore(tx).LockAccepted(t.Context(), offer.ID, accepted.Thread.ID))

	second := make(chan error, 1)
	go func() {
		_, err := fx.accept(t, offer, seenAll(offer), "")
		second <- err
	}()
	require.Eventually(t, func() bool {
		var waiting int
		require.NoError(t, fx.stores.Pool.GetRawPool().QueryRow(t.Context(), "SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'").Scan(&waiting))

		return waiting > 0
	}, 5*time.Second, 10*time.Millisecond)

	_, err = reviewstore.NewReviewStore(tx).CreateMessage(t.Context(), reviewstore.CreateMessageParams{
		ThreadID:   accepted.Thread.ID,
		ReviewID:   &accepted.Thread.ReviewID,
		AuthorKind: personAuthor.Kind,
		Body:       offer.Comments[1].Body,
		External:   externalOf(offer.Comments[1]),
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))

	require.ErrorIs(t, <-second, ErrForgeThreadNothingNew)
}

func (fx *reviewFixture) seedOffer(t *testing.T, comments ...forge.ReviewComment) {
	t.Helper()

	seeded := fx.stores.SeedForgeThreads(t, fx.worker.ID, forgeThreadOf(comments...))
	require.Len(t, seeded, 1)
	fx.offerID = seeded[0].ID
}
