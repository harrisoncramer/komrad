package mail

import (
	"testing"
	"time"

	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListByKeyIsTheUnclaimedSet pins the read-back to the watched set: a piece of mail comes
// back while nobody has claimed it, expired or not, and not once its recipient has taken it.
func TestListByKeyIsTheUnclaimedSet(t *testing.T) {
	ctx := t.Context()
	st := open(t)
	sender := seedAgent(t, st, "sender")
	recipient := seedAgent(t, st, "recipient")

	send := func(body string, deadline time.Duration) string {
		t.Helper()

		sent, err := st.Send(ctx, SendParams{FromAgentID: &sender, FromName: "sender", ToAgentID: recipient, Body: body, Deadline: deadline})
		require.NoError(t, err)

		return sent.ID
	}
	waiting := send("waiting", time.Hour)
	expired := send("expired", -time.Hour)

	ids := func(mail []Mail) []string { return xslice.Map(mail, func(m Mail) string { return m.ID }) }

	listed, err := st.ListByKey(ctx, []string{waiting, expired, "not-an-id"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{waiting, expired}, ids(listed), "expiry is the reader's clock, not the set")

	unclaimed, err := st.ListUnclaimed(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{waiting, expired}, ids(unclaimed))

	claimed, err := st.Claim(ctx, recipient, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	listed, err = st.ListByKey(ctx, []string{waiting, expired})
	require.NoError(t, err)
	assert.Equal(t, []string{expired}, ids(listed), "a claimed piece is a tombstone")

	none, err := st.ListByKey(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, none)
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

		sent, err := st.Send(ctx, SendParams{FromAgentID: &sender, FromName: "sender", ToAgentID: to, Body: body, Deadline: time.Hour})
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
