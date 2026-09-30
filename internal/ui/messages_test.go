package ui_test

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"komrad/internal/caller"
	"komrad/internal/engine"
	"komrad/internal/ui"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
)

const traceID = "7c1f2a94"

var landedAt = regexp.MustCompile(`[A-Z][a-z]{2} \d{1,2} at \d{1,2}:\d{2}[ap]m`)

// TestMessagesHeader covers a message flashing on the engine line and what it leaves there.
func TestMessagesHeader(t *testing.T) {
	t.Run("a message flashes its summary beside the engine", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{sidebarWidth: 60})

		harness.press("enter")

		assert.Equal(t, " local  ✗ Could not resume agent \"api-refactor\"", readEngineLine(harness.readFrame()))
	})

	t.Run("a summary wider than the pane is clipped", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{sidebarWidth: 33})

		harness.press("enter")

		assert.Equal(t, " local  ✗ Could not resume a…", readEngineLine(harness.readFrame()))
	})

	t.Run("an error left standing is counted once the flash is over", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{sidebarWidth: 60})

		harness.press("enter", "enter")
		settleFlash(harness)

		assert.Equal(t, " local  ✗ 2  C-g", readEngineLine(harness.readFrame()))
	})

	t.Run("dismissing the message that landed ends its flash", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{sidebarWidth: 60})

		harness.press("enter", "enter", "ctrl+g", "d", "esc")

		assert.Equal(t, "\uf444 local  ✗ 1  C-g", readEngineLine(harness.readFrame()))
	})

	t.Run("an info message leaves nothing once the flash is over", func(t *testing.T) {
		harness := openWorktrees(t)

		harness.press("D", "y")
		require.Contains(t, readEngineLine(harness.readFrame()), "·")
		settleFlash(harness)

		assert.Equal(t, " local", readEngineLine(harness.readFrame()))
	})

	t.Run("a pane too short for the header flashes on the row the trail is drawn on", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{height: 13})

		harness.press("enter")
		assert.Equal(t, "✗ Could not resume agent \"api-refactor\"", readEngineRow(harness.readFrame(), 0))

		settleFlash(harness)
		assert.NotContains(t, harness.readFrame(), "Could not resume")
	})
}

