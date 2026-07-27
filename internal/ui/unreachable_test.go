package ui

import (
	"errors"
	"strings"
	"testing"

	agentstore "claude-agents/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnreachableView covers what the panel shows when the service stops
// answering. A swallowed poll would leave the last good list on screen, so a
// dead server would read as a panel whose agents had all been deleted.
func TestUnreachableView(t *testing.T) {
	tests := []struct {
		name         string
		start        error
		msg          agentsLoadedMsg
		wantTakeover bool
	}{
		{
			name: "while the service answers, the list renders as usual",
			msg:  agentsLoadedMsg{items: []agentItem{{agent: agentstore.Agent{Name: "still-here", State: agentstore.StateNew}}}},
		},
		{
			name:         "a failed poll takes the screen over",
			msg:          agentsLoadedMsg{err: errors.New("dial tcp 127.0.0.1:8787: connect: connection refused")},
			wantTakeover: true,
		},
		{
			name:  "and it clears itself once the service answers again",
			start: errors.New("connection refused"),
			msg:   agentsLoadedMsg{items: []agentItem{{agent: agentstore.Agent{Name: "back", State: agentstore.StateNew}}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				addr:        "127.0.0.1:8787",
				unreachable: tt.start,
				agents:      []agentItem{{agent: agentstore.Agent{Name: "still-here", State: agentstore.StateDone}}},
			}

			updated, _ := m.Update(tt.msg)
			m, ok := updated.(model)
			require.True(t, ok)

			view := m.View()
			if !tt.wantTakeover {
				assert.NoError(t, m.unreachable)
				assert.NotContains(t, view, "Cannot reach the server")

				return
			}

			assert.Contains(t, view, "Cannot reach the server")
			assert.Contains(t, view, "127.0.0.1:8787", "the address to go looking at is on screen")
			assert.Contains(t, view, "Nothing has been deleted.")
			assert.NotContains(t, view, "still-here", "the stale list is not shown as if it were current")
		})
	}
}

// TestWrapError covers the error shown on that screen. It wraps rather than
// truncating, since the useful part of a dial error is at the end.
func TestWrapError(t *testing.T) {
	longWords := strings.Split(strings.TrimSpace(strings.Repeat("refused ", 60)), " ")

	tests := []struct {
		name      string
		words     []string
		wantLines bool
	}{
		{
			name:  "a short error stays on one line",
			words: []string{"connection", "refused"},
		},
		{
			name:      "a long one wraps and keeps every word",
			words:     longWords,
			wantLines: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := wrapError(strings.Join(tt.words, " "))

			assert.NotContains(t, wrapped, "…", "the text is wrapped rather than cut")
			assert.Equal(t, tt.words, strings.Fields(wrapped), "every word survives")

			lines := strings.Split(wrapped, "\n")
			if !tt.wantLines {
				assert.Len(t, lines, 1)
				return
			}

			require.Greater(t, len(lines), 1)
			for i, line := range lines {
				assert.LessOrEqual(t, len(strings.TrimRight(line, " ")), errorWidth+len(errorIndent),
					"line %d stays within the width", i)
				if i > 0 {
					assert.True(t, strings.HasPrefix(line, errorIndent), "line %d is indented under the first", i)
				}
			}
		})
	}
}
