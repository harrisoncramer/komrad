package ui

import (
	"errors"
	"slices"
	"testing"
	"time"

	"weave/config"
	"weave/internal/client"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
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

func agentRowOf(agent *agentv1.Agent) *watchv1.Row {
	return &watchv1.Row{Row: &watchv1.Row_Agent{Agent: agent}}
}

func workflowRowOf(workflow *workflowv1.Workflow) *watchv1.Row {
	return &watchv1.Row{Row: &watchv1.Row_Workflow{Workflow: workflow}}
}

func scheduleRowOf(schedule *schedulev1.Schedule) *watchv1.Row {
	return &watchv1.Row{Row: &watchv1.Row_Schedule{Schedule: schedule}}
}

func markRowOf(mark *agentv1.Mark) *watchv1.Row {
	return &watchv1.Row{Row: &watchv1.Row_Mark{Mark: mark}}
}

func mailRowOf(mail *agentv1.Mail) *watchv1.Row {
	return &watchv1.Row{Row: &watchv1.Row_Mail{Mail: mail}}
}

func putEvent(row *watchv1.Row) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Put{Put: row}}
}

func snapshotEvent(rows ...*watchv1.Row) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Snapshot{Snapshot: &watchv1.Snapshot{Rows: rows}}}
}

func deletedEvent(resource watchv1.Resource, key string) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Deleted{Deleted: &watchv1.Deleted{Resource: resource, Key: key}}}
}

func agentIDs(items []panel.AgentRow) []string {
	return xslice.Map(items, func(item panel.AgentRow) string { return item.Agent.ID })
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

// TestSnapshotReplacesTheListing covers the opening of a stream: the snapshot's rows fill a
// fresh world, the listing is assembled from it, and the cursor is placed the way the first
// listing places it.
func TestSnapshotReplacesTheListing(t *testing.T) {
	m := watchedPanel()

	m, _ = update(t, m, client.WatchBatch{snapshotEvent(
		agentRowOf(&agentv1.Agent{
			Id:         "01a",
			Name:       "older",
			WorkflowId: "w1",
		}),
		agentRowOf(&agentv1.Agent{
			Id:         "01b",
			Name:       "api-refactor",
			WorkflowId: "w1",
		}),
		workflowRowOf(&workflowv1.Workflow{Id: "w2", Name: "beta"}),
		workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "Alpha"}),
		scheduleRowOf(&schedulev1.Schedule{Id: "s1"}),
		markRowOf(&agentv1.Mark{
			Id:      "mark-a",
			Mark:    "a",
			AgentId: proto.String("01b"),
		}),
		pullRequestRowOf("pr-1", "agent/x", 7),
	)})

	assert.Equal(t, []string{"01b", "01a"}, agentIDs(m.listing.agents), "agents newest first")
	require.Len(t, m.listing.workflows, 2)
	assert.Equal(t, "w1", m.listing.workflows[0].ID, "workflows by name, case aside")
	assert.Len(t, m.listing.schedules, 1)
	assert.Equal(t, map[string]string{"01b": "a"}, m.listing.marks)
	assert.Len(t, m.listing.world.PullRequests, 1)

	selected, ok := m.agents.selectedAgent(m.shared())
	require.True(t, ok)
	assert.Equal(t, "api-refactor", selected.Agent.Name)

	m, _ = update(t, m, client.WatchBatch{snapshotEvent(
		agentRowOf(&agentv1.Agent{
			Id:         "01c",
			Name:       "after-reconnect",
			WorkflowId: "w1",
		}),
	)})

	assert.Equal(t, []string{"01c"}, agentIDs(m.listing.agents), "the next stream's snapshot replaces what the last one built")
	assert.Empty(t, m.listing.marks)
	assert.Empty(t, m.listing.workflows)
	assert.Empty(t, m.listing.schedules)
	assert.Empty(t, m.listing.world.PullRequests)
}

