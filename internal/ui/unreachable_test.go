package ui

import (
	"errors"
	"strings"
	"testing"

	"claude-agents/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnreachableTakesOverTheView is the regression test for a dead server
// reading as an empty store. A failed poll used to be swallowed, leaving the
// last good list on screen, so a server that had stopped answering looked like a
// panel whose chats had all been deleted.
func TestUnreachableTakesOverTheView(t *testing.T) {
	m := model{
		addr:  "127.0.0.1:8787",
		chats: []chatItem{{agent: store.Agent{Name: "still-here", State: store.StateDone}}},
	}

	// While the service answers, the list renders as usual.
	require.NotContains(t, m.View(), "Cannot reach the server")

	updated, _ := m.Update(chatsLoadedMsg{err: errors.New("dial tcp 127.0.0.1:8787: connect: connection refused")})
	m, ok := updated.(model)
	require.True(t, ok)

	view := m.View()
	assert.Contains(t, view, "Cannot reach the server")
	assert.Contains(t, view, "127.0.0.1:8787", "the address to go looking at is on screen")
	assert.Contains(t, view, "Nothing has been deleted.")
	assert.NotContains(t, view, "still-here", "the stale list is not shown as if it were current")
}

// TestUnreachableClearsOnceTheServiceAnswers confirms the panel heals on its own
// rather than needing to be reopened, since the poll keeps running underneath.
func TestUnreachableClearsOnceTheServiceAnswers(t *testing.T) {
	m := model{addr: "127.0.0.1:8787", unreachable: errors.New("connection refused")}

	updated, _ := m.Update(chatsLoadedMsg{items: []chatItem{{agent: store.Agent{Name: "back", State: store.StateNew}}}})
	m, ok := updated.(model)
	require.True(t, ok)

	assert.NoError(t, m.unreachable)
	assert.NotContains(t, m.View(), "Cannot reach the server")
}

func TestTruncateError(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{
			name: "a newline would break the single-line layout",
			msg:  "dial failed:\nconnection refused",
			want: "dial failed: connection refused",
		},
		{
			name: "a short error is left alone",
			msg:  "connection refused",
			want: "connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, truncateError(tt.msg))
		})
	}

	long := truncateError(strings.Repeat("x", errorWidth+50))
	assert.True(t, strings.HasSuffix(long, "…"), "an overlong error is cut rather than wrapped into a wall")
	assert.Len(t, long, errorWidth+len("…"))
}