// TestMessagesPane covers every message read in the main pane.
func TestMessagesPane(t *testing.T) {
	t.Run("the key is refused with nothing to read", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{})

		harness.press("ctrl+g", "k")

		assert.Equal(t, "backend", readHoveredRow(harness.readFrame()), "the list kept the keys")
	})

	t.Run("the whole message is drawn in the main pane with the list still beside it", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{})

		harness.press("enter", "ctrl+g")

		frame := harness.readFrame()
		pane := joinProse(readStage(frame))
		assert.Contains(t, pane, "messages 1 of 1")
		assert.Contains(t, pane, `Could not resume agent "api-refactor"`)
		assert.Contains(t, pane, "no such agent")
		assert.Contains(t, pane, "error ·")
		assert.Contains(t, pane, "komrad log "+traceID)
		assert.Equal(t, "  api-refactor", readHoveredRow(frame))
		assert.Contains(t, readLegend(frame), "d dismiss")
	})

	t.Run("the cursor walks from the newest message back", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{})
		harness.press("enter", "enter", "enter", "ctrl+g")

		var tallies []string
		for _, key := range []string{"j", "j", "j", "k"} {
			tallies = append(tallies, readPaneTally(harness.readFrame()))
			harness.press(key)
		}
		tallies = append(tallies, readPaneTally(harness.readFrame()))

		assert.Equal(t, []string{"1 of 3", "2 of 3", "3 of 3", "3 of 3", "2 of 3"}, tallies)
	})

	t.Run("the message under the cursor is drawn whole in a short pane", func(t *testing.T) {
		for height := 9; height <= 16; height++ {
			harness := openMessages(t, panelHarnessParams{height: height})
			harness.press("enter", "enter", "enter", "ctrl+g")

			for at := range 3 {
				stage := readStage(harness.readFrame())
				caret := slices.IndexFunc(stage, func(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), "›") })
				require.GreaterOrEqual(t, caret, 0, "message %d at height %d", at, height)
				require.Less(t, caret+2, len(stage), "message %d at height %d", at, height)
				assert.Contains(t, stage[caret+2], "komrad log "+traceID, "message %d at height %d", at, height)
				harness.press("j")
			}
		}
	})

	t.Run("d dismisses the message under the cursor and the last one hands the keys back", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{})
		harness.press("enter", "enter", "ctrl+g", "d")

		assert.Equal(t, "1 of 1", readPaneTally(harness.readFrame()))

		harness.press("d", "k")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "Could not resume")
		assert.Equal(t, "backend", readHoveredRow(frame))
	})

	t.Run("D dismisses every message", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{sidebarWidth: 60})
		harness.press("enter", "enter", "ctrl+g", "D")
		settleFlash(harness)

		frame := harness.readFrame()
		assert.NotContains(t, frame, "Could not resume")
		assert.Equal(t, " local", readEngineLine(frame))
	})

	t.Run("esc hands the keys back and keeps the messages", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{sidebarWidth: 60})
		harness.press("enter", "ctrl+g", "esc", "k")
		settleFlash(harness)

		frame := harness.readFrame()
		assert.Equal(t, "backend", readHoveredRow(frame))
		assert.Equal(t, " local  ✗ 1  C-g", readEngineLine(frame))
	})

	t.Run("a message landing while the pane is read leaves the reader where they were", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{})

		harness.press("enter", "ctrl+g", "M")

		assert.Equal(t, "2 of 2", readPaneTally(harness.readFrame()))
	})

	t.Run("the messages stand in for a form opened over the list", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{})

		harness.press("enter", "c", "ctrl+g")

		stage := joinProse(readStage(harness.readFrame()))
		assert.Contains(t, stage, "messages 1 of 1")
		assert.Contains(t, stage, "komrad log "+traceID)
	})

	t.Run("the unreachable server says what to do about it", func(t *testing.T) {
		harness := openUnreachable(t, engine.Remote{})

		harness.press("ctrl+g")

		pane := joinProse(readStage(harness.readFrame()))
		assert.Contains(t, pane, "connection refused")
		assert.Contains(t, pane, "Start it with: komrad serve")
	})
}

// TestMessagesCopy covers y putting the message under the cursor on the clipboard.
func TestMessagesCopy(t *testing.T) {
	harness := openMessages(t, panelHarnessParams{})
	harness.press("enter", "ctrl+g", "y")

	assert.Equal(t, []string{"Could not resume agent \"api-refactor\"\n\nno such agent\n\nkomrad log " + traceID}, harness.panel.ReadCopied())
}

// TestMessagesBlink covers a message blinking in the header as it lands.
func TestMessagesBlink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		press []string
	}{
		{
			name:  "a message blinks twice and settles as it found the row",
			press: []string{"enter"},
		},
		{
			name:  "two messages landing together blink at one speed",
			press: []string{"enter", "enter"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openMessages(t, panelHarnessParams{sidebarWidth: 60})
			harness.press(tt.press...)

			var heads []string
			for range 4 {
				heads = append(heads, splitLines(harness.readRawFrame())[1])
				harness.firePulse()
			}

			lit, settled := heads[0], heads[3]
			assert.NotEqual(t, lit, settled)
			assert.Equal(t, []string{lit, settled, lit, settled}, heads)
			assert.Equal(t, ansi.Strip(lit), ansi.Strip(settled), "the blink changes no words")
		})
	}
}

// TestMessagesGolden draws the header and the main pane whole, colours included.
func TestMessagesGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		open func(t *testing.T) *panelHarness
		read func(harness *panelHarness) string
	}{
		{
			name: "an-error-flashing",
			open: func(t *testing.T) *panelHarness {
				harness := openMessages(t, panelHarnessParams{sidebarWidth: 60})
				harness.press("enter")
				settlePulse(harness)

				return harness
			},
			read: readStyledEngineLine,
		},
		{
			name: "errors-counted",
			open: func(t *testing.T) *panelHarness {
				harness := openMessages(t, panelHarnessParams{sidebarWidth: 60})
				harness.press("enter", "enter")
				settleFlash(harness)

				return harness
			},
			read: readStyledEngineLine,
		},
		{
			name: "the-pane-holding-the-keys",
			open: func(t *testing.T) *panelHarness {
				harness := openMessages(t, panelHarnessParams{})
				harness.press("enter", "ctrl+g", "M", "M", "j")
				settleFlash(harness)

				return harness
			},
			read: readStyledPane,
		},
		{
			name: "a-copied-message",
			open: func(t *testing.T) *panelHarness {
				harness := openMessages(t, panelHarnessParams{})
				harness.press("enter", "ctrl+g")
				settleFlash(harness)
				harness.press("y")

				return harness
			},
			read: readStyledPane,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := tt.open(t)

			ui.RequireGolden(t, "messages-"+tt.name, landedAt.ReplaceAllString(tt.read(harness), "<landed>"))
		})
	}
}

