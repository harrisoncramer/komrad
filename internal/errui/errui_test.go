package errui

import (
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/charmbracelet/lipgloss"
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

// narrowPane is the width of the pane the panel runs in, the narrow half of the
// split beside the stage. Screens are asserted against it because that is the width
// the renderer above the panel cuts a line at.
const narrowPane = 59

// indent is the continuation prefix a value gets under a label that fits the column, which
// is what the fold tests below hand it.
var indent = strings.Repeat(" ", labelWidth)

// widePane is a popup with room for a finding's reason and its fix on their own lines,
// which is what the startup screen draws when the doctor found something.
const widePane = 100

// TestFold covers how text is folded to a pane. It wraps rather than truncating,
// since the useful part of a dial error is at the end.
func TestFold(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantLines bool
	}{
		{
			name: "a short value stays on one line",
			text: "connection refused",
		},
		{
			name:      "a long one wraps and keeps every word",
			text:      strings.TrimSpace(strings.Repeat("refused ", 60)),
			wantLines: true,
		},
		{
			name:      "a single word too long to fit is broken rather than left to overrun",
			text:      "/Users/someone/Library/Application Support/weave/configuration.toml",
			wantLines: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			folded := fold(tt.text, narrowPane, indent)

			assert.NotContains(t, folded, "…", "the text is folded rather than cut")
			assert.Equal(t, strings.Fields(tt.text), strings.Fields(strings.ReplaceAll(folded, "\n", "")),
				"every word survives, allowing for the breaks")

			lines := strings.Split(folded, "\n")
			if !tt.wantLines {
				assert.Len(t, lines, 1)

				return
			}

			require.Greater(t, len(lines), 1)
			for i, line := range lines {
				assert.LessOrEqual(t, lipgloss.Width(line), narrowPane, "line %d stays inside the pane", i)
				if i > 0 {
					assert.True(t, strings.HasPrefix(line, indent), "line %d is indented under the first", i)
				}
			}
		})
	}
}

// TestScreensFitTheirPane covers the whole of every screen against the pane it draws
// into, prose and hints included. The renderer above the panel cuts a line that
// overruns the pane rather than wrapping it, so anything unfolded loses its tail at
// the pane edge: on a hint that is the command it was naming, and on a detail it is
// the end of the error, which is the part saying what went wrong.
func TestScreensFitTheirPane(t *testing.T) {
	err := errors.New("the agent server stopped during startup: failed to reach db: dial tcp 127.0.0.1:5460: connect: connection refused")

	tests := []struct {
		name     string
		rendered string
	}{
		{
			name:     "the config screen",
			rendered: Config(narrowPane, err),
		},
		{
			name: "the startup screen",
			rendered: Startup(narrowPane, err, []Finding{{
				Name:   "readonly login",
				Reason: `the least-privilege login the MCP tools connect as does not work: password authentication failed for user "weave_readonly"`,
				Fix:    "weave readonly-role",
			}}),
		},
		{
			// A file with more than one key nothing answers to is reported a line per
			// key, so the screen has to hold a detail that already has breaks in it.
			name: "the config screen carrying a detail of several lines",
			rendered: Config(narrowPane, errors.New(
				"failed to parse config /home/u/.config/weave/config.toml: "+
					"default_harnes is not a setting; the top level takes default_harness, max_agents\n"+
					"layout.splitt is not a setting; [layout] takes sidebar_side, sidebar_width, info_side, info_height")),
		},
		{
			name:     "the unreachable screen",
			rendered: Unreachable(narrowPane, "127.0.0.1:8787", err),
		},
		{
			name:     "a refused write's banner",
			rendered: Banner{Summary: `Could not rename agent "api-refactor"`, Detail: Reason(err)}.Render(narrowPane),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i, line := range strings.Split(tt.rendered, "\n") {
				assert.LessOrEqual(t, lipgloss.Width(line), narrowPane, "line %d stays inside the pane", i)
			}
		})
	}
}

// TestConfig covers the screen shown when the config file will not parse. The panel
// does not start, so this screen is all the reader gets: it has to carry the error
// itself and name the file to go and fix.
func TestConfig(t *testing.T) {
	err := errors.New("failed to parse config /home/u/.config/weave/config.toml: keybindings.list is not a setting; [keybindings] takes actions, agents, archive, filter, form, help, nav, schedules, selection, workflows")

	// The error wraps at the screen width, so the text is compared with its line
	// breaks flattened away.
	rendered := strings.Join(strings.Fields(Config(narrowPane, err)), " ")

	assert.Contains(t, rendered, "Your configuration could not be read")
	assert.Contains(t, rendered, "keybindings.list is not a setting")
	assert.Contains(t, rendered, "config.toml", "the file to go and fix is named")
}

// TestStartup covers the screen shown when the server would not start. The reason it gave
// came out of its log and is the only thing on screen saying what failed, so it has to
// survive the render whole.
func TestStartup(t *testing.T) {
	err := errors.New("the agent server stopped during startup: failed to reach db: dial tcp 127.0.0.1:5460: connect: connection refused")

	// The error wraps at the screen width, so the text is compared with its line
	// breaks flattened away.
	rendered := strings.Join(strings.Fields(Startup(narrowPane, err, nil)), " ")

	assert.Contains(t, rendered, "The agent server could not be started")
	assert.Contains(t, rendered, "connect: connection refused")
	assert.Contains(t, rendered, "weave doctor",
		"with nothing found, the command that would find something is named")
}

// TestStartupCarriesTheFindings covers the difference between this screen and a log tail.
// The reason a server gave says what failed; the findings say which dependency is missing
// and what repairs it, which is the only part anyone can act on.
func TestStartupCarriesTheFindings(t *testing.T) {
	err := errors.New("the agent server stopped during startup: failed to reach db read-only")

	rendered := strings.Join(strings.Fields(Startup(widePane, err, []Finding{{
		Name:   "readonly login",
		Reason: "the least-privilege login the MCP tools connect as does not work",
		Fix:    "weave readonly-role",
	}})), " ")

	assert.Contains(t, rendered, "readonly login")
	assert.Contains(t, rendered, "weave readonly-role", "the command that repairs it is on screen")
	assert.NotContains(t, rendered, "weave doctor",
		"nothing points at the doctor once the doctor has already answered")
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
			rendered := Unreachable(narrowPane, tt.addr, errors.New("connection refused"))

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
	rendered := Banner{Summary: "Could not delete the agent"}.Render(narrowPane)

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
