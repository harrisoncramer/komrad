package mail

import (
	"testing"
	"time"

	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListWatchedIsTheUnclaimedSet pins the watched set to mail its recipient has not taken, expired or not.
func TestListWatchedIsTheUnclaimedSet(t *testing.T) {
	ctx := t.Context()
	st := open(t)
	sender := seedAgent(t, st, "sender")
	recipient := seedAgent(t, st, "recipient")

	send := func(body string, deadline time.Duration) string {
		t.Helper()

		sent, err := st.Send(ctx, SendParams{
			FromAgentID: &sender,
			FromName:    "sender",
			ToAgentID:   recipient,
			Body:        body,
			Deadline:    deadline,
		})
		require.NoError(t, err)

		return sent.ID
	}
	waiting := send("waiting", time.Hour)
	expired := send("expired", -time.Hour)

	ids := func(mail []Mail) []string { return xslice.Map(mail, func(m Mail) string { return m.ID }) }

	listed, err := st.ListWatched(ctx, []string{waiting})
	require.NoError(t, err)
	assert.Equal(t, []string{waiting}, ids(listed))

	unclaimed, err := st.ListWatched(ctx, nil)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{waiting, expired}, ids(unclaimed), "the clock does not decide membership")

	none, err := st.ListWatched(ctx, []string{})
	require.NoError(t, err)
	assert.Empty(t, none)

	claimed, err := st.Claim(ctx, recipient, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	listed, err = st.ListWatched(ctx, []string{waiting, expired})
	require.NoError(t, err)
	assert.Equal(t, []string{expired}, ids(listed), "a claimed piece is a tombstone")
}
