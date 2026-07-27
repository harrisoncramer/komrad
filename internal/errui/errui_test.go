package errui

import (
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScreenSections covers the layout every screen is built from. Sections are
// separated only when they have content, so a screen carrying no details or hints
// does not render the blank lines they would have sat in.
func TestScreenSections(t *testing.T) {
	tests := []struct {
		name   string
		screen Screen
		want   []string
	}{
		{
			name:   "a screen with only a title and body",
			screen: Screen{Title: "Something broke", Body: []string{"One line."}},
			want:   []string{"Something broke", "", "One line."},
		},
		{
			name: "details sit under the body they explain",
			screen: Screen{
				Title:   "Something broke",
				Body:    []string{"One line."},
				Details: []Detail{{Label: "error", Value: "boom"}},
			},
			want: []string{"Something broke", "", "One line.", "", "error    boom"},
		},
		{
			name: "hints close the screen, separated from the detail above them",
			screen: Screen{
				Title:   "Something broke",
				Details: []Detail{{Label: "error", Value: "boom"}},
				Hints:   []string{"Try again."},
			},
			want: []string{"Something broke", "", "error    boom", "", "Try again."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered := strings.TrimRight(tt.screen.Render(), "\n")

			assert.Equal(t, tt.want, strings.Split(rendered, "\n"))
		})
	}
}

// TestWrap covers how a reported value is folded. It wraps rather than
// truncating, since the useful part of a dial error is at the end.
func TestWrap(t *testing.T) {
	longWords := strings.Split(strings.TrimSpace(strings.Repeat("refused ", 60)), " ")

	tests := []struct {
		name      string
		words     []string
		wantLines bool
	}{
		{
			name:  "a short value stays on one line",
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
			wrapped := Wrap(strings.Join(tt.words, " "))

			assert.NotContains(t, wrapped, "…", "the text is wrapped rather than cut")
			assert.Equal(t, tt.words, strings.Fields(wrapped), "every word survives")

			lines := strings.Split(wrapped, "\n")
			if !tt.wantLines {
				assert.Len(t, lines, 1)

				return
			}

			require.Greater(t, len(lines), 1)
			for i, line := range lines {
				assert.LessOrEqual(t, len(strings.TrimRight(line, " ")), Width+len(Indent),
					"line %d stays within the width", i)
				if i > 0 {
					assert.True(t, strings.HasPrefix(line, Indent), "line %d is indented under the first", i)
				}
			}
		})
	}
}

// TestConfig covers the screen shown when the config file will not parse. The panel
// does not start, so this screen is all the reader gets: it has to carry the error
// itself and name the file to go and fix.
func TestConfig(t *testing.T) {
	err := errors.New("failed to parse config /home/u/.config/claude-agents/config.toml: 'keybindings' has invalid keys: list")

	// The error wraps at the screen width, so the text is compared with its line
	// breaks flattened away.
	rendered := strings.Join(strings.Fields(Config(err)), " ")

	assert.Contains(t, rendered, "Your configuration could not be read")
	assert.Contains(t, rendered, "invalid keys: list")
	assert.Contains(t, rendered, "config.toml", "the file to go and fix is named")
}

// TestStartup covers the screen shown when the server would not start. The reason
// it gave came out of its log and is the only thing on screen saying what to fix, so
// it has to survive the render whole.
func TestStartup(t *testing.T) {
	err := errors.New("the agent server stopped during startup: failed to reach db: dial tcp 127.0.0.1:5460: connect: connection refused")

	// The error wraps at the screen width, so the text is compared with its line
	// breaks flattened away.
	rendered := strings.Join(strings.Fields(Startup(err)), " ")

	assert.Contains(t, rendered, "The agent server could not be started")
	assert.Contains(t, rendered, "connect: connection refused")
	assert.Contains(t, rendered, "mise run docker:up", "the way to bring the database up is named")
}

// TestUnreachable covers that the address is shown when there is one and left out
// when there is not, rather than rendering an empty labelled row.
func TestUnreachable(t *testing.T) {
	tests := []struct {
		name       string
		addr       string
		wantAbsent string
	}{
		{name: "with an address, it is on screen", addr: "127.0.0.1:8787"},
		{name: "without one, no address row is rendered", wantAbsent: "address"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered := Unreachable(tt.addr, errors.New("connection refused"))

			assert.Contains(t, rendered, "Cannot reach the server")
			assert.Contains(t, rendered, "connection refused")

			if tt.addr != "" {
				assert.Contains(t, rendered, tt.addr)
			}
			if tt.wantAbsent != "" {
				assert.NotContains(t, rendered, tt.wantAbsent)
			}
		})
	}
}

// TestBannerRendersWithoutAReason covers a service that fails without saying why,
// which would otherwise render as a red line with nothing under it.
func TestBannerRendersWithoutAReason(t *testing.T) {
	rendered := Banner{Summary: "Could not delete the agent"}.Render()

	assert.Contains(t, rendered, "no reason given")
	assert.NotContains(t, strings.Split(rendered, "\n")[1], "esc", "the reason line is its own line")
}

// TestReason covers reducing a service error to the part worth reading.
func TestReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "a connect error shows its message, not its code",
			err:  connect.NewError(connect.CodeAlreadyExists, errors.New("name is taken")),
			want: "name is taken",
		},
		{
			name: "a plain error shows itself",
			err:  errors.New("dial tcp: connection refused"),
			want: "dial tcp: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Reason(tt.err))
		})
	}
}
