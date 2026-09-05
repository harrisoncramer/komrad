package ui

import (
	"context"
	"testing"

	"weave/lib/xptr"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestIsMarkLetter pins what may be a mark, since the key after the binding is whatever the
// person happened to press.
func TestIsMarkLetter(t *testing.T) {
	for _, key := range []string{"a", "m", "z"} {
		assert.True(t, isMarkLetter(key), "key %q", key)
	}

	for _, key := range []string{"A", "Z", "1", "esc", "ctrl+a", "'", "", "ab", "●"} {
		assert.False(t, isMarkLetter(key), "key %q", key)
	}
}

// TestMarkGutter pins the gutter keeping one width whether or not the agent is marked, since
// a row that shifted sideways when marked would unalign the list.
func TestMarkGutter(t *testing.T) {
	withColor(t)

	bare := markGutter("")
	assert.Equal(t, "  ", bare)

	marked := markGutter("a")
	assert.Contains(t, marked, "a")
	assert.Equal(t, lipgloss.Width(bare), lipgloss.Width(marked), "a marked row is drawn no wider than an unmarked one")
}

// TestSetMark covers the two keys that put a letter on the hovered agent, including the second
// press of the same letter that takes it off again.
func TestSetMark(t *testing.T) {
	tests := []struct {
		name    string
		holding *string
		letter  string
		wantID  string
		want    *string
	}{
		{
			name:   "an unmarked agent takes the letter",
			letter: "a",
			wantID: "a1",
			want:   xptr.Ptr("a"),
		},
		{
			name:    "a letter another agent holds is still set, since the service moves it",
			holding: xptr.Ptr("b"),
			letter:  "a",
			wantID:  "a1",
			want:    xptr.Ptr("a"),
		},
		{
			name:    "the letter the agent already holds takes the mark off it",
			holding: xptr.Ptr("a"),
			letter:  "a",
			wantID:  "a1",
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := &fakeMarkAgents{}
			m := markTree()
			m.api = api{agents: agents}
			if tt.holding != nil {
				m.listing.marks = map[string]string{agentRowKey("a1"): *tt.holding}
			}
			m.tree.cursor = rowIndexOfKey(m.agentRows(), agentRowKey("a1"))

			next, cmd := m.Update(keyMsg(m.keys.Marks.Set[0]))
			pending, ok := next.(model)
			require.True(t, ok)
			require.Nil(t, cmd, "the binding alone writes nothing")
			assert.Equal(t, markPendingSet, pending.marks.pending)

			_, cmd = pending.Update(keyMsg(tt.letter))
			require.NotNil(t, cmd, "the letter is what writes")
			cmd()

			assert.Equal(t, tt.wantID, agents.id)
			assert.Equal(t, tt.want, agents.mark)
		})
	}
}

// TestSetMarkOnAWorkflow pins a header taking a letter through the same two keys, since the
// letters name rows rather than agents.
func TestSetMarkOnAWorkflow(t *testing.T) {
	agents := &fakeMarkAgents{}
	m := markTree()
	m.api = api{agents: agents}
	m.tree.cursor = rowIndexOfKey(m.agentRows(), workflowRowKey("w1"))
	require.GreaterOrEqual(t, m.tree.cursor, 0)

	pending, _ := m.Update(keyMsg(m.keys.Marks.Set[0]))
	_, cmd := pending.(model).Update(keyMsg("a"))
	require.NotNil(t, cmd)
	cmd()

	assert.Equal(t, "w1", agents.workflowID)
	assert.Empty(t, agents.id, "a header is not marked as though it were an agent")
	assert.Equal(t, xptr.Ptr("a"), agents.mark)
}

// TestSetMarkClearsAWorkflowsOwnLetter pins the second press taking it off a header too.
func TestSetMarkClearsAWorkflowsOwnLetter(t *testing.T) {
	agents := &fakeMarkAgents{}
	m := markTree()
	m.api = api{agents: agents}
	m.listing.marks = map[string]string{workflowRowKey("w1"): "a"}
	m.tree.cursor = rowIndexOfKey(m.agentRows(), workflowRowKey("w1"))

	pending, _ := m.Update(keyMsg(m.keys.Marks.Set[0]))
	_, cmd := pending.(model).Update(keyMsg("a"))
	require.NotNil(t, cmd)
	cmd()

	assert.Equal(t, "w1", agents.workflowID)
	assert.Nil(t, agents.mark, "the letter it already answers to takes the mark off")
}

