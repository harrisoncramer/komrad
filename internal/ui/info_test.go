package ui

import (
	"strings"
	"testing"
	"time"

	"weave/config"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInfoDefault covers the info pane's automatic open and close. It opens on an agent row
// unless it has been dismissed, stays closed on a workflow header, which has no record of
// its own, and is dismissed for good once the pane itself is killed rather than reopened the
// next time an agent is hovered.
func TestInfoDefault(t *testing.T) {
	t.Run("an agent row opens the record below by default", func(t *testing.T) {
		tmux := &recordingTmux{}
		m := infoModel(tmux)

		m.stageSelection()

		assert.Equal(t, infoRecord, m.info.kind)
		assert.Equal(t, m.frame.GuestPane(), "%9", "the record never displaces the agent")
		require.Len(t, tmux.respawns, 1)
		assert.Contains(t, tmux.respawns[0], "less")
		assert.Equal(t, "%2", tmux.respawnPanes[0], "the record is paged into the pane below")
	})

	t.Run("a workflow header has no record of its own to show", func(t *testing.T) {
		tmux := &recordingTmux{}
		m := infoModel(tmux)
		m.tree.cursor = 0

		m.stageSelection()

		assert.Equal(t, infoNothing, m.info.kind)
		assert.NotContains(t, tmux.respawnPanes, "%2", "nothing is paged into the pane below")
	})

	t.Run("moving from an agent to a header closes what was open", func(t *testing.T) {
		tmux := &recordingTmux{}
		m := infoModel(tmux)
		m.stageSelection()
		require.Equal(t, infoRecord, m.info.kind)

		m.tree.cursor = 0
		m.stageSelection()

		assert.Equal(t, infoNothing, m.info.kind)
		assert.Contains(t, tmux.killed, "%2", "the pane goes with it")
	})

	t.Run("killing the pane dismisses it for the rest of the session", func(t *testing.T) {
		tmux := &recordingTmux{}
		m := infoModel(tmux)
		m.stageSelection()
		require.Equal(t, infoRecord, m.info.kind)

		tmux.deadPanes = map[string]bool{"%2": true}
		m.reconcileInfo()

		assert.Equal(t, infoNothing, m.info.kind)
		assert.True(t, m.info.dismissed)

		m.stageSelection()
		assert.Equal(t, infoNothing, m.info.kind, "an agent row does not reopen it")
	})
}

// TestAgentMarkdown covers the record the stage shows for an agent. A dormant one
// has no pane to display, so this is all there is of it.
func TestAgentMarkdown(t *testing.T) {
	agent := agentstore.Agent{
		ID:        "a1",
		Name:      "auth",
		Pane:      "%9",
		Cwd:       "/repo",
		SessionID: "abc",
		CreatedAt: time.Date(2026, 7, 24, 15, 4, 0, 0, time.UTC),
	}

	tests := []struct {
		name  string
		alive bool
		// schedule is the name of the schedule that launched the agent, empty for
		// one a person started.
		schedule string
		// metadata is what the agent recorded about its own work, empty for one that
		// recorded nothing.
		metadata    []agentstore.Metadata
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:  "a dormant agent says so and offers the way back",
			alive: false,
			wantContain: []string{
				"_dormant — not running_",
				"Press open to resume",
			},
		},
		{
			name:  "a live agent claims no dormancy and lists its record",
			alive: true,
			wantContain: []string{
				"1. agent: —",
				"2. directory: /repo",
				"3. flags: —",
				"4. agent_id: a1",
				"5. session_id: abc",
			},
			wantAbsent: []string{"dormant", "Press open to resume"},
		},
		{
			name:        "an agent a schedule launched names the schedule",
			alive:       true,
			schedule:    "morning-ci",
			wantContain: []string{"launched by: morning-ci"},
		},
		{
			name:       "one a person started claims no schedule",
			alive:      true,
			wantAbsent: []string{"launched by"},
		},
		{
			name:       "an agent that recorded nothing adds nothing for it",
			alive:      true,
			wantAbsent: []string{"branch", "pull"},
		},
		{
			name:  "what the agent recorded is numbered in with the rest and copyable",
			alive: true,
			metadata: []agentstore.Metadata{
				{Key: "branch", Value: "harry/rate-limits"},
				{Key: "pr", Value: "https://github.com/o/r/pull/312"},
			},
			wantContain: []string{
				"branch: harry/rate-limits",
				"pr: https://github.com/o/r/pull/312",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := agent
			agent.Metadata = tt.metadata

			got := agentMarkdown(agent, tt.alive, tt.schedule)

			for _, want := range tt.wantContain {
				assert.Contains(t, got.markdown, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, got.markdown, absent)
			}

			if len(tt.metadata) > 0 {
				assert.Contains(t, got.values, "harry/rate-limits", "the metadata value is copyable")
				assert.Contains(t, got.values, "https://github.com/o/r/pull/312", "the metadata value is copyable")
			}
		})
	}
}

