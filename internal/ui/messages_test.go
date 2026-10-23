package ui_test

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"komrad/internal/caller"
	"komrad/internal/engine"
	"komrad/internal/ui/paneltest"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	worktreev1 "komrad/proto/gen/worktree/v1"
)

// TestMessagesHeader covers a message flashing on the engine line and what it leaves there.
func TestMessagesHeader(t *testing.T) {
	t.Run("dismissing the message that landed ends its flash", func(t *testing.T) {
		harness := openMessages(t, paneltest.Params{SidebarWidth: 60})

		harness.Press("enter", "enter", "ctrl+g", "d", "esc")

		assert.Equal(t, "\uf444 local  ✗ 1  C-g", readEngineLine(harness.Frame()))
	})

	t.Run("an info message leaves nothing once the flash is over", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, &worktreev1.WatchedWorktree{
			Dir:      "/trees/orphan",
			RepoDir:  "/home/h/komrad",
			Branch:   "task/orphan",
			Readable: true,
		})

		harness.Press("shift+tab", "esc", "space", "c", "y")
		harness.Watch(buildDeletedWorktrees("/trees/orphan"))
		require.Contains(t, readEngineLine(harness.Frame()), "·")
		settleFlash(harness)

		assert.Equal(t, " local", readEngineLine(harness.Frame()))
	})
}

// TestMessagesPane covers every message read in the main pane.
func TestMessagesPane(t *testing.T) {
	t.Run("the key is refused with nothing to read", func(t *testing.T) {
		harness := openMessages(t, paneltest.Params{})

		harness.Press("ctrl+g", "k")

		assert.Equal(t, "backend", paneltest.HoveredRow(harness.Frame()), "the list kept the keys")
	})

	t.Run("the cursor walks from the newest message back", func(t *testing.T) {
		harness := openMessages(t, paneltest.Params{})
		harness.Press("enter", "enter", "enter", "ctrl+g")

		var tallies []string
		for _, key := range []string{"j", "j", "j", "k"} {
			tallies = append(tallies, readPaneTally(harness.Frame()))
			harness.Press(key)
		}
		tallies = append(tallies, readPaneTally(harness.Frame()))

		assert.Equal(t, []string{"1 of 3", "2 of 3", "3 of 3", "3 of 3", "2 of 3"}, tallies)
	})

	t.Run("the message under the cursor is rendered whole in a short pane", func(t *testing.T) {
		for height := 9; height <= 16; height++ {
			harness := openMessages(t, paneltest.Params{Height: height})
			harness.Press("enter", "enter", "enter", "ctrl+g")

			for at := range 3 {
				stage := paneltest.Stage(harness.Frame())
				caret := slices.IndexFunc(stage, func(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), "›") })
				require.GreaterOrEqual(t, caret, 0, "message %d at height %d", at, height)
				require.Less(t, caret+2, len(stage), "message %d at height %d", at, height)
				assert.Contains(t, stage[caret+2], "komrad log "+traceID, "message %d at height %d", at, height)
				harness.Press("j")
			}
		}
	})

	t.Run("d dismisses the message under the cursor and the last one hands the keys back", func(t *testing.T) {
		harness := openMessages(t, paneltest.Params{})
		harness.Press("enter", "enter", "ctrl+g", "d")

		assert.Equal(t, "1 of 1", readPaneTally(harness.Frame()))

		harness.Press("d", "k")

		frame := harness.Frame()
		assert.NotContains(t, frame, "Could not resume")
		assert.Equal(t, "backend", paneltest.HoveredRow(frame))
	})

	t.Run("D dismisses every message", func(t *testing.T) {
		harness := openMessages(t, paneltest.Params{SidebarWidth: 60})
		harness.Press("enter", "enter", "ctrl+g", "D")
		settleFlash(harness)

		frame := harness.Frame()
		assert.NotContains(t, frame, "Could not resume")
		assert.Equal(t, " local", readEngineLine(frame))
	})

	t.Run("esc hands the keys back and keeps the messages", func(t *testing.T) {
		harness := openMessages(t, paneltest.Params{SidebarWidth: 60})
		harness.Press("enter", "ctrl+g", "esc", "k")
		settleFlash(harness)

		frame := harness.Frame()
		assert.Equal(t, "backend", paneltest.HoveredRow(frame))
		assert.Equal(t, " local  ✗ 1  C-g", readEngineLine(frame))
	})

	t.Run("a message landing while the pane is read leaves the reader where they were", func(t *testing.T) {
		harness := openMessages(t, paneltest.Params{})

		harness.Press("enter")
		harness.Hold(agentv1connect.AgentServiceResumeAgentProcedure)
		harness.Press("enter", "ctrl+g")
		harness.Release(agentv1connect.AgentServiceResumeAgentProcedure)

		assert.Equal(t, "2 of 2", readPaneTally(harness.Frame()))
	})

	t.Run("the messages stand in for a form opened over the list", func(t *testing.T) {
		harness := openMessages(t, paneltest.Params{})

		harness.Press("enter", "c", "ctrl+g")

		stage := paneltest.JoinProse(paneltest.Stage(harness.Frame()))
		assert.Contains(t, stage, "messages 1 of 1")
		assert.Contains(t, stage, "komrad log "+traceID)
	})

	t.Run("the unreachable server says what to do about it", func(t *testing.T) {
		harness := openUnreachable(t, engine.Remote{})

		harness.Press("ctrl+g")

		pane := paneltest.JoinProse(paneltest.Stage(harness.Frame()))
		assert.Contains(t, pane, "connection refused")
		assert.Contains(t, pane, "Start it with: komrad serve")
	})
}

