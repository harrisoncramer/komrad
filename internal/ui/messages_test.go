package ui_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"komrad/internal/caller"
	"komrad/internal/engine"
	"komrad/internal/ui"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
)

const traceID = "7c1f2a94"

var (
	landedAt       = regexp.MustCompile(`[A-Z][a-z]{2} \d{1,2} at \d{1,2}:\d{2}[ap]m`)
	landedAtPadded = regexp.MustCompile(landedAt.String() + `(\x1b\[m)? *`)
)

// TestMessagesRegion covers the rows under the record that say what went wrong.
func TestMessagesRegion(t *testing.T) {
	t.Run("an empty stack says komrad is running normally", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{}, "no such agent")

		assert.Equal(t, []string{"✔ No messages", "  Komrad is running normally"}, readMessages(harness.readFrame()))
	})

	t.Run("a message takes back the row the empty region gave the list", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{}, "no such agent")
		empty := findRule(harness.readFrame())

		harness.press("enter")

		frame := harness.readFrame()
		assert.Equal(t, []string{"✗ Could not resume ag…", "  no such agent", "  ctrl+g to read"}, readMessages(frame))
		assert.Equal(t, empty-1, findRule(frame))
	})

	t.Run("a pane with no rows to spare draws no region", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{height: 13}, "no such agent")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "No messages")
		assert.Equal(t, "  api-refactor", readHoveredRow(frame))
	})

	t.Run("the key is refused with nothing to read", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{}, "no such agent")

		harness.press("ctrl+g", "k")

		frame := harness.readFrame()
		assert.Equal(t, "backend", readHoveredRow(frame), "the list kept the keys")
		assert.NotContains(t, readLegend(frame), "back to list")
	})

	t.Run("the region takes the keys from the list", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{}, "no such agent")

		harness.press("enter", "ctrl+g", "k")

		frame := harness.readFrame()
		assert.Equal(t, "  api-refactor", readHoveredRow(frame))
		assert.Contains(t, readLegend(frame), "C-g back to list")
	})

	t.Run("the whole message is drawn once the region has the keys", func(t *testing.T) {
		harness := openUnreachable(t, engine.Remote{})

		harness.press("ctrl+g")

		frame := harness.readFrame()
		region := readMessages(frame)
		require.NotEmpty(t, region)
		assert.Contains(t, region[0], "1 of 1", "the tally is on the summary row")
		prose := joinProse(region)
		assert.Contains(t, prose, "connection refused")
		assert.Contains(t, prose, "Start it with: komrad serve")
		assert.Contains(t, prose, "error ·", "the footer says what kind of message this is")
		assert.Contains(t, frame, "Agents & Workflows", "the list is still above it")
	})

	t.Run("a message carrying a trace says how to read it in the log", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{}, "no such agent")

		harness.press("enter", "ctrl+g")

		assert.Contains(t, joinProse(readMessages(harness.readFrame())), "komrad log "+traceID)
	})

	t.Run("a reason longer than the region scrolls under its pinned summary", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{height: 24}, strings.Repeat("the reason it gave ran on and on. ", 12))
		harness.press("enter", "ctrl+g")

		top := readMessages(harness.readFrame())
		require.NotEmpty(t, top)
		assert.Contains(t, top[0], "Could not resume")
		assert.Contains(t, joinProse(top), "more", "what is past the bottom is counted")

		harness.press("G")

		moved := readMessages(harness.readFrame())
		require.NotEmpty(t, moved)
		assert.Contains(t, moved[0], "Could not resume", "the summary keeps its row")
		assert.Contains(t, joinProse(moved), "↑", "what is above the window is counted")
		assert.Contains(t, joinProse(moved), "error ·", "the piece the cursor moved to is on screen")
	})

	t.Run("the whole message stands beside a form opened over the list", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{}, "no such agent")

		harness.press("enter", "r", "ctrl+g")

		stage := strings.Join(readStage(harness.readFrame()), "\n")
		assert.Contains(t, stage, "message 1 of 1")
		assert.Contains(t, stage, "komrad log "+traceID)
	})
}

