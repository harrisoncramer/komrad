package ui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/paint"
	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingHolds answers the hold calls and keeps what the panel sent.
type recordingHolds struct {
	agentv1connect.AgentServiceClient
	held     []string
	released []*agentv1.ReleaseAgentRequest
}

// refusingHolds refuses the first release and takes every one after it.
type refusingHolds struct {
	recordingHolds
	refusals int
}

func (r *recordingHolds) HoldAgent(_ context.Context, req *connect.Request[agentv1.HoldAgentRequest]) (*connect.Response[agentv1.HoldAgentResponse], error) {
	r.held = append(r.held, req.Msg.GetId())

	return connect.NewResponse(&agentv1.HoldAgentResponse{}), nil
}

func (r *recordingHolds) ReleaseAgent(_ context.Context, req *connect.Request[agentv1.ReleaseAgentRequest]) (*connect.Response[agentv1.ReleaseAgentResponse], error) {
	r.released = append(r.released, req.Msg)

	return connect.NewResponse(&agentv1.ReleaseAgentResponse{}), nil
}

func (r *recordingHolds) ListAgents(context.Context, *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
}

func (r *refusingHolds) ReleaseAgent(ctx context.Context, req *connect.Request[agentv1.ReleaseAgentRequest]) (*connect.Response[agentv1.ReleaseAgentResponse], error) {
	if r.refusals > 0 {
		r.refusals--

		return nil, errors.New("the input box would not take the words")
	}

	return r.recordingHolds.ReleaseAgent(ctx, req)
}

// TestRecordCopy covers the copy key taking a field's value out of the record.
func TestRecordCopy(t *testing.T) {
	t.Run("the value is taken rather than the clipped line it is drawn on", func(t *testing.T) {
		clip := &fakeClipboard{}
		m := detailsModel(&recordingTmux{})
		m.clip = clip

		bottom := send(t, send(t, m, "ctrl+t"), "G")
		copied, cmd := bottom.updateDetails(keyMsg("y"))
		require.NotNil(t, cmd)

		require.NoError(t, copyResultFrom(t, cmd).err)
		assert.Equal(t, []string{"https://github.com/o/r/pull/312"}, clip.written)
		assert.Positive(t, requireModel(t, copied).details.flashed, "the copied field flashes while the write runs")
	})

	t.Run("the field on screen is taken after the row changes under the record", func(t *testing.T) {
		clip := &fakeClipboard{}
		m := detailsModel(&recordingTmux{})
		m.clip = clip
		m.agents.placed = true
		down := send(t, send(t, send(t, send(t, m, "ctrl+t"), "j"), "j"), "j")

		applyListing(&down, down.listing.workflows)
		held := down.hoveredDetails()
		require.Equal(t, "w1", held.id, "the workflow header took the archived agent's place")

		copied, cmd := down.updateDetails(keyMsg("y"))
		require.NotNil(t, cmd)
		require.NoError(t, copyResultFrom(t, cmd).err)

		assert.Equal(t, []string{held.fields[0].value}, clip.written)
		assert.Equal(t, 1, requireModel(t, copied).details.flashed, "and flashes the line it took it from")
	})

	t.Run("a narrowed record copies from the rows that are left", func(t *testing.T) {
		clip := &fakeClipboard{}
		m := detailsModel(&recordingTmux{})
		m.height, m.width = 40, 60
		m.clip = clip
		m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/repo", inspection: readWorktree(time.Now())})

		narrowed := pressMark(t, send(t, m, "ctrl+t"), "/", "c", "o", "m", "m", "i", "t", "tab", "enter", "G")
		_, cmd := narrowed.copyDetailsField()
		require.NotNil(t, cmd)
		collectMessages(cmd)

		assert.Equal(t, []string{"1234567890ab  3h   feat(ui): a thing"}, clip.written)
	})

	t.Run("a clipboard that refuses the write is reported", func(t *testing.T) {
		m := detailsModel(&recordingTmux{})
		m.clip = &fakeClipboard{err: errors.New("no clipboard utility found")}

		_, cmd := send(t, m, "ctrl+t").updateDetails(keyMsg("y"))
		require.NotNil(t, cmd)

		assert.EqualError(t, copyResultFrom(t, cmd).err, "no clipboard utility found")
	})
}