// TestJumpMarkReachesAWorkflow pins a header being somewhere a jump lands.
func TestJumpMarkReachesAWorkflow(t *testing.T) {
	m := markTree()
	markRow(&m, workflowRowKey("w2"))
	m.tree.cursor = rowIndexOfKey(m.agentRows(), agentRowKey("a1"))

	jumped := pressMark(t, m, m.keys.Marks.Jump[0], "c")
	assert.Nil(t, jumped.banner)
	assert.Equal(t, workflowRowKey("w2"), rowKeyAt(jumped.agentRows(), jumped.tree.cursor))
}

// TestSetMarkRefusesWhatIsNotALetter pins a stray key aborting the mark rather than being
// taken as one, since every key on the screen is reachable while the panel is waiting.
func TestSetMarkRefusesWhatIsNotALetter(t *testing.T) {
	agents := &fakeMarkAgents{}
	m := markTree()
	m.api = api{agents: agents}
	m.tree.cursor = rowIndexOfKey(m.agentRows(), agentRowKey("a1"))

	pending, _ := m.Update(keyMsg(m.keys.Marks.Set[0]))
	next, cmd := pending.(model).Update(keyMsg("1"))
	assert.Nil(t, cmd, "nothing is written")

	after, ok := next.(model)
	require.True(t, ok)
	assert.Equal(t, markPendingNone, after.marks.pending, "the press is spent either way")
	require.NotNil(t, after.banner)
	assert.Empty(t, agents.id)
}

// TestSetMarkEscapes pins escape backing out of a mark without saying anything went wrong.
func TestSetMarkEscapes(t *testing.T) {
	agents := &fakeMarkAgents{}
	m := markTree()
	m.api = api{agents: agents}
	m.tree.cursor = rowIndexOfKey(m.agentRows(), agentRowKey("a1"))

	pending, _ := m.Update(keyMsg(m.keys.Marks.Set[0]))
	next, cmd := pending.(model).Update(keyMsg("esc"))
	assert.Nil(t, cmd)

	after, ok := next.(model)
	require.True(t, ok)
	assert.Equal(t, markPendingNone, after.marks.pending)
	assert.Nil(t, after.banner)
	assert.Empty(t, agents.id)
}

// TestSetMarkOwnsTheKeyAfterIt pins the letter being read as a mark even when it is a key the
// screen otherwise acts on, which is what makes every letter reachable.
func TestSetMarkOwnsTheKeyAfterIt(t *testing.T) {
	agents := &fakeMarkAgents{}
	m := markTree()
	m.api = api{agents: agents}
	m.tree.cursor = rowIndexOfKey(m.agentRows(), agentRowKey("a1"))

	pending, _ := m.Update(keyMsg(m.keys.Marks.Set[0]))
	_, cmd := pending.(model).Update(keyMsg(m.keys.Agents.Hide[0]))
	require.NotNil(t, cmd)
	cmd()

	assert.Equal(t, "a1", agents.id)
	assert.Equal(t, xptr.Ptr(m.keys.Agents.Hide[0]), agents.mark, "the hide key is read as the letter it is")
}

// TestSetMarkEscapesPastABanner pins escape backing out of a mark even while a banner is up,
// since the banner's own escape would otherwise leave the panel waiting for a letter and take
// the next key pressed as one.
func TestSetMarkEscapesPastABanner(t *testing.T) {
	agents := &fakeMarkAgents{}
	m := markTree()
	m.api = api{agents: agents}
	m.tree.cursor = rowIndexOfKey(m.agentRows(), agentRowKey("a1"))
	m.report("something went wrong earlier", errMarkLetter)

	pending, _ := m.Update(keyMsg(m.keys.Marks.Set[0]))
	next, _ := pending.(model).Update(keyMsg("esc"))

	after, ok := next.(model)
	require.True(t, ok)
	require.Equal(t, markPendingNone, after.marks.pending, "the mark is what escape backed out of")

	moved, cmd := after.Update(keyMsg(after.keys.Nav.Down[0]))
	assert.Nil(t, cmd)
	assert.Empty(t, agents.id, "the next key is not read as a letter")

	_, ok = moved.(model)
	require.True(t, ok)
}

