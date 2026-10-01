package ui_test

import (
	"strings"
	"testing"

	"komrad/internal/panes"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLeader covers the leader and the keys that follow it.
func TestLeader(t *testing.T) {
	sidebar := "%0"

	t.Run("the leader opens its table from every pane of the panel's window alone", func(t *testing.T) {
		harness := openFocus(t)

		command, bound := harness.panel.ReadRootBinding("C-Space")
		require.True(t, bound)
		assert.Equal(t, []string{"if-shell", "-F", "#{==:#{window_id},@1}", "switch-client -T komrad-leader", "send-keys C-Space"}, command)

		command, bound = harness.panel.ReadTableBinding("komrad-leader", "j")
		require.True(t, bound)
		assert.Equal(t, []string{"send-keys", "-t", sidebar, "C-Space", "j"}, command)

		command, bound = harness.panel.ReadTableBinding("komrad-leader", "v")
		require.True(t, bound)
		assert.Equal(t, []string{"if-shell", "-F", "#{==:#{pane_id},%0}", "send-keys -t %0 C-Space v", "copy-mode"}, command,
			"any pane but the sidebar goes straight into copy-mode")

		leader := "C-Space"
		for _, key := range []string{"C-Space", "Space"} {
			command, bound := harness.panel.ReadTableBinding("komrad-leader", key)
			require.True(t, bound, key)
			assert.Equal(t, []string{"if-shell", "-F", "#{window_zoomed_flag}", "send-keys -t " + sidebar + " " + leader + " " + leader, "last-pane"}, command)
		}
	})

	t.Run("the leader twice with nowhere to go back to hands the keys to the next stop", func(t *testing.T) {
		harness, _ := openDocuments(t)

		harness.press("ctrl+space", "ctrl+space")

		assert.Equal(t, "/", strings.TrimSpace(readStage(harness.readFrame())[0]), "the document holds the keys")
	})

	t.Run("the leader twice goes back to the stop the keys last left", func(t *testing.T) {
		harness, _ := openDocuments(t)

		harness.press("ctrl+space", "ctrl+space", "ctrl+space", "ctrl+space")
		assert.NotEqual(t, "/", strings.TrimSpace(readStage(harness.readFrame())[0]), "the list holds the keys again")

		harness.press("ctrl+space", "ctrl+space")
		assert.Equal(t, "/", strings.TrimSpace(readStage(harness.readFrame())[0]), "the document holds the keys again")
	})

	t.Run("the leader twice returns to the record rather than moving on to the next stop", func(t *testing.T) {
		harness := openGroupedArchive(t, buildOneArchivedAgent()...)

		harness.press("j", "pick:record", "esc", "ctrl+space", "ctrl+space")

		assert.NotEmpty(t, readRecordCursor(harness.readFrame()), "the record holds the keys")
	})

	dormantOrchestrator := newOrchestrator()
	dormantOrchestrator.Alive = xptr.Ptr(false)
	dormantOrchestrator.State = agentv1.AgentState_AGENT_STATE_IDLE

	for _, tt := range []struct {
		name     string
		agent    *agentv1.Agent
		press    []string
		wantSent request
	}{
		{
			name:     "the leader twice over a dormant agent resumes it",
			agent:    newDormant("login"),
			press:    []string{"j"},
			wantSent: request{procedure: agentv1connect.AgentServiceResumeAgentProcedure, msg: &agentv1.ResumeAgentRequest{Id: "a1"}},
		},
		{
			name:     "the leader twice over a dormant orchestrator's workflow starts it",
			agent:    dormantOrchestrator,
			wantSent: request{procedure: agentv1connect.AgentServiceStartOrchestratorProcedure, msg: &agentv1.StartOrchestratorRequest{WorkflowId: "w1"}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			harness := newPanelHarness(t, panelHarnessParams{})
			answerWorktreeReads(harness)
			answerLastScreens(harness, map[string]string{tt.agent.GetId(): "⏺ the migration is written"})
			answerResume(harness)
			harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), tt.agent))
			harness.press(tt.press...)
			harness.fireTimers()

			harness.press("ctrl+space", "ctrl+space")

			harness.assertSentTo(tt.wantSent.procedure, tt.wantSent.msg)
			assert.NotEqual(t, "/", strings.TrimSpace(readStage(harness.readFrame())[0]), "the saved screen never holds the keys")
		})
	}

	t.Run("an unknown key after the leader is dropped", func(t *testing.T) {
		harness := openFocus(t)
		selected := len(harness.panel.ReadSelectedPanes())

		harness.press("ctrl+space", "enter")

		assert.Empty(t, harness.panel.ReadSelectedPanes()[selected:], "the key went to the leader")
	})
}