// TestRecordEmphasis covers what the record paints that a stripped frame cannot show.
func TestRecordEmphasis(t *testing.T) {
	t.Run("a search typed on the list lights its match in the record", func(t *testing.T) {
		typed := pressMark(t, detailsModel(&recordingTmux{}), "/", "a", "u", "t", "h")
		held := typed.hoveredDetails()

		rows := typed.detailFieldRows(typed.detailsRows(held), typed.detailsCursor(held))

		assert.Contains(t, rows[0], paint.Panel.Match().Render("auth"))
	})

	t.Run("a search standing over the record lights the matched run of a value", func(t *testing.T) {
		m := detailsModel(&recordingTmux{})
		m.height, m.width = 40, 60
		m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/repo", inspection: readWorktree(time.Now())})
		standing := pressMark(t, send(t, m, "ctrl+t"), "/", "o", "r", "i", "g", "i", "n", "enter")
		held := standing.hoveredDetails()

		rows := standing.detailFieldRows(standing.detailsRows(held), standing.detailsCursor(held))

		base := slices.IndexFunc(held.fields, func(field recordField) bool {
			return field.label == "base"
		})
		assert.Contains(t, ansi.Strip(rows[base]), "origin/main  ↑3 ↓12")
		assert.Contains(t, rows[base], paint.Panel.Match().Render("origin"))
	})

	t.Run("the hovered label takes the accent only while the record has the keys", func(t *testing.T) {
		m := detailsModel(&recordingTmux{})
		held := m.hoveredDetails()
		label := padRight(held.fields[0].label, labelColumn(held.fields))

		blurred := m.detailFieldRows(m.detailsRows(held), 0)[0]
		m.details.focused = true
		lit := m.detailFieldRows(m.detailsRows(held), 0)[0]

		assert.Contains(t, blurred, dimStyle.Render(label))
		assert.Contains(t, lit, rowCursorStyle().Render(label))
		assert.Equal(t, 1, strings.Count(ansi.Strip(lit), "›"))
	})
}

// TestWorktreeReadGoesStale covers an answer about the hovered agent's directory being asked for again once it is old.
func TestWorktreeReadGoesStale(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	now := time.Now()
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/repo", inspection: readWorktree(now)})

	assert.Nil(t, m.inspectAgentWorktreeCmd(now), "a fresh answer is not asked for again")
	assert.NotNil(t, m.inspectAgentWorktreeCmd(now.Add(worktreeReadTTL+time.Second)), "a stale one is")
}

// TestWorkflowRowNamesItsOrchestratorsPane covers the pane the warm-up clock reads for a workflow's row.
func TestWorkflowRowNamesItsOrchestratorsPane(t *testing.T) {
	m := model{
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		listing: listing{
			workflows: []panel.Workflow{{ID: "w1", Name: "backend"}},
			agents: []panel.AgentRow{{
				Agent: panel.Agent{
					ID:         "o1",
					Name:       "orchestrator",
					Pane:       "%9",
					Harness:    "claude",
					WorkflowID: "w1",
					Role:       panel.RoleOrchestrator,
				},
				Alive: true,
			}},
		},
	}

	pane, harness := m.selectedAgentPane()

	assert.Equal(t, "%9", pane)
	assert.Equal(t, "claude", harness)
}

// TestARefusedUnpauseKeepsTheEditedFiles covers the retry the panel's own advice asks for.
func TestARefusedUnpauseKeepsTheEditedFiles(t *testing.T) {
	at := time.Now()
	agents := &refusingHolds{refusals: 1}

	m := heldModel(agents, &at)
	m.noteEdited("a1", []string{"internal/agents/hold.go"})

	refused, cmd := m.Update(keyMsg("p"))
	first := requireModel(t, refused)
	first = settle(t, first, cmd)

	require.Empty(t, agents.released, "the refusal sent nothing")

	pressed, cmd := first.Update(keyMsg("p"))
	retried := requireModel(t, pressed)
	settle(t, retried, cmd)

	require.Len(t, agents.released, 1)
	assert.Contains(t, agents.released[0].GetSaid(), "internal/agents/hold.go",
		"the retry after a refusal still names the edited file")
}

// TestASilentUnpauseForgetsTheEditedFiles covers the release that says nothing.
func TestASilentUnpauseForgetsTheEditedFiles(t *testing.T) {
	at := time.Now()
	agents := &recordingHolds{}

	m := heldModel(agents, &at)
	m.noteEdited("a1", []string{"internal/agents/hold.go"})

	pressed, _ := m.Update(keyMsg("P"))
	opened := requireModel(t, pressed)
	require.Equal(t, formAgentRelease, opened.form.kind)

	got := answerPrompt(t, opened, "y")

	require.Len(t, agents.released, 1)
	assert.Empty(t, agents.released[0].GetSaid())
	assert.Empty(t, got.edited["a1"], "a silent release leaves no paths behind for a later hold")
	require.NotEmpty(t, got.messages.items)
	assert.Contains(t, got.messages.items[0].detail, "internal/agents/hold.go",
		"the person is told what the agent was not")
}