// TestMessagesStack covers walking back through the messages that have landed.
func TestMessagesStack(t *testing.T) {
	t.Run("the newest message is the last of them and < walks back through time", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{}, "no such agent")
		harness.press("enter", "enter", "enter", "ctrl+g")

		var tallies []string
		for _, key := range []string{"<", "<", "<", ">"} {
			tallies = append(tallies, readTally(harness.readFrame()))
			harness.press(key)
		}
		tallies = append(tallies, readTally(harness.readFrame()))

		assert.Equal(t, []string{"3 of 3", "2 of 3", "1 of 3", "1 of 3", "2 of 3"}, tallies)
	})

	t.Run("esc takes the message on screen away and lands on the one behind it", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{}, "no such agent")
		harness.press("enter", "enter", "ctrl+g", "esc")

		frame := harness.readFrame()
		assert.Equal(t, "1 of 1", readTally(frame))
		assert.Contains(t, readLegend(frame), "back to list", "the region keeps the keys while there is more to read")

		harness.press("esc", "k")

		frame = harness.readFrame()
		assert.Contains(t, frame, "No messages")
		assert.Equal(t, "backend", readHoveredRow(frame), "an empty region hands the keys back")
	})

	t.Run("a message landing while the region is read leaves the reader where they were", func(t *testing.T) {
		harness := openMessages(t, panelHarnessParams{}, "no such agent")

		harness.press("enter", "ctrl+g", "M")

		assert.Equal(t, "1 of 2", readTally(harness.readFrame()))
	})
}

// TestMessagesCopy covers y putting the piece under the cursor on the clipboard.
func TestMessagesCopy(t *testing.T) {
	harness := openMessages(t, panelHarnessParams{}, "no such agent")
	harness.press("enter", "ctrl+g")

	harness.press("y", "j", "y", "j", "y", "j", "y")

	copied := harness.panel.ReadCopied()
	require.Len(t, copied, 4)
	assert.Equal(t, []string{`Could not resume agent "api-refactor"`, "no such agent", traceID}, copied[:3])
	assert.Regexp(t, landedAt, copied[3])
}

// TestMessagesBlink covers a message blinking as it lands.
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

			harness := openMessages(t, panelHarnessParams{}, "no such agent")
			harness.press(tt.press...)

			var heads []string
			for range 5 {
				heads = append(heads, readStyledMessages(harness)[0])
				harness.fireTimers()
			}

			lit, settled := heads[0], heads[4]
			assert.NotEqual(t, lit, settled)
			assert.Equal(t, []string{lit, settled, lit, settled, settled}, heads)
			assert.Equal(t, ansi.Strip(lit), ansi.Strip(settled), "the blink changes no words")
		})
	}

	t.Run("a message landing behind the one being read does not blink", func(t *testing.T) {
		t.Parallel()

		harness := openMessages(t, panelHarnessParams{}, "no such agent")
		harness.press("enter", "ctrl+g")
		harness.fireTimers()
		harness.press("M")

		landed := readStyledSummary(harness)
		harness.fireTimers()

		assert.Equal(t, landed, readStyledSummary(harness))
	})
}

// TestMessagesGolden draws the region whole, colours included.
func TestMessagesGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		open func(t *testing.T) *panelHarness
	}{
		{
			name: "empty",
			open: func(t *testing.T) *panelHarness {
				return openMessages(t, panelHarnessParams{}, "no such agent")
			},
		},
		{
			name: "an-error-behind-another",
			open: func(t *testing.T) *panelHarness {
				harness := openMessages(t, panelHarnessParams{}, "no such agent")
				harness.press("enter", "enter")
				settlePulse(harness)

				return harness
			},
		},
		{
			name: "a-warning-read-in-the-region",
			open: func(t *testing.T) *panelHarness {
				harness := openMessages(t, panelHarnessParams{}, "no such agent")
				harness.press(widenListPresses...)
				harness.press("enter", "ctrl+g", "M", "M", ">", ">")
				settlePulse(harness)

				return harness
			},
		},
		{
			name: "the-region-holding-the-keys",
			open: func(t *testing.T) *panelHarness {
				harness := openUnreachable(t, engine.Remote{})
				settlePulse(harness)
				harness.press("ctrl+g")

				return harness
			},
		},
		{
			name: "a-copied-piece",
			open: func(t *testing.T) *panelHarness {
				harness := openMessages(t, panelHarnessParams{}, "no such agent")
				harness.press("enter", "ctrl+g")
				settlePulse(harness)
				harness.press("j", "y")

				return harness
			},
		},
		{
			name: "an-info-message-read-in-the-region",
			open: func(t *testing.T) *panelHarness {
				harness := openArchive(t, buildOneArchivedAgent()...)
				harness.press("D", "y")
				settlePulse(harness)
				harness.press("ctrl+g")

				return harness
			},
		},
		{
			name: "an-info-message-landing",
			open: func(t *testing.T) *panelHarness {
				harness := openArchive(t, buildOneArchivedAgent()...)
				harness.press("D", "y")

				return harness
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := tt.open(t)

			region := landedAtPadded.ReplaceAllString(strings.Join(readStyledMessages(harness), "\n"), "<landed>$1")
			ui.RequireGolden(t, "messages-"+tt.name, region)
		})
	}
}

