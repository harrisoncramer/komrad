package mail

import (
	"testing"
	"time"

	"weave/internal/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMailNotifiesTheRecipient pins that sending, claiming and deleting mail each name the
// recipient, whose row's unread count is what moved.
func TestMailNotifiesTheRecipient(t *testing.T) {
	ctx := t.Context()
	st := open(t)
	sender := seedAgent(t, st, "sender")
	recipient := seedAgent(t, st, "recipient")
	raised := dbtest.Listen(t, st.url)

	sent, err := st.Send(ctx, SendParams{FromAgentID: &sender, ToAgentID: recipient, Body: "hi", Deadline: time.Hour})
	require.NoError(t, err)
	assert.Equal(t, "agents "+recipient, raised.Next(t), "a send names the recipient")

	_, err = st.Claim(ctx, recipient, 10)
	require.NoError(t, err)
	assert.Equal(t, "agents "+recipient, raised.Next(t), "a claim names the recipient")

	require.NoError(t, st.Delete(ctx, []string{sent.ID}))
	assert.Equal(t, "agents "+recipient, raised.Next(t), "a delete names the recipient of what it removed")
}
