package ui

import (
	"context"
	"testing"

	"weave/lib/xptr"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	agentstore "weave/internal/store/agent"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	bare := markGutter(agentstore.Agent{})
	assert.Equal(t, "  ", bare)

	marked := markGutter(agentstore.Agent{Mark: xptr.Ptr("a")})
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
			m.listing.agents[0].agent.Mark = tt.holding
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
			markAgent(&m, tt.marked, "c")
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
	markAgent(&m, "a3", "c")
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

func markAgent(m *model, id, letter string) {
	for i, item := range m.listing.agents {
		if item.agent.ID == id {
			m.listing.agents[i].agent.Mark = xptr.Ptr(letter)

			return
		}
	}
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
	id   string
	mark *string
}

func (f *fakeMarkAgents) SetMark(_ context.Context, req *connect.Request[agentv1.SetMarkRequest]) (*connect.Response[agentv1.SetMarkResponse], error) {
	f.id = req.Msg.GetId()
	f.mark = req.Msg.Mark

	return connect.NewResponse(&agentv1.SetMarkResponse{}), nil
}
