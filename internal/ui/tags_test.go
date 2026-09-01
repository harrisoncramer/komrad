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

var tags = []config.Tag{
	{Glyph: "●", Color: "#F932A1"},
	{Glyph: "◐", Color: "#0f0"},
	{Glyph: "✓", Color: ""},
}

// TestNextTag walks the cycle the tag key drives, which covers every tag and clearing.
func TestNextTag(t *testing.T) {
	tests := []struct {
		name    string
		current *string
		tags    []config.Tag
		want    *string
	}{
		{
			name: "an untagged agent with nothing configured stays untagged",
			tags: nil,
			want: nil,
		},
		{
			name:    "a tagged agent with nothing configured clears",
			current: xptr.Ptr("●"),
			tags:    nil,
			want:    nil,
		},
		{
			name:    "a glyph the config no longer lists clears, so the key is a way back out",
			current: xptr.Ptr("★"),
			tags:    tags,
			want:    nil,
		},
		{
			name: "an untagged agent takes the first tag",
			tags: tags,
			want: xptr.Ptr("●"),
		},
		{
			name:    "a tagged agent moves along the list",
			current: xptr.Ptr("◐"),
			tags:    tags,
			want:    xptr.Ptr("✓"),
		},
		{
			name:    "the last tag clears rather than wrapping around",
			current: xptr.Ptr("✓"),
			tags:    tags,
			want:    nil,
		},
		{
			name:    "one configured tag leaves the key a toggle",
			current: xptr.Ptr("●"),
			tags:    []config.Tag{{Glyph: "●"}},
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, nextTag(tt.current, tt.tags))
		})
	}
}

// TestTagColor pins the colour of each glyph, including the ones the config never named, as
// the index lipgloss reads it back as.
func TestTagColor(t *testing.T) {
	tests := []struct {
		name  string
		glyph string
		tags  []config.Tag
		want  string
	}{
		{
			name:  "a glyph the config does not list falls back to the default yellow",
			glyph: "★",
			tags:  tags,
			want:  "3",
		},
		{
			name:  "so does one configured with no colour of its own",
			glyph: "✓",
			tags:  tags,
			want:  "3",
		},
		{
			name:  "a configured colour is the one used",
			glyph: "●",
			tags:  tags,
			want:  "#F932A1",
		},
		{
			name:  "including a three-digit one",
			glyph: "◐",
			tags:  tags,
			want:  "#0f0",
		},
		{
			name:  "a colour named rather than given in hex draws as its index",
			glyph: "◆",
			tags:  []config.Tag{{Glyph: "◆", Color: "brightcyan"}},
			want:  "14",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tagColor(tt.glyph, tt.tags)))
		})
	}
}

// TestTaggedGlyph pins the row drawing the tag itself, so an agent carrying one the config
// never listed still shows it rather than vanishing.
func TestTaggedGlyph(t *testing.T) {
	tests := []struct {
		name string
		tag  *string
		want string
	}{
		{
			name: "an untagged agent draws nothing",
		},
		{
			name: "a tagged agent draws its glyph",
			tag:  xptr.Ptr("●"),
			want: "●",
		},
		{
			name: "a glyph the config does not list still draws",
			tag:  xptr.Ptr("★"),
			want: "★",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tagGlyph(agentstore.Agent{Tag: tt.tag}, tags)
			if tt.want == "" {
				assert.Empty(t, got)
				return
			}

			assert.Contains(t, got, tt.want)
		})
	}
}

// TestNextSelectionTag walks the cycle over a set of agents, where the next tag is decided
// by whether they already agree.
func TestNextSelectionTag(t *testing.T) {
	tests := []struct {
		name    string
		current []*string
		tags    []config.Tag
		want    *string
	}{
		{
			name: "a selection with nothing in it has nowhere to go",
			tags: tags,
		},
		{
			name:    "nor does one with nothing configured",
			current: []*string{xptr.Ptr("●"), nil},
		},
		{
			name:    "agents that disagree collapse onto the first tag",
			current: []*string{xptr.Ptr("●"), nil},
			tags:    tags,
			want:    xptr.Ptr("●"),
		},
		{
			name:    "including when none of them holds it",
			current: []*string{xptr.Ptr("◐"), xptr.Ptr("✓")},
			tags:    tags,
			want:    xptr.Ptr("●"),
		},
		{
			name:    "untagged agents take the first tag, as one does",
			current: []*string{nil, nil},
			tags:    tags,
			want:    xptr.Ptr("●"),
		},
		{
			name:    "agents that agree step on together",
			current: []*string{xptr.Ptr("◐"), xptr.Ptr("◐")},
			tags:    tags,
			want:    xptr.Ptr("✓"),
		},
		{
			name:    "and clear off the end of the list rather than wrapping",
			current: []*string{xptr.Ptr("✓"), xptr.Ptr("✓")},
			tags:    tags,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, nextSelectionTag(tt.current, tt.tags))
		})
	}
}

// TestTagSelection pins every chosen agent taking the same glyph in one call, with the
// selection still up so the key can be pressed again.
func TestTagSelection(t *testing.T) {
	tests := []struct {
		name     string
		carrying map[string]string
		want     *string
	}{
		{
			name: "untagged agents take the first tag",
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
			agents := &fakeTagAgents{}
			chosen := []string{"a1", "a2"}

			m := selectionTree()
			m.api = api{agents: agents}
			m.tags = tags
			m.tree.selected = selection{ids: chosen}

			for i, item := range m.listing.agents {
				if tag, ok := tt.carrying[item.agent.ID]; ok {
					m.listing.agents[i].agent.Tag = xptr.Ptr(tag)
				}
			}

			next, cmd := m.updateAgents(keyMsg("t"))
			require.NotNil(t, cmd, "the tag key is answered while a selection is up")
			cmd()

			assert.Equal(t, chosen, agents.ids, "every chosen agent goes in one call")
			assert.Equal(t, tt.want, agents.tag)

			after, ok := next.(model)
			require.True(t, ok)
			assert.Equal(t, chosen, after.tree.selected.ids, "the selection stays up to be pressed again")
		})
	}
}

type fakeTagAgents struct {
	agentv1connect.AgentServiceClient
	ids []string
	tag *string
}

func (f *fakeTagAgents) SetTags(_ context.Context, req *connect.Request[agentv1.SetTagsRequest]) (*connect.Response[emptypb.Empty], error) {
	f.ids = req.Msg.GetIds()
	f.tag = req.Msg.Tag

	return connect.NewResponse(&emptypb.Empty{}), nil
}