// TestPanePicker covers the letters the leader draws over every place the keys can land.
func TestPanePicker(t *testing.T) {
	sidebar, agent, info := "%0", "%9", "%77"

	window := func(panesShown ...panes.Rect) ([]panes.Rect, map[string]string) {
		screens := map[string]string{}
		for _, rect := range panesShown {
			screens[rect.Pane] = "the screen of " + rect.Pane + "\n"
		}

		return panesShown, screens
	}
	sidebarRect := panes.Rect{
		Pane:   sidebar,
		Width:  140,
		Height: 40,
	}
	agentRect := panes.Rect{
		Pane:   agent,
		Left:   141,
		Width:  80,
		Height: 28,
	}
	infoRect := panes.Rect{
		Pane:   info,
		Left:   141,
		Top:    29,
		Width:  80,
		Height: 11,
	}

	tests := []struct {
		name         string
		shown        []panes.Rect
		from         string
		press        []string
		wantSelected []string
		wantRecord   bool
		wantFrozen   bool
	}{
		{
			name:         "the agent's letter lands the keys on its pane",
			shown:        []panes.Rect{sidebarRect, agentRect},
			press:        []string{"j"},
			wantSelected: []string{agent},
		},
		{
			name:       "the details letter lands on the record",
			shown:      []panes.Rect{sidebarRect, agentRect},
			press:      []string{"f"},
			wantRecord: true,
		},
		{
			name:         "the info pane gets a letter of its own",
			shown:        []panes.Rect{sidebarRect, agentRect, infoRect},
			press:        []string{"k"},
			wantSelected: []string{info},
		},
		{
			name:       "no info pane leaves its letter unused",
			shown:      []panes.Rect{sidebarRect, agentRect},
			press:      []string{"k"},
			wantFrozen: true,
		},
		{
			name:         "esc gives the keys back to the pane that had them",
			shown:        []panes.Rect{sidebarRect, agentRect},
			from:         agent,
			press:        []string{"esc"},
			wantSelected: []string{agent},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openFocus(t)
			harness.panel.SetScreens(window(tt.shown...))
			if tt.from != "" {
				harness.panel.SetActivePane(tt.from)
			}

			harness.press("ctrl+space", "j")
			require.Contains(t, harness.readDrawn(), "JUMP")
			selected := len(harness.panel.ReadSelectedPanes())

			harness.press(tt.press...)

			assert.Equal(t, tt.wantSelected, harness.panel.ReadSelectedPanes()[selected:])
			assert.Equal(t, tt.wantFrozen, harness.panel.IsZoomed())
			if !tt.wantFrozen {
				assert.Equal(t, tt.wantRecord, readRecordCursor(harness.readFrame()) != "")
			}
		})
	}

	t.Run("each letter is drawn over its own target on blank cells", func(t *testing.T) {
		harness := openFocus(t)
		harness.panel.SetScreens(window(sidebarRect, agentRect, infoRect))

		harness.press("ctrl+space", "j")

		lines := strings.Split(harness.readDrawn(), "\n")
		rows := func(left, right int) []int {
			var found []int
			for row, line := range lines {
				if strings.Contains(ansi.Cut(line, left, right), "██") {
					found = append(found, row)
				}
			}

			return found
		}

		assert.NotEmpty(t, rows(0, 140), "the sidebar's blocks carry letters")
		agentRows, infoRows := rows(141, 221), []int{}
		for _, row := range agentRows {
			if row >= 29 {
				infoRows = append(infoRows, row)
			}
		}
		assert.Len(t, infoRows, 5, "the info pane carries one letter five rows tall")
		assert.Len(t, agentRows, 10, "the agent and the info pane carry one letter each")
	})
}