// TestMarkBeforeItsAgentDrawsOnTheNextAssemble covers the stream promising no order between
// resources: a mark for an agent the world does not hold is kept, and drawn once the agent
// arrives.
func TestMarkBeforeItsAgentDrawsOnTheNextAssemble(t *testing.T) {
	m := watchedPanel()
	m.width, m.height = 200, 40

	m, _ = update(t, m, client.WatchBatch{snapshotEvent(
		workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "api"}),
	)})
	m, _ = update(t, m, client.WatchBatch{
		putEvent(markRowOf(&agentv1.Mark{
			Id:      "mark-a",
			Mark:    "a",
			AgentId: proto.String("01a"),
		})),
	})

	assert.Empty(t, m.listing.agents, "the mark draws no row of its own")
	_, ok := markedRow(m.listing, "a")
	assert.False(t, ok, "and names nothing to jump to yet")

	m, _ = update(t, m, client.WatchBatch{
		putEvent(agentRowOf(&agentv1.Agent{
			Id:         "01a",
			Name:       "one",
			WorkflowId: "w1",
		})),
	})

	rows := m.agents.snapshot(m.shared(), listPane{width: m.width}).Rows
	require.Len(t, rows, 2)
	assert.Equal(t, "a", rows[1].Mark, "the agent arrives wearing the mark")
}

// TestTombstoneDropsTheRowAndItsChildren covers an agent leaving with the children attached to
// it, each of which the service tombstones in the same batch.
func TestTombstoneDropsTheRowAndItsChildren(t *testing.T) {
	m := watchedPanel()

	m, _ = update(t, m, client.WatchBatch{snapshotEvent(
		workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "api"}),
		agentRowOf(&agentv1.Agent{
			Id:         "01a",
			Name:       "parent",
			WorkflowId: "w1",
		}),
		agentRowOf(&agentv1.Agent{
			Id:            "01b",
			Name:          "child",
			WorkflowId:    "w1",
			ParentAgentId: proto.String("01a"),
		}),
		agentRowOf(&agentv1.Agent{
			Id:         "01c",
			Name:       "other",
			WorkflowId: "w1",
		}),
		markRowOf(&agentv1.Mark{
			Id:      "mark-a",
			Mark:    "a",
			AgentId: proto.String("01a"),
		}),
	)})
	require.Len(t, m.agents.rows(m.shared()), 4)

	m, _ = update(t, m, client.WatchBatch{
		deletedEvent(watchv1.Resource_RESOURCE_AGENT, "01a"),
		deletedEvent(watchv1.Resource_RESOURCE_MARK, "mark-a"),
		deletedEvent(watchv1.Resource_RESOURCE_AGENT, "01b"),
	})

	assert.Equal(t, []string{"01c"}, agentIDs(m.listing.agents))
	assert.Empty(t, m.listing.marks)
	assert.Len(t, m.agents.rows(m.shared()), 2)
}

// TestUnclaimedMailExpiresByThePanelsClock covers the unread glyph being decided here: mail
// that is still to be claimed draws it, and mail past its expiry does not, with no event to
// say so.
func TestUnclaimedMailExpiresByThePanelsClock(t *testing.T) {
	m := watchedPanel()
	stamp := func(at time.Time) string { return at.UTC().Format(time.RFC3339) }

	m, _ = update(t, m, client.WatchBatch{snapshotEvent(
		workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "api"}),
		agentRowOf(&agentv1.Agent{Id: "01a", WorkflowId: "w1"}),
		agentRowOf(&agentv1.Agent{Id: "01b", WorkflowId: "w1"}),
		mailRowOf(&agentv1.Mail{
			Id:        "m1",
			ToAgentId: "01a",
			ExpiresAt: stamp(time.Now().Add(time.Hour)),
		}),
		mailRowOf(&agentv1.Mail{
			Id:        "m2",
			ToAgentId: "01b",
			ExpiresAt: stamp(time.Now().Add(-time.Hour)),
		}),
	)})

	waiting, ok := m.listing.liveAgent("01a")
	require.True(t, ok)
	assert.True(t, waiting.UnreadMail)
	expired, ok := m.listing.liveAgent("01b")
	require.True(t, ok)
	assert.False(t, expired.UnreadMail)

	m, _ = update(t, m, client.WatchBatch{
		deletedEvent(watchv1.Resource_RESOURCE_MAIL, "m1"),
	})

	claimed, ok := m.listing.liveAgent("01a")
	require.True(t, ok)
	assert.False(t, claimed.UnreadMail, "mail that was claimed leaves as a tombstone")
}

