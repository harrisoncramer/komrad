package ui

import (
	"errors"
	"testing"
	"time"

	"weave/config"
	"weave/internal/client"
	"weave/internal/ui/layout"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	watchv1 "weave/proto/gen/watch/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func watchedPanel() model {
	return model{
		api: api{
			agents:    emptyLists{},
			workflows: emptyLists{},
			schedules: emptyLists{},
		},
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
	}
}

func update(t *testing.T, m model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()

	next, cmd := m.Update(msg)

	updated, ok := next.(model)
	require.True(t, ok)

	return updated, cmd
}

func agentEvent(agent *agentv1.Agent) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Agent{Agent: agent}}
}

func workflowEvent(workflow *workflowv1.Workflow) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Workflow{Workflow: workflow}}
}

func scheduleEvent(schedule *schedulev1.Schedule) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Schedule{Schedule: schedule}}
}

func deletedEvent(kind watchv1.RowKind, id string) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Deleted{Deleted: &watchv1.Deleted{Kind: kind, Id: id}}}
}

func agentIDs(items []agentItem) []string {
	return xslice.Map(items, func(item agentItem) string { return item.agent.ID })
}

// hasListed says whether the tick's command asked the service for the archive or the worktrees.
func hasListed(cmd tea.Cmd) bool {
	for _, msg := range produced(cmd) {
		if _, ok := msg.(listedMsg); ok {
			return true
		}
	}

	return false
}

// TestSnapshotReplacesTheListing covers the opening of a stream: every row before Synced lands
// in the order the list RPCs answer, and the cursor is placed the way the first listing places
// it.
func TestSnapshotReplacesTheListing(t *testing.T) {
	m := watchedPanel()

	m, _ = update(t, m, client.WatchBatch{Snapshot: true, Events: []*watchv1.WatchEvent{
		agentEvent(&agentv1.Agent{Id: "01a", Name: "older", WorkflowId: "w1"}),
		agentEvent(&agentv1.Agent{Id: "01b", Name: "api-refactor", WorkflowId: "w1", Mark: proto.String("a")}),
		workflowEvent(&workflowv1.Workflow{Id: "w2", Name: "beta"}),
		workflowEvent(&workflowv1.Workflow{Id: "w1", Name: "Alpha"}),
		scheduleEvent(&schedulev1.Schedule{Id: "s1"}),
	}})

	assert.Equal(t, []string{"01b", "01a"}, agentIDs(m.listing.agents), "agents newest first")
	require.Len(t, m.listing.workflows, 2)
	assert.Equal(t, "w1", m.listing.workflows[0].ID, "workflows by name, case aside")
	assert.Len(t, m.listing.schedules, 1)
	assert.Equal(t, map[string]string{agentRowKey("01b"): "a"}, m.listing.marks)

	selected, ok := m.agents.selectedAgent(m.shared())
	require.True(t, ok)
	assert.Equal(t, "api-refactor", selected.agent.Name)

	m, _ = update(t, m, client.WatchBatch{Snapshot: true, Events: []*watchv1.WatchEvent{
		agentEvent(&agentv1.Agent{Id: "01c", Name: "after-reconnect", WorkflowId: "w1"}),
	}})

	assert.Equal(t, []string{"01c"}, agentIDs(m.listing.agents), "the next stream's snapshot replaces what the last one built")
	assert.Empty(t, m.listing.marks)
}

// TestEventsReplaceAndDropRows covers the live half: a row event lands over the row it names or
// files a new one where the order puts it, and a tombstone drops one, with the cursor held on
// the row it was on throughout.
func TestEventsReplaceAndDropRows(t *testing.T) {
	m := watchedPanel()

	m, _ = update(t, m, client.WatchBatch{Snapshot: true, Events: []*watchv1.WatchEvent{
		workflowEvent(&workflowv1.Workflow{Id: "w1", Name: "api"}),
		agentEvent(&agentv1.Agent{Id: "01a", Name: "one", WorkflowId: "w1"}),
		agentEvent(&agentv1.Agent{Id: "01b", Name: "two", WorkflowId: "w1", Mark: proto.String("b")}),
	}})
	m.agents.cursor = rowIndexOfAgentID(m.agents.rows(m.shared()), "01a")

	m, _ = update(t, m, client.WatchBatch{Events: []*watchv1.WatchEvent{
		agentEvent(&agentv1.Agent{Id: "01a", Name: "renamed", WorkflowId: "w1"}),
		deletedEvent(watchv1.RowKind_ROW_KIND_AGENT, "01b"),
		agentEvent(&agentv1.Agent{Id: "01c", Name: "three", WorkflowId: "w1"}),
		workflowEvent(&workflowv1.Workflow{Id: "w0", Name: "aaa", Mark: proto.String("w")}),
		scheduleEvent(&schedulev1.Schedule{Id: "s1"}),
	}})

	assert.Equal(t, []string{"01c", "01a"}, agentIDs(m.listing.agents))
	assert.Equal(t, "renamed", m.listing.agents[1].agent.Name)
	assert.Equal(t, "w0", m.listing.workflows[0].ID, "a workflow is filed by name")
	assert.Equal(t, map[string]string{workflowRowKey("w0"): "w"}, m.listing.marks, "a tombstone takes its mark with it")
	assert.Len(t, m.listing.schedules, 1)

	selected, ok := m.agents.selectedAgent(m.shared())
	require.True(t, ok)
	assert.Equal(t, "renamed", selected.agent.Name, "the cursor follows the row it was on")

	m, _ = update(t, m, client.WatchBatch{Events: []*watchv1.WatchEvent{
		deletedEvent(watchv1.RowKind_ROW_KIND_WORKFLOW, "w0"),
		deletedEvent(watchv1.RowKind_ROW_KIND_SCHEDULE, "s1"),
	}})

	assert.Equal(t, "w1", m.listing.workflows[0].ID)
	assert.Empty(t, m.listing.marks)
	assert.Empty(t, m.listing.schedules)
}

// TestLostStreamIsReportedUntilTheNextSnapshot covers the server going away being said once
// when the stream ends, and cleared by the snapshot the next stream opens with.
func TestLostStreamIsReportedUntilTheNextSnapshot(t *testing.T) {
	m := watchedPanel()

	m, _ = update(t, m, watchLostMsg{err: errors.New("the server hung up")})
	require.Error(t, m.unreachable)
	require.Len(t, m.messages.items, 1)

	m, _ = update(t, m, watchLostMsg{})
	assert.Len(t, m.messages.items, 1, "a stream that keeps failing to open says nothing more")

	m, _ = update(t, m, client.WatchBatch{Snapshot: true})
	require.NoError(t, m.unreachable)

	m, _ = update(t, m, watchLostMsg{})
	require.ErrorIs(t, m.unreachable, errWatchClosed, "a close with no error is still the server gone")
	assert.Len(t, m.messages.items, 2)
}

// TestTickListsOnlyWhatTheWatchLacks covers the archive screen, whose listing rides no stream,
// being listed on every tick, and the agents screen asking for nothing.
func TestTickListsOnlyWhatTheWatchLacks(t *testing.T) {
	m := watchedPanel()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	_, cmd := update(t, m, tickMsg{at: now})
	assert.False(t, hasListed(cmd), "the agents screen is fed by the watch alone")

	m.screen = screenArchive
	_, cmd = update(t, m, tickMsg{at: now})
	assert.True(t, hasListed(cmd), "the archive is listed on every tick")
}