// TestJumpMark covers the cursor landing on a marked agent from wherever it was, including the
// rows a filter or a hidden flag would otherwise keep it off.
func TestJumpMark(t *testing.T) {
	tests := []struct {
		name   string
		marked string
		setup  func(m *model)
	}{
		{
			name:   "an agent further down the list",
			marked: "a3",
		},
		{
			name:   "an agent in another workflow",
			marked: "a4",
		},
		{
			name:   "an agent the filter is keeping off the list",
			marked: "a3",
			setup:  func(m *model) { m.filter.setQuery("auth") },
		},
		{
			name:   "a hidden agent, which the jump reveals rather than refusing",
			marked: "a5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := markTree()
			markAgent(&m, tt.marked)
			if tt.setup != nil {
				tt.setup(&m)
			}

			pending, cmd := m.Update(keyMsg(m.keys.Marks.Jump[0]))
			require.Nil(t, cmd)
			next, _ := pending.(model).Update(keyMsg("c"))

			after, ok := next.(model)
			require.True(t, ok)
			assert.Nil(t, after.banner)
			assert.Equal(t, agentRowKey(tt.marked), rowKeyAt(after.agentRows(), after.tree.cursor))
		})
	}
}

// TestJumpMarkThatCannotLandKeepsTheScreen pins a jump that fails leaving the filter and the
// hidden rows as they were, since revealing on the way to nowhere loses what you were looking at.
func TestJumpMarkThatCannotLandKeepsTheScreen(t *testing.T) {
	m := markTree()
	markAgent(&m, "a3")
	// A row whose workflow is not in the listing is drawn under no header, which neither
	// clearing the filter nor showing hidden rows can reach.
	m.listing.workflows = nil
	m.filter.setQuery("auth")

	pending, _ := m.Update(keyMsg(m.keys.Marks.Jump[0]))
	next, _ := pending.(model).Update(keyMsg("c"))

	after, ok := next.(model)
	require.True(t, ok)
	require.NotNil(t, after.banner)
	assert.Equal(t, "auth", after.filter.query, "the filter stands")
	assert.False(t, after.tree.showHidden, "and the hidden rows stay hidden")
}

// TestJumpMarkFromAHeaderComesBack pins the row jumped from being remembered even when it is a
// workflow header, which holds no agent to name it by.
func TestJumpMarkFromAHeaderComesBack(t *testing.T) {
	m := markTree()
	markAgent(&m, "a3")
	m.tree.cursor = rowIndexOfKey(m.agentRows(), workflowRowKey("w1"))
	require.GreaterOrEqual(t, m.tree.cursor, 0)

	jumped := pressMark(t, m, m.keys.Marks.Jump[0], "c")
	require.Equal(t, agentRowKey("a3"), rowKeyAt(jumped.agentRows(), jumped.tree.cursor))

	back := pressMark(t, jumped, m.keys.Marks.Jump[0], m.keys.Marks.Jump[0])
	assert.Equal(t, workflowRowKey("w1"), rowKeyAt(back.agentRows(), back.tree.cursor))
}

// TestJumpMarkBackToARowThatHasGone pins the key saying so and forgetting the row, rather than
// failing the same way for the rest of the session.
func TestJumpMarkBackToARowThatHasGone(t *testing.T) {
	m := markTree()
	markAgent(&m, "a3")
	m.tree.cursor = rowIndexOfKey(m.agentRows(), agentRowKey("a1"))

	jumped := pressMark(t, m, m.keys.Marks.Jump[0], "c")
	require.Equal(t, agentRowKey("a1"), jumped.marks.origin)

	jumped.listing.agents = jumped.listing.agents[1:]
	at := rowKeyAt(jumped.agentRows(), jumped.tree.cursor)

	after := pressMark(t, jumped, m.keys.Marks.Jump[0], m.keys.Marks.Jump[0])
	require.NotNil(t, after.banner)
	assert.Equal(t, "Could not jump back", after.banner.Summary, "the key says what it failed at")
	assert.Empty(t, after.marks.origin, "the row that has gone is forgotten")
	assert.Equal(t, at, rowKeyAt(after.agentRows(), after.tree.cursor), "and the cursor stays where it was")
}

