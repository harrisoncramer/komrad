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

	listed, err := st.ListWatched(ctx, []string{waiting, "not-an-id"})
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

// TestListUnclaimedToIsTheRecipientsUnclaimedSet pins the read a page of agents is assembled
// from: the unclaimed mail to the named agents, claimed and other recipients' mail left out.
func TestListUnclaimedToIsTheRecipientsUnclaimedSet(t *testing.T) {
	ctx := t.Context()
	st := open(t)
	sender := seedAgent(t, st, "sender")
	one := seedAgent(t, st, "one")
	other := seedAgent(t, st, "other")

	send := func(to, body string) string {
		t.Helper()

		sent, err := st.Send(ctx, SendParams{
			FromAgentID: &sender,
			FromName:    "sender",
			ToAgentID:   to,
			Body:        body,
			Deadline:    time.Hour,
		})
		require.NoError(t, err)

		return sent.ID
	}
	first := send(one, "first")
	second := send(one, "second")
	send(other, "elsewhere")

	claimed, err := st.Claim(ctx, one, 1)
	require.NoError(t, err)
	require.Equal(t, []string{first}, xslice.Map(claimed, func(m Mail) string { return m.ID }), "the oldest is taken first")

	listed, err := st.ListUnclaimedTo(ctx, []string{one, "not-an-id"})
	require.NoError(t, err)
	assert.Equal(t, []string{second}, xslice.Map(listed, func(m Mail) string { return m.ID }))

	none, err := st.ListUnclaimedTo(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, none)
}
