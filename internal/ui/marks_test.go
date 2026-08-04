package ui

import (
	"context"
	"testing"

	"weave/config"
	"weave/lib/xptr"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	agentstore "weave/internal/store/agent"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// marks are the configured marks the cases below step through, each in a colour of
// its own so a row drawing the wrong one shows up.
var marks = []config.Mark{
	{Glyph: "●", Color: "#F932A1"},
	{Glyph: "◐", Color: "#0f0"},
	{Glyph: "✓", Color: ""},
}

// TestNextMark walks the cycle the mark key drives. One key covers every mark and
// clearing, so where it goes next is the whole behaviour.
func TestNextMark(t *testing.T) {
	tests := []struct {
		name    string
		current *string
		marks   []config.Mark
		want    *string
	}{
		{
			name:  "an unmarked agent with nothing configured stays unmarked",
			marks: nil,
			want:  nil,
		},
		{
			name:    "a marked agent with nothing configured clears",
			current: xptr.Ptr("●"),
			marks:   nil,
			want:    nil,
		},
		{
			name:    "a glyph the config no longer lists clears, so the key is a way back out",
			current: xptr.Ptr("★"),
			marks:   marks,
			want:    nil,
		},
		{
			name:  "an unmarked agent takes the first mark",
			marks: marks,
			want:  xptr.Ptr("●"),
		},
		{
			name:    "a marked agent moves along the list",
			current: xptr.Ptr("◐"),
			marks:   marks,
			want:    xptr.Ptr("✓"),
		},
		{
			name:    "the last mark clears rather than wrapping around",
			current: xptr.Ptr("✓"),
			marks:   marks,
			want:    nil,
		},
		{
			name:    "one configured mark leaves the key a toggle",
			current: xptr.Ptr("●"),
			marks:   []config.Mark{{Glyph: "●"}},
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, nextMark(tt.current, tt.marks))
		})
	}
}

// TestMarkColor covers which colour a glyph is drawn in, including the glyphs the
// config has nothing to say about. A colour comes back as lipgloss reads it, so a
// configured name arrives here as the index it draws as.
func TestMarkColor(t *testing.T) {
	tests := []struct {
		name  string
		glyph string
		marks []config.Mark
		want  string
	}{
		{
			name:  "a glyph the config does not list falls back to the default yellow",
			glyph: "★",
			marks: marks,
			want:  "3",
		},
		{
			name:  "so does one configured with no colour of its own",
			glyph: "✓",
			marks: marks,
			want:  "3",
		},
		{
			name:  "a configured colour is the one used",
			glyph: "●",
			marks: marks,
			want:  "#F932A1",
		},
		{
			name:  "including a three-digit one",
			glyph: "◐",
			marks: marks,
			want:  "#0f0",
		},
		{
			name:  "a colour named rather than given in hex draws as its index",
			glyph: "◆",
			marks: []config.Mark{{Glyph: "◆", Color: "brightcyan"}},
			want:  "14",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(markColor(tt.glyph, tt.marks)))
		})
	}
}

// TestMarkedMark covers what a row draws. The glyph is the mark itself, so an agent
// carrying one the config never listed still shows it rather than vanishing. Which
// colour it takes is TestMarkColor's business, since lipgloss only writes the escape
// when it decides the terminal has a profile.
func TestMarkedMark(t *testing.T) {
	tests := []struct {
		name string
		mark *string
		want string
	}{
		{
			name: "an unmarked agent draws nothing",
		},
		{
			name: "a marked agent draws its glyph",
			mark: xptr.Ptr("●"),
			want: "●",
		},
		{
			name: "a glyph the config does not list still draws",
			mark: xptr.Ptr("★"),
			want: "★",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := markGlyph(agentstore.Agent{Mark: tt.mark}, marks)
			if tt.want == "" {
				assert.Empty(t, got)
				return
			}

			assert.Contains(t, got, tt.want)
		})
	}
}

