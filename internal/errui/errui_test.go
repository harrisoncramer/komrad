package errui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"weave/internal/caller"

	"connectrpc.com/connect"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScreenSections covers the layout every screen is built from: sections are separated
// only when they have content, so a screen carrying no details or hints doesn't render the
// blank lines they would have sat in.
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

const narrowPane = 59

var indent = strings.Repeat(" ", labelWidth)

const widePane = 100

// TestFold covers how text is folded to a pane, wrapping rather than truncating so the end of a
// dial error survives.
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

// TestScreensFitTheirPane covers the whole of every screen against the pane it draws into, prose
// and hints included.
func TestScreensFitTheirPane(t *testing.T) {
	err := errors.New("the agent server stopped during startup: failed to reach db: dial tcp 127.0.0.1:5460: connect: connection refused")

	tests := []struct {
		name     string
		rendered string
	}{
		{
			name:     "the config screen",
			rendered: RenderConfigError(narrowPane, err),
		},
		{
			name: "the startup screen",
			rendered: RenderStartupError(narrowPane, err, []Finding{{
				Name:   "readonly login",
				Reason: `the least-privilege login the MCP tools connect as does not work: password authentication failed for user "weave_readonly"`,
				Fix:    "weave readonly-role",
			}}),
		},
		{
			name: "the config screen carrying a detail of several lines",
			rendered: RenderConfigError(narrowPane, errors.New(
				"failed to parse config /home/u/.config/weave/config.toml: "+
					"default_harnes is not a setting; the top level takes default_harness, max_agents\n"+
					"layout.splitt is not a setting; [layout] takes sidebar_side, sidebar_width, info_side, info_height")),
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

// TestConfig covers the screen shown when the config file won't parse: the panel doesn't
// start, so this screen is all the reader gets, and has to carry the error itself and name
// the file to fix.
func TestConfig(t *testing.T) {
	err := errors.New("failed to parse config /home/u/.config/weave/config.toml: keybindings.list is not a setting; [keybindings] takes scripts, agents, archive, filter, form, help, nav, schedules, selection, workflows")

	rendered := strings.Join(strings.Fields(RenderConfigError(narrowPane, err)), " ")

	assert.Contains(t, rendered, "Your configuration could not be read")
	assert.Contains(t, rendered, "keybindings.list is not a setting")
	assert.Contains(t, rendered, "config.toml", "the file to go and fix is named")
}

// TestStartup covers the screen shown when the server wouldn't start: the reason it gave
// came out of its log and is the only thing on screen saying what failed, so it has to
// survive the render whole.
func TestStartup(t *testing.T) {
	err := errors.New("the agent server stopped during startup: failed to reach db: dial tcp 127.0.0.1:5460: connect: connection refused")

	rendered := strings.Join(strings.Fields(RenderStartupError(narrowPane, err, nil)), " ")

	assert.Contains(t, rendered, "The agent server could not be started")
	assert.Contains(t, rendered, "connect: connection refused")
	assert.Contains(t, rendered, "weave doctor",
		"with nothing found, the command that would find something is named")
}

// TestStartupCarriesTheFindings covers the difference between this screen and a log tail:
// the reason a server gave says what failed, while the findings say which dependency is
// missing and what repairs it, the only part anyone can act on.
func TestStartupCarriesTheFindings(t *testing.T) {
	err := errors.New("the agent server stopped during startup: failed to reach db read-only")

	rendered := strings.Join(strings.Fields(RenderStartupError(widePane, err, []Finding{{
		Name:   "readonly login",
		Reason: "the least-privilege login the MCP tools connect as does not work",
		Fix:    "weave readonly-role",
	}})), " ")

	assert.Contains(t, rendered, "readonly login")
	assert.Contains(t, rendered, "weave readonly-role", "the command that repairs it is on screen")
	assert.NotContains(t, rendered, "weave doctor",
		"nothing points at the doctor once the doctor has already answered")
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
			assert.Equal(t, tt.want, GetReason(tt.err))
		})
	}
}

// TestTraceOf covers the id the service returns on a failure, which is what turns a banner
// from a complaint into something a person can follow.
func TestTraceOf(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "an error that never reached the service carries none",
			err:  errors.New("dial tcp: connection refused"),
			want: "",
		},
		{
			name: "a refusal the service answered without one",
			err:  connect.NewError(connect.CodeNotFound, errors.New("no agent has that id")),
			want: "",
		},
		{
			name: "a refusal the service traced",
			err:  tracedError("019fd9f8-8327-7e4c-82b3-5339543f9e7e"),
			want: "019fd9f8-8327-7e4c-82b3-5339543f9e7e",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, GetTrace(tt.err))
		})
	}
}

func tracedError(trace string) error {
	err := connect.NewError(connect.CodeInternal, errors.New("the pane was already gone"))
	err.Meta().Set(caller.TraceHeader, trace)

	return err
}

// TestStartupOnAnEngineRunningOtherCode covers the screen a remote panel gets, where restarting
// anything over there is the one action that helps least and disturbs most.
func TestStartupOnAnEngineRunningOtherCode(t *testing.T) {
	err := fmt.Errorf("%w: it is serving v0.0.0-engine.8fe4a3b and this panel is v0.0.0-engine.16cbead4",
		ErrEngineBuild)

	rendered := strings.Join(strings.Fields(RenderStartupError(narrowPane, err, nil)), " ")

	assert.Contains(t, rendered, "The engine is running different code")
	assert.Contains(t, rendered, "every agent on it is still running")
	assert.Contains(t, rendered, "v0.0.0-engine.8fe4a3b")
	assert.NotContains(t, rendered, "The agent server could not be started")
}