// TestJumpMarkKeepsAStandingSearch pins a jump that reveals a hidden row leaving the search
// alone, since nothing a search does hides a row and dropping it loses what you were reading.
func TestJumpMarkKeepsAStandingSearch(t *testing.T) {
	m := markTree()
	markRow(&m, agentRowKey("a5"))
	m.filter.setQuery("auth")

	jumped := pressMark(t, m, m.keys.Marks.Jump[0], "c")

	assert.True(t, jumped.filter.isStanding(), "the search survives the jump")
	assert.Equal(t, "auth", jumped.filter.query)
	assert.True(t, jumped.tree.showHidden, "and the hidden row it landed on is revealed")
	assert.Equal(t, agentRowKey("a5"), rowKeyAt(jumped.agentRows(), jumped.tree.cursor))
}

// TestJumpMarkUnknownLetter pins a letter nobody holds leaving the cursor where it was and
// saying so, rather than moving somewhere arbitrary.
func TestJumpMarkUnknownLetter(t *testing.T) {
	m := markTree()
	m.tree.cursor = rowIndexOfKey(m.agentRows(), agentRowKey("a1"))

	pending, _ := m.Update(keyMsg(m.keys.Marks.Jump[0]))
	next, _ := pending.(model).Update(keyMsg("q"))

	after, ok := next.(model)
	require.True(t, ok)
	require.NotNil(t, after.banner)
	assert.Equal(t, agentRowKey("a1"), rowKeyAt(after.agentRows(), after.tree.cursor))
}

// TestJumpMarkBack pins the jump key pressed twice returning to the row the last jump left, so
// looking at a marked agent and coming back are one gesture.
func TestJumpMarkBack(t *testing.T) {
	m := markTree()
	markAgent(&m, "a3")
	m.tree.cursor = rowIndexOfKey(m.agentRows(), agentRowKey("a1"))

	jumped := pressMark(t, m, m.keys.Marks.Jump[0], "c")
	require.Equal(t, agentRowKey("a3"), rowKeyAt(jumped.agentRows(), jumped.tree.cursor))

	back := pressMark(t, jumped, m.keys.Marks.Jump[0], m.keys.Marks.Jump[0])
	assert.Equal(t, agentRowKey("a1"), rowKeyAt(back.agentRows(), back.tree.cursor))

	again := pressMark(t, back, m.keys.Marks.Jump[0], m.keys.Marks.Jump[0])
	assert.Equal(t, agentRowKey("a3"), rowKeyAt(again.agentRows(), again.tree.cursor), "and back again")
}

// TestJumpMarkBackWithNowhereToGo pins the jump key pressed twice before any jump doing
// nothing rather than moving the cursor somewhere it has never been.
func TestJumpMarkBackWithNowhereToGo(t *testing.T) {
	m := markTree()
	m.tree.cursor = rowIndexOfKey(m.agentRows(), agentRowKey("a1"))

	after := pressMark(t, m, m.keys.Marks.Jump[0], m.keys.Marks.Jump[0])
	assert.Nil(t, after.banner)
	assert.Equal(t, agentRowKey("a1"), rowKeyAt(after.agentRows(), after.tree.cursor))
}

func markTree() model {
	m := selectionTree()
	m.api = api{agents: &fakeMarkAgents{}}

	return m
}

func markAgent(m *model, id string) {
	markRow(m, agentRowKey(id))
}

func markRow(m *model, key string) {
	if m.listing.marks == nil {
		m.listing.marks = map[string]string{}
	}
	m.listing.marks[key] = "c"
}

func pressMark(t *testing.T, m model, keys ...string) model {
	t.Helper()

	var next tea.Model = m
	for _, key := range keys {
		updated, _ := next.(model).Update(keyMsg(key))
		next = updated
	}

	after, ok := next.(model)
	require.True(t, ok)

	return after
}

type fakeMarkAgents struct {
	agentv1connect.AgentServiceClient
	id         string
	workflowID string
	mark       *string
}

func (f *fakeMarkAgents) SetMark(_ context.Context, req *connect.Request[agentv1.SetMarkRequest]) (*connect.Response[emptypb.Empty], error) {
	f.id = req.Msg.GetAgentId()
	f.workflowID = req.Msg.GetWorkflowId()
	f.mark = req.Msg.Mark

	return connect.NewResponse(&emptypb.Empty{}), nil
}
