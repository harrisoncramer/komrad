package ui

import (
	"strings"
	"testing"
	"time"

	"claude-agents/internal/appconfig"

	agentstore "claude-agents/internal/store/agent"
	workflowstore "claude-agents/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInfoKey covers the info toggle. A live agent's pane occupies the stage, so
// showing its stored record means putting the record there instead and putting
// the pane back afterwards.
func TestInfoKey(t *testing.T) {
	tests := []struct {
		name       string
		cursor     int
		presses    int
		thenMove   bool
		wantPinned bool
		wantBat    int
		wantSwaps  int
	}{
		{
			name:    "on a workflow header there is no record to show",
			cursor:  0,
			presses: 1,
		},
		{
			name:       "one press puts the record in the stage",
			cursor:     1,
			presses:    1,
			wantPinned: true,
			wantBat:    1,
		},
		{
			name:      "a second press swaps the agent's pane back in",
			cursor:    1,
			presses:   2,
			wantBat:   1,
			wantSwaps: 1,
		},
		{
			// Two renders: the pinned record, then the workflow header the cursor
			// lands on, which stages a record of its own.
			name:     "moving the cursor unpins, so the record never outlives its row",
			cursor:   1,
			presses:  1,
			thenMove: true,
			wantBat:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &recordingTmux{}
			m := infoModel(tmux)
			m.agentCursor = tt.cursor

			for range tt.presses {
				m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
			}
			if tt.thenMove {
				m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyUp})
			}

			assert.Equal(t, tt.wantPinned, m.infoPinned)
			assert.Equal(t, tt.wantSwaps, tmux.swaps, "swaps move the agent's own pane")

			bat := 0
			for _, respawn := range tmux.respawns {
				if strings.Contains(respawn, "bat") {
					bat++
				}
			}
			assert.Equal(t, tt.wantBat, bat, "the record is rendered through bat")
		})
	}
}

// TestAgentMarkdown covers the record the stage shows for an agent. A dormant one
// has no pane to display, so this is all there is of it.
func TestAgentMarkdown(t *testing.T) {
	agent := agentstore.Agent{
		Name:            "auth",
		Pane:            "%9",
		Cwd:             "/repo",
		ClaudeSessionID: "abc",
		CreatedAt:       time.Date(2026, 7, 24, 15, 4, 0, 0, time.UTC),
	}

	tests := []struct {
		name  string
		alive bool
		// schedule is the name of the schedule that launched the agent, empty for
		// one a person started.
		schedule    string
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:  "a dormant agent says so and offers the way back",
			alive: false,
			wantContain: []string{
				"_dormant — not running_",
				"Press open to restore",
			},
		},
		{
			name:  "a live agent says where it runs",
			alive: true,
			wantContain: []string{
				"_running in %9_",
				"- directory: /repo",
				"- resume: abc",
				"- flags: —",
			},
			wantAbsent: []string{"Press open to restore"},
		},
		{
			name:        "an agent a schedule launched names the schedule",
			alive:       true,
			schedule:    "morning-ci",
			wantContain: []string{"- launched by: morning-ci"},
		},
		{
			name:       "one a person started claims no schedule",
			alive:      true,
			wantAbsent: []string{"launched by"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agentMarkdown(agent, tt.alive, tt.schedule)

			for _, want := range tt.wantContain {
				assert.Contains(t, got, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, got, absent)
			}
		})
	}
}

// recordingTmux records only what the stage does, so a test can tell a
// swapped-in agent pane from a respawned bat preview. Every other call is an
// inert stub.
type recordingTmux struct {
	// path is what ClientPath reports, the directory the panel falls back to for
	// a form left blank.
	path     string
	swaps    int
	respawns []string
}

func (t *recordingTmux) SetGlobal(string, string)                {}
func (t *recordingTmux) ShowGlobal(string) string                { return "" }
func (t *recordingTmux) SetSessionOption(string, string, string) {}
func (t *recordingTmux) DetachSession(string)                    {}
func (t *recordingTmux) BindKey(string, ...string)               {}
func (t *recordingTmux) ClientPath(string) string                { return t.path }
func (t *recordingTmux) SendKeys(string, ...string) error        { return nil }
func (t *recordingTmux) PaneAlive(string) bool                   { return true }
func (t *recordingTmux) CapturePane(string) string               { return "" }
func (t *recordingTmux) CurrentPane() (string, error)            { return "%0", nil }
func (t *recordingTmux) SplitRight(string, string) string        { return "%1" }
func (t *recordingTmux) SwapPanes(string, string)                { t.swaps++ }
func (t *recordingTmux) KillPane(string)                         {}

func (t *recordingTmux) NewWindow(string, string, string) (string, error) {
	return "%2", nil
}

func (t *recordingTmux) RespawnPane(_, _ string, argv ...string) {
	t.respawns = append(t.respawns, strings.Join(argv, " "))
}

// infoModel builds an agents model holding one live agent, with the cursor on
// it. Row 0 is the workflow header the agent sits under, so the agent is row 1.
func infoModel(tmux *recordingTmux) model {
	stage := NewStage(tmux)
	stage.Init()

	return model{
		stage:     stage,
		keys:      appconfig.DefaultKeybindings(),
		screen:    screenAgents,
		workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
		agents: []agentItem{
			{agent: agentstore.Agent{Name: "auth", Pane: "%9", Cwd: "/repo", WorkflowID: "w1"}, alive: true},
		},
		agentCursor: 1,
	}
}

// pressKey feeds one key to the agents screen and returns the model it produced.
func pressKey(t *testing.T, m model, key tea.KeyMsg) model {
	t.Helper()

	next, _ := m.updateAgents(key)
	updated, ok := next.(model)
	require.True(t, ok, "want a panel model back, got %T", next)

	return updated
}