func openMessages(t *testing.T, params panelHarnessParams, reason string) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, params)
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), newDormant("api-refactor")))
	harness.refuse(agentv1connect.AgentServiceResumeAgentProcedure, newTracedRefusal(reason))
	harness.press("j")

	return harness
}

func settlePulse(harness *panelHarness) {
	for range 3 {
		harness.fireTimers()
	}
}

func newTracedRefusal(reason string) error {
	refusal := connect.NewError(connect.CodeFailedPrecondition, errors.New(reason))
	refusal.Meta().Set(caller.TraceHeader, traceID)

	return refusal
}

func readMessages(frame string) []string {
	lines := splitLines(frame)
	top, bottom, width := findMessageRules(lines)
	if top < 0 {
		return nil
	}

	return xslice.Map(lines[top+1:bottom], func(line string) string {
		return strings.TrimRight(ansi.Truncate(line, width, ""), " ")
	})
}

func readStyledMessages(harness *panelHarness) []string {
	styled := splitLines(harness.readRawFrame())
	top, bottom, width := findMessageRules(splitLines(harness.readFrame()))
	if top < 0 {
		return nil
	}

	return xslice.Map(styled[top+1:bottom], func(line string) string {
		return ansi.Truncate(line, width, "")
	})
}

func readStyledSummary(harness *panelHarness) string {
	_, summary, _ := strings.Cut(readStyledMessages(harness)[0], "✗")

	return summary
}

func findMessageRules(lines []string) (int, int, int) {
	var rules []int
	for i, line := range lines {
		if strings.HasPrefix(line, "─") {
			rules = append(rules, i)
		}
	}
	if len(rules) < 2 {
		return -1, -1, 0
	}

	bottom := rules[len(rules)-1]

	return rules[len(rules)-2], bottom, ansi.StringWidth(strings.TrimRight(lines[bottom], " "))
}

func readTally(frame string) string {
	region := readMessages(frame)
	if len(region) == 0 {
		return ""
	}

	fields := strings.Fields(region[0])
	if len(fields) < 3 {
		return ""
	}

	return strings.Join(fields[len(fields)-3:], " ")
}

// TestMessagesRefusedWrites covers a refused write being said in the message region.
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

			harness.press(append(tt.press, widenListPresses...)...)

			frame := harness.readFrame()
			assert.Contains(t, frame, tt.wantSummary)
			assert.Contains(t, frame, tt.reason, "the service's reason is shown rather than the connect code in front of it")
			assert.NotEmpty(t, readHoveredRow(frame), "the list is still on screen above the region")
		})
	}

	t.Run("a write that lands says nothing and takes nothing away", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newDormant("api-refactor"))
		harness.refuse(agentv1connect.AgentServiceResumeAgentProcedure, connect.NewError(connect.CodeNotFound, errors.New("no such agent")))

		harness.press("j", "enter", "z", "ctrl+s")
		harness.press(widenListPresses...)

		frame := harness.readFrame()
		assert.Contains(t, frame, `Could not resume agent "api-refactor"`)
		assert.NotContains(t, frame, "+1", "the archive that landed pushed nothing onto the stack")
	})

	t.Run("a message outlives the form opened over it", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newDormant("api-refactor"))
		harness.refuse(agentv1connect.AgentServiceResumeAgentProcedure, connect.NewError(connect.CodeNotFound, errors.New("no such agent")))

		harness.press("j", "enter", "r")
		require.Contains(t, harness.readFrame(), "Rename")

		harness.press("esc")

		assert.Contains(t, harness.readFrame(), "no such agent")
	})
}

// TestMessagesRegionKeys covers the keys of the message region.
func TestMessagesRegionKeys(t *testing.T) {
	harness := openAgents(t, newLiveWorkflow("w1", "backend"), newDormant("api-refactor"))
	harness.refuse(agentv1connect.AgentServiceResumeAgentProcedure, connect.NewError(connect.CodeNotFound, errors.New("no such agent")))
	harness.press("j", "enter")

	assert.Contains(t, harness.readFrame(), "to read", "the way into the whole message is said")

	harness.press("esc")
	assert.Contains(t, harness.readFrame(), "no such agent", "the list's own esc leaves the region alone")

	harness.press("ctrl+g", "esc")
	assert.Contains(t, harness.readFrame(), "No messages", "the region's esc dismisses what it is showing")
}