// TestEventsReplaceAndDropRows covers the live half: a row event lands over the row it names or
// files a new one where the order puts it, and a tombstone drops one, with the cursor held on
// the row it was on throughout.
func TestEventsReplaceAndDropRows(t *testing.T) {
	m := watchedPanel()

	m, _ = update(t, m, client.WatchBatch{snapshotEvent(
		workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "api"}),
		agentRowOf(&agentv1.Agent{
			Id:         "01a",
			Name:       "one",
			WorkflowId: "w1",
		}),
		agentRowOf(&agentv1.Agent{
			Id:         "01b",
			Name:       "two",
			WorkflowId: "w1",
		}),
		markRowOf(&agentv1.Mark{
			Id:      "mark-b",
			Mark:    "b",
			AgentId: proto.String("01b"),
		}),
	)})
	m.agents.cursor = rowIndexOfAgentID(m.agents.rows(m.shared()), "01a")

	m, _ = update(t, m, client.WatchBatch{
		putEvent(agentRowOf(&agentv1.Agent{
			Id:         "01a",
			Name:       "renamed",
			WorkflowId: "w1",
		})),
		deletedEvent(watchv1.Resource_RESOURCE_AGENT, "01b"),
		deletedEvent(watchv1.Resource_RESOURCE_MARK, "mark-b"),
		putEvent(agentRowOf(&agentv1.Agent{
			Id:         "01c",
			Name:       "three",
			WorkflowId: "w1",
		})),
		putEvent(workflowRowOf(&workflowv1.Workflow{Id: "w0", Name: "aaa"})),
		putEvent(markRowOf(&agentv1.Mark{
			Id:         "mark-w",
			Mark:       "w",
			WorkflowId: proto.String("w0"),
		})),
		putEvent(scheduleRowOf(&schedulev1.Schedule{Id: "s1"})),
	})

	assert.Equal(t, []string{"01c", "01a"}, agentIDs(m.listing.agents))
	assert.Equal(t, "renamed", m.listing.agents[1].Agent.Name)
	assert.Equal(t, "w0", m.listing.workflows[0].ID, "a workflow is filed by name")
	assert.Equal(t, map[string]string{"w0": "w"}, m.listing.marks, "a mark's tombstone takes the letter off")
	assert.Len(t, m.listing.schedules, 1)

	selected, ok := m.agents.selectedAgent(m.shared())
	require.True(t, ok)
	assert.Equal(t, "renamed", selected.Agent.Name, "the cursor follows the row it was on")

	m, _ = update(t, m, client.WatchBatch{
		deletedEvent(watchv1.Resource_RESOURCE_WORKFLOW, "w0"),
		deletedEvent(watchv1.Resource_RESOURCE_MARK, "mark-w"),
		deletedEvent(watchv1.Resource_RESOURCE_SCHEDULE, "s1"),
	})

	assert.Equal(t, "w1", m.listing.workflows[0].ID)
	assert.Empty(t, m.listing.marks)
	assert.Empty(t, m.listing.schedules)
}

// TestRenamedWorkflowMovesToItsPlace covers a workflow event that changes the name its row is
// ordered by, which refiles the row rather than leaving it where the old name sorted it, and
// counts as the workflows moving under an open form.
func TestRenamedWorkflowMovesToItsPlace(t *testing.T) {
	m := watchedPanel()

	m, _ = update(t, m, client.WatchBatch{snapshotEvent(
		workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "api"}),
		workflowRowOf(&workflowv1.Workflow{Id: "w2", Name: "web"}),
	)})
	before := slices.Clone(m.listing.workflows)

	m.applyWatch(client.WatchBatch{
		putEvent(workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "zzz"})),
	})

	assert.Equal(t, "w2", m.listing.workflows[0].ID)
	assert.Equal(t, "zzz", m.listing.workflows[1].Name)
	assert.NotEqual(t, before, m.listing.workflows, "and the rows compared against are the ones from before the event")
}

// TestLostStreamIsReportedUntilTheNextSnapshot covers the server going away being said once
// when the stream ends, and cleared by the snapshot the next stream opens with.
func TestLostStreamIsReportedUntilTheNextSnapshot(t *testing.T) {
	m := watchedPanel()

	m, _ = update(t, m, watchLostMsg{err: errors.New("the server hung up")})
	require.Error(t, m.unreachable)
	require.Len(t, m.messages.items, 1)

	m, _ = update(t, m, watchLostMsg{err: errors.New("connection refused")})
	assert.Len(t, m.messages.items, 1, "a stream that keeps failing to open says nothing more")

	m, _ = update(t, m, client.WatchBatch{snapshotEvent()})
	require.NoError(t, m.unreachable)

	m, _ = update(t, m, watchLostMsg{})
	require.NoError(t, m.unreachable, "a close the service chose is reopened without a word")
	assert.Len(t, m.messages.items, 1)
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
