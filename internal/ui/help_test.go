package ui_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"komrad/internal/ui/paneltest"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
)

// TestHelp covers the legend under the list and the key that expands it.
func TestHelp(t *testing.T) {
	t.Run("the legend opens on one line and the help key brings the rest out", func(t *testing.T) {
		harness := openHelp(t, paneltest.Params{}, slices.Repeat([]string{"ctrl+space", "l"}, 4)...)
		collapsed := harness.Frame()

		legend := paneltest.Legend(collapsed)
		assert.Contains(t, legend, "open", "it keeps what the row does first")
		assert.Contains(t, legend, "show help")
		assert.NotContains(t, legend, "rename", "and drops what the row also does")

		harness.Press("?")

		expanded := harness.Frame()
		legend = paneltest.Legend(expanded)
		assert.Contains(t, legend, "rename", "the expanded legend names every binding")
		assert.Contains(t, legend, "hide help")
		assert.Less(t, findRule(expanded), findRule(collapsed), "the rows the legend takes are the list's")

		harness.Press("?")
		assert.Contains(t, paneltest.Legend(harness.Frame()), "show help", "the key toggles rather than only expanding")
	})
}

func openHelp(t *testing.T, params paneltest.Params, press ...string) *paneltest.Panel {
	t.Helper()

	dormant := paneltest.Dormant("api-refactor")
	dormant.Id = "a2"

	harness := paneltest.Start(t, params)
	harness.AnswerWorktreeReads()
	harness.Refuse(agentv1connect.AgentServiceResumeAgentProcedure, newTracedRefusal("no such agent"))
	harness.Watch(paneltest.Snapshot(paneltest.Workflow("w1", "backend"), newThinking(), dormant))
	harness.Press(press...)

	return harness
}

func findRule(frame string) int {
	return slices.IndexFunc(paneltest.SplitLines(frame), func(line string) bool {
		return strings.HasPrefix(line, "─")
	})
}

func newThinking() *agentv1.Agent {
	return &agentv1.Agent{
		Id:         "a1",
		Name:       "telescope",
		WorkflowId: "w1",
		PaneId:     "%9",
		Alive:      xptr.Ptr(true),
		State:      agentv1.AgentState_AGENT_STATE_THINKING,
	}
}