func openMessages(t *testing.T, params panelHarnessParams) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, params)
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), newDormant("api-refactor")))
	harness.refuse(agentv1connect.AgentServiceResumeAgentProcedure, newTracedRefusal("no such agent"))
	harness.press("j")

	return harness
}

func settlePulse(harness *panelHarness) {
	for range 3 {
		harness.firePulse()
	}
}

func settleFlash(harness *panelHarness) {
	for range 4 {
		harness.fireTimers()
	}
}

func newTracedRefusal(reason string) error {
	refusal := connect.NewError(connect.CodeFailedPrecondition, errors.New(reason))
	refusal.Meta().Set(caller.TraceHeader, traceID)

	return refusal
}

func readStyledEngineLine(harness *panelHarness) string {
	return splitLines(harness.readRawFrame())[1]
}

func readStyledPane(harness *panelHarness) string {
	return strings.Join(readStage(harness.readRawFrame()), "\n")
}

func readPaneTally(frame string) string {
	for _, line := range readStage(frame) {
		fields := strings.Fields(line)
		if len(fields) == 4 && fields[0] == "messages" {
			return strings.Join(fields[1:], " ")
		}
	}

	return ""
}

// TestMessagesRefusedWrites covers a refused write being said in the messages.
func TestMessagesRefusedWrites(t *testing.T) {
	tests := []struct {
		name        string
		agent       *agentv1.Agent
		refused     string
		reason      string
		press       []string
		wantSummary string
	}{
		{
			name:        "a refused archive",
			agent:       newDormant("api-refactor"),
			refused:     agentv1connect.AgentServiceArchiveAgentsProcedure,
			reason:      "already archived",
			press:       []string{"j", "z", "ctrl+s"},
			wantSummary: `Could not archive agent "api-refactor"`,
		},
		{
			name:        "a refused resume",
			agent:       newDormant("api-refactor"),
			refused:     agentv1connect.AgentServiceResumeAgentProcedure,
			reason:      "no such agent",
			press:       []string{"j", "enter"},
			wantSummary: `Could not resume agent "api-refactor"`,
		},
		{
			name:        "a refused pause",
			agent:       newPausable(""),
			refused:     agentv1connect.AgentServiceHoldAgentProcedure,
			reason:      `"telescope" would not stop`,
			press:       []string{"j", "p"},
			wantSummary: `Could not pause agent "telescope"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, newLiveWorkflow("w1", "backend"), tt.agent)
			harness.refuse(tt.refused, connect.NewError(connect.CodeFailedPrecondition, errors.New(tt.reason)))

			harness.press(tt.press...)
			harness.press("ctrl+g")

			pane := joinProse(readStage(harness.readFrame()))
			assert.Contains(t, pane, tt.wantSummary)
			assert.Contains(t, pane, tt.reason, "the service's reason is shown rather than the connect code in front of it")
		})
	}

	t.Run("a write that lands says nothing and takes nothing away", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newDormant("api-refactor"))
		harness.refuse(agentv1connect.AgentServiceResumeAgentProcedure, connect.NewError(connect.CodeNotFound, errors.New("no such agent")))

		harness.press("j", "enter", "z", "ctrl+s", "ctrl+g")

		assert.Equal(t, "1 of 1", readPaneTally(harness.readFrame()), "the archive that landed pushed nothing onto the stack")
	})
}

func readOpenMessages(harness *panelHarness) string {
	harness.press("ctrl+g")

	return joinProse(readStage(harness.readFrame()))
}