// TestALandedUnpauseForgetsTheEditedFiles covers the release that named the files, which one
// agent hears about once.
func TestALandedUnpauseForgetsTheEditedFiles(t *testing.T) {
	at := time.Now()
	agents := &recordingHolds{}

	m := heldModel(agents, &at)
	m.noteEdited("a1", []string{"internal/agents/hold.go"})

	pressed, cmd := m.Update(keyMsg("p"))
	got := settle(t, requireModel(t, pressed), cmd)

	require.Len(t, agents.released, 1)
	assert.Contains(t, agents.released[0].GetSaid(), "internal/agents/hold.go")
	assert.Empty(t, got.edited["a1"], "a release that named the files forgets them")
}

// TestALandedUnpauseKeepsAFileOpenedWhileItFlew covers a path remembered after the release was
// built.
func TestALandedUnpauseKeepsAFileOpenedWhileItFlew(t *testing.T) {
	at := time.Now()
	agents := &recordingHolds{}

	m := heldModel(agents, &at)
	m.noteEdited("a1", []string{"internal/agents/hold.go"})

	pressed, cmd := m.Update(keyMsg("p"))
	sent := requireModel(t, pressed)
	sent.noteEdited("a1", []string{"internal/ui/refresh.go"})

	got := settle(t, sent, cmd)

	require.Len(t, agents.released, 1)
	assert.NotContains(t, agents.released[0].GetSaid(), "internal/ui/refresh.go")
	assert.Equal(t, []string{"internal/ui/refresh.go"}, got.edited["a1"],
		"a landing forgets the files it named and leaves the rest")
}

// heldModel is the agents screen standing on one agent, held or not.
func heldModel(agents agentv1connect.AgentServiceClient, held *time.Time) model {
	m := model{
		api:    api{agents: agents, workflows: emptyLists{}},
		tmux:   &recordingTmux{},
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		listing: listing{
			workflows: []panel.Workflow{{ID: "w1", Name: "backend"}},
			agents: []panel.AgentRow{{Agent: panel.Agent{
				ID:         "a1",
				Name:       "telescope",
				State:      panel.StateThinking,
				WorkflowID: "w1",
				HeldAt:     held,
			}, Alive: true}},
		},
	}
	m.agents.cursor = firstAgentRowIndex(m.agents.rows(m.shared()))

	return m
}

// agentHelp is the agents legend as the frame draws it.
func (m model) agentHelp() keyMap {
	return m.listHelp(m.agents.help(m.shared()))
}

// agentsHeader is the list's header as the frame draws it.
func (m model) agentsHeader() string {
	params := m.agents.listParams(m.shared())

	return m.tabbedHeader(params.title, params.tally)
}

func detailsModel(tmux *recordingTmux) model {
	m := infoModel(tmux)
	m.height, m.width = 24, 40
	m.clip = &fakeClipboard{}
	m.listing.agents[0].Agent.ID = "a1"
	m.listing.agents[0].Agent.SessionID = "abc"
	m.listing.agents[0].Agent.Metadata = []panel.Metadata{
		{Key: "pr", Value: "https://github.com/o/r/pull/312"},
	}

	return m
}

func send(t *testing.T, m model, key string) model {
	t.Helper()

	next, _ := m.Update(keyMsg(key))
	shown, ok := next.(model)
	require.True(t, ok)

	return shown
}

// readWorktree is a directory read through git as the tests hand it to the record.
func readWorktree(now time.Time) worktreeInspection {
	return worktreeInspection{
		answered: true,
		readAt:   now,
		item: worktreeItem{
			dir:       "/repo",
			readable:  true,
			branch:    "agent/x",
			occupants: []worktreeAgent{{id: "a2", name: "reader"}},
			size:      &worktreeSize{bytes: 1200 * 1024 * 1024, measuredAt: now},
		},
		modified:   []string{"a.go", "b.go"},
		untracked:  []string{"notes.md"},
		baseRef:    "origin/main",
		ahead:      3,
		behind:     12,
		landed:     1,
		sequencing: true,
		commits: []worktreeCommit{
			{
				sha:         "87a3bcc98c47",
				subject:     "fix(view): drop the column",
				committedAt: now.Add(-2 * time.Hour),
			},
			{
				sha:         "1234567890ab",
				subject:     "feat(ui): a thing",
				committedAt: now.Add(-3 * time.Hour),
			},
		},
	}
}
