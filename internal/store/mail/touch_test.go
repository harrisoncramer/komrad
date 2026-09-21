package mail

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMailTouchesTheRecipient pins that sending, claiming and deleting mail each name the
// recipient, whose row's unread count is what moved.
func TestMailTouchesTheRecipient(t *testing.T) {
	ctx := t.Context()
	st := open(t)
	sender := seedAgent(t, st, "sender")
	recipient := seedAgent(t, st, "recipient")
	st.touched.Reset()

	sent, err := st.Send(ctx, SendParams{FromAgentID: &sender, ToAgentID: recipient, Body: "hi", Deadline: time.Hour})
	require.NoError(t, err)
	assert.Equal(t, []string{recipient}, st.touched.Agents(), "a send names the recipient")

	st.touched.Reset()
	_, err = st.Claim(ctx, recipient, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{recipient}, st.touched.Agents(), "a claim names the recipient")

	st.touched.Reset()
	require.NoError(t, st.Delete(ctx, []string{sent.ID}))
	assert.Equal(t, []string{recipient}, st.touched.Agents(), "a delete names the recipient of what it removed")
}