// TestWorkflowMarkdown covers the record the stage shows for a workflow. The stage is
// the one place a person can read the stored defaults without opening the edit form.
func TestWorkflowMarkdown(t *testing.T) {
	workflow := workflowstore.Workflow{
		ID:            "w1",
		Name:          "api",
		Context:       "shared notes",
		Harness:       "codex",
		Cwd:           "/repo/api",
		Flags:         "--model opus",
		SetupAction:   "bootstrap",
		CleanupAction: "teardown",
	}

	got := workflowMarkdown(workflow, 3)

	wantContain := []string{
		"# api",
		"shared notes",
		"1. agents: 3",
		"2. default agent: codex",
		"3. directory: /repo/api",
		"4. flags: --model opus",
		"5. setup: bootstrap",
		"6. cleanup: teardown",
	}

	for _, want := range wantContain {
		assert.Contains(t, got.markdown, want)
	}
	assert.Equal(t, []string{"3", "codex", "/repo/api", "--model opus", "bootstrap", "teardown"}, got.values)
}

// recordingTmux records what the panel puts on its frame, so a test can tell a swapped-in
// agent pane from a respawned pager preview. How the frame itself is laid out is the layout
// package's own to cover, so nothing about splits or sizes is kept here. Every other call is
// an inert stub.
type recordingTmux struct {
	// path is what GetClientPath reports, the directory the panel falls back to for
	// a form left blank.
	path     string
	swaps    int
	respawns []string
	// width is what GetPaneWidth reports for every pane, the columns markdown renders to.
	width int
	// windows are the windows opened for the user's own client, each recorded as its
	// directory followed by the argv it runs. openWindowErr makes the next one fail.
	windows       []string
	openWindowErr error
	// detaches counts the sessions the popup has been closed by detaching from.
	detaches int
	// killed are the panes that have been torn down.
	killed []string
	// belowAnchors are the panes the info pane has been split beneath.
	belowAnchors []string
	// globals are the tmux options the panel has recorded, which is how an action is told
	// which pane the panel laid out for it.
	globals map[string]string
	// respawnPanes are the panes each respawn went to, alongside the argv in respawns.
	respawnPanes []string
	// deadPanes are the panes IsPaneAlive answers no for, which is how a test stands a pane
	// up and then takes it away.
	deadPanes map[string]bool
	// resizes are the shares the stage has been sized to, which is how a re-read layout is
	// seen to have reached tmux.
	resizes []int
}

func (t *recordingTmux) ShowGlobal(string) string                 { return "" }
func (t *recordingTmux) SetSessionOption(string, string, string)  {}
func (t *recordingTmux) DetachSession(string)                     { t.detaches++ }
func (t *recordingTmux) BindKey(string, ...string)                {}
func (t *recordingTmux) GetClientPath(string) string              { return t.path }
func (t *recordingTmux) SendKeys(string, ...string) error         { return nil }
func (t *recordingTmux) IsPaneAlive(pane string) bool             { return !t.deadPanes[pane] }
func (t *recordingTmux) GetPaneWidth(string) int                  { return t.width }
func (t *recordingTmux) CapturePane(string) string                { return "" }
func (t *recordingTmux) SplitRight(string, string, string) string { return "%1" }
func (t *recordingTmux) SplitLeft(string, string, string) string  { return "%1" }

func (t *recordingTmux) SplitBelow(anchor, _, _ string) string {
	t.belowAnchors = append(t.belowAnchors, anchor)

	return "%2"
}

func (t *recordingTmux) SplitAbove(anchor, _, _ string) string {
	t.belowAnchors = append(t.belowAnchors, anchor)

	return "%2"
}

func (t *recordingTmux) SetPanelGlobal(name, value string) {
	t.SetGlobal(name, value)
}

func (t *recordingTmux) SetGlobal(name, value string) {
	if t.globals == nil {
		t.globals = map[string]string{}
	}

	t.globals[name] = value
}

func (t *recordingTmux) SwapPanes(string, string)        { t.swaps++ }
func (t *recordingTmux) GetSiblingPanes(string) []string { return nil }
func (t *recordingTmux) GetPaneOwner(string) string      { return "" }
func (t *recordingTmux) BreakPane(string)                {}
func (t *recordingTmux) KillPane(pane string)            { t.killed = append(t.killed, pane) }

func (t *recordingTmux) NewWindow(string, string, string) (string, error) {
	return "%2", nil
}

func (t *recordingTmux) OpenWindow(_, _, dir string, argv ...string) error {
	if t.openWindowErr != nil {
		return t.openWindowErr
	}

	t.windows = append(t.windows, strings.Join(append([]string{dir}, argv...), " "))

	return nil
}

func (t *recordingTmux) ResizePane(_ string, percent int) {
	t.resizes = append(t.resizes, percent)
}

func (t *recordingTmux) RespawnPane(pane, _ string, argv ...string) {
	t.respawns = append(t.respawns, strings.Join(argv, " "))
	t.respawnPanes = append(t.respawnPanes, pane)
}

// infoModel builds an agents model holding one live agent, with the cursor on
// it. Row 0 is the workflow header the agent sits under, so the agent is row 1.
func infoModel(tmux *recordingTmux) model {
	frame := layout.New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	return model{
		tmux:   tmux,
		frame:  frame,
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{
				{agent: agentstore.Agent{Name: "auth", Pane: "%9", Cwd: "/repo", WorkflowID: "w1"}, alive: true},
			},
			rowCursor: rowCursor{cursor: 1},
		},
	}
}