// TestMessagesCopy covers y putting the message under the cursor on the clipboard.
func TestMessagesCopy(t *testing.T) {
	t.Run("the message under the cursor is put on the clipboard", func(t *testing.T) {
		harness := openMessages(t, paneltest.Params{})
		harness.Press("enter", "ctrl+g", "y")

		assert.Equal(t, []string{"Could not resume agent \"api-refactor\"\n\nno such agent\n\nkomrad log " + traceID}, harness.Copied())
	})

	t.Run("the copied message keeps its flash when another lands above it", func(t *testing.T) {
		harness := openMessages(t, paneltest.Params{})
		harness.Press("enter")
		harness.Refuse(agentv1connect.AgentServiceResumeAgentProcedure, newTracedRefusal("gone for good"))
		harness.Hold(agentv1connect.AgentServiceResumeAgentProcedure)
		harness.Press("enter", "ctrl+g")
		settleFlash(harness)
		plain := readStyledSummary(harness, "no such agent")
		harness.Press("y")
		flashed := readStyledSummary(harness, "no such agent")
		require.NotEqual(t, plain, flashed, "the copy flashes the row")

		harness.Release(agentv1connect.AgentServiceResumeAgentProcedure)

		assert.Equal(t, flashed, readStyledSummary(harness, "no such agent"))
	})
}

// TestMessagesGolden renders the header and the main pane whole, colours included.
func TestMessagesGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		open func(t *testing.T) *paneltest.Panel
		read func(harness *paneltest.Panel) string
	}{
		{
			name: "an-error-flashing",
			open: func(t *testing.T) *paneltest.Panel {
				harness := openMessages(t, paneltest.Params{SidebarWidth: 60})
				harness.Press("enter")
				settlePulse(harness)

				return harness
			},
			read: readStyledEngineLine,
		},
		{
			name: "errors-counted",
			open: func(t *testing.T) *paneltest.Panel {
				harness := openMessages(t, paneltest.Params{SidebarWidth: 60})
				harness.Press("enter", "enter")
				settleFlash(harness)

				return harness
			},
			read: readStyledEngineLine,
		},
		{
			name: "the-pane-holding-the-keys",
			open: func(t *testing.T) *paneltest.Panel {
				harness := openMessages(t, paneltest.Params{})
				harness.Press("enter", "enter", "enter", "ctrl+g", "j")
				settleFlash(harness)

				return harness
			},
			read: readStyledPane,
		},
		{
			name: "a-copied-message",
			open: func(t *testing.T) *paneltest.Panel {
				harness := openMessages(t, paneltest.Params{})
				harness.Press("enter", "ctrl+g")
				settleFlash(harness)
				harness.Press("y")

				return harness
			},
			read: readStyledPane,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := tt.open(t)

			paneltest.RequireGolden(t, "messages-"+tt.name, landedAt.ReplaceAllString(tt.read(harness), "<landed>"))
		})
	}
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
			press:       []string{"j", "space", "a", "enter"},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "backend"), tt.agent)
			harness.Refuse(tt.refused, connect.NewError(connect.CodeFailedPrecondition, errors.New(tt.reason)))

			harness.Press(tt.press...)
			harness.Press("ctrl+g")

			pane := paneltest.JoinProse(paneltest.Stage(harness.Frame()))
			assert.Contains(t, pane, tt.wantSummary)
			assert.Contains(t, pane, tt.reason, "the server's reason is shown rather than the connect code in front of it")
		})
	}

	t.Run("a write that lands says nothing and takes nothing away", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "backend"), newDormant("api-refactor"))
		harness.Refuse(agentv1connect.AgentServiceResumeAgentProcedure, connect.NewError(connect.CodeNotFound, errors.New("no such agent")))

		harness.Press("j", "enter", "space", "a", "enter", "ctrl+g")

		assert.Equal(t, "1 of 1", readPaneTally(harness.Frame()), "the archive that landed pushed nothing onto the stack")
	})
}

const traceID = "7c1f2a94"

var landedAt = regexp.MustCompile(`[A-Z][a-z]{2} \d{1,2} at \d{1,2}:\d{2}[ap]m`)

func openMessages(t *testing.T, params paneltest.Params) *paneltest.Panel {
	t.Helper()

	harness := paneltest.Start(t, params)
	harness.AnswerWorktreeReads()
	harness.Watch(paneltest.Snapshot(paneltest.Workflow("w1", "backend"), newDormant("api-refactor")))
	harness.Refuse(agentv1connect.AgentServiceResumeAgentProcedure, newTracedRefusal("no such agent"))
	harness.Press("j")

	return harness
}

func settlePulse(harness *paneltest.Panel) {
	harness.Advance(time.Second)
}

func settleFlash(harness *paneltest.Panel) {
	harness.Advance(6 * time.Second)
}

func newTracedRefusal(reason string) error {
	refusal := connect.NewError(connect.CodeFailedPrecondition, errors.New(reason))
	refusal.Meta().Set(caller.TraceHeader, traceID)

	return refusal
}

func readStyledEngineLine(harness *paneltest.Panel) string {
	return paneltest.SplitLines(harness.RawFrame())[1]
}

func readStyledPane(harness *paneltest.Panel) string {
	return strings.Join(paneltest.Stage(harness.RawFrame()), "\n")
}

func readStyledSummary(harness *paneltest.Panel, summary string) string {
	line := findLine(paneltest.Stage(harness.RawFrame()), summary)
	at := strings.Index(line, summary)
	if at < 0 {
		return ""
	}

	return line[max(0, strings.LastIndex(line[:at], "\x1b[")):]
}

func readPaneTally(frame string) string {
	for _, line := range paneltest.Stage(frame) {
		fields := strings.Fields(line)
		if len(fields) == 4 && fields[0] == "messages" {
			return strings.Join(fields[1:], " ")
		}
	}

	return ""
}