// TestNextSelectionMark walks the cycle over a set of agents rather than one. A selection
// carries no single mark to step from, so where the key goes is decided by whether the
// agents in it agree.
func TestNextSelectionMark(t *testing.T) {
	tests := []struct {
		name    string
		current []*string
		marks   []config.Mark
		want    *string
	}{
		{
			name:  "a selection with nothing in it has nowhere to go",
			marks: marks,
		},
		{
			name:    "nor does one with nothing configured",
			current: []*string{xptr.Ptr("●"), nil},
		},
		{
			name:    "agents that disagree collapse onto the first mark",
			current: []*string{xptr.Ptr("●"), nil},
			marks:   marks,
			want:    xptr.Ptr("●"),
		},
		{
			name:    "including when none of them holds it",
			current: []*string{xptr.Ptr("◐"), xptr.Ptr("✓")},
			marks:   marks,
			want:    xptr.Ptr("●"),
		},
		{
			name:    "unmarked agents take the first mark, as one does",
			current: []*string{nil, nil},
			marks:   marks,
			want:    xptr.Ptr("●"),
		},
		{
			name:    "agents that agree step on together",
			current: []*string{xptr.Ptr("◐"), xptr.Ptr("◐")},
			marks:   marks,
			want:    xptr.Ptr("✓"),
		},
		{
			name:    "and clear off the end of the list rather than wrapping",
			current: []*string{xptr.Ptr("✓"), xptr.Ptr("✓")},
			marks:   marks,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, nextSelectionMark(tt.current, tt.marks))
		})
	}
}

// TestMarkSelection covers the mark key while a selection is up. Every chosen agent takes
// the same glyph in one call, and the selection stays afterwards, since a cycle nobody can
// press twice is not one.
func TestMarkSelection(t *testing.T) {
	tests := []struct {
		name string
		// carrying is what the chosen agents already hold, keyed by agent id.
		carrying map[string]string
		want     *string
	}{
		{
			name: "unmarked agents take the first mark",
			want: xptr.Ptr("●"),
		},
		{
			name:     "agents that disagree collapse onto it",
			carrying: map[string]string{"a1": "◐"},
			want:     xptr.Ptr("●"),
		},
		{
			name:     "agents that agree step on",
			carrying: map[string]string{"a1": "◐", "a2": "◐"},
			want:     xptr.Ptr("✓"),
		},
		{
			name:     "and clear off the end of the list",
			carrying: map[string]string{"a1": "✓", "a2": "✓"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := &fakeMarkAgents{}
			chosen := []string{"a1", "a2"}

			m := selectionTree()
			m.api = api{agents: agents}
			m.marks = marks
			m.tree.selected = selection{ids: chosen}

			for i, item := range m.tree.agents {
				if mark, ok := tt.carrying[item.agent.ID]; ok {
					m.tree.agents[i].agent.Mark = xptr.Ptr(mark)
				}
			}

			next, cmd := m.updateAgents(keyMsg("m"))
			require.NotNil(t, cmd, "the mark key is answered while a selection is up")
			cmd()

			assert.Equal(t, chosen, agents.ids, "every chosen agent goes in one call")
			assert.Equal(t, tt.want, agents.mark)

			after, ok := next.(model)
			require.True(t, ok)
			assert.Equal(t, chosen, after.tree.selected.ids, "the selection stays up to be pressed again")
		})
	}
}

// fakeMarkAgents records what the panel asked the service to mark. Every other method is
// left to the embedded nil interface, so a press reaching one panics rather than passing.
type fakeMarkAgents struct {
	agentv1connect.AgentServiceClient
	ids  []string
	mark *string
}

func (f *fakeMarkAgents) SetMarks(_ context.Context, req *connect.Request[agentv1.SetMarksRequest]) (*connect.Response[emptypb.Empty], error) {
	f.ids = req.Msg.GetIds()
	f.mark = req.Msg.Mark

	return connect.NewResponse(&emptypb.Empty{}), nil
}
