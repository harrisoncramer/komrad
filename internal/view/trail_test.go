package view

import (
	"strings"
	"testing"

	"weave/internal/ui/paint"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sizedColumns is what a list drawing a size at the right edge keeps: the repo column and
// nothing after it.
func sizedColumns() Columns {
	return Columns{Review: true, Forge: true}
}

// TestPrepareKeepsTheTrailOnlyWhereTheListHasRoom covers the trail being given up list-wide,
// on the pane alone, rather than row by row.
func TestPrepareKeepsTheTrailOnlyWhereTheListHasRoom(t *testing.T) {
	tests := []struct {
		name string
		pane int
		want string
	}{
		{
			name: "the default sidebar keeps its sizes",
			pane: 30,
			want: "49M",
		},
		{
			name: "the narrowest pane that keeps them",
			pane: 25,
			want: "49M",
		},
		{
			name: "one column narrower gives every trail up",
			pane: 24,
			want: "",
		},
		{
			name: "no pane at all leaves the trail alone",
			want: "49M",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{
				Rows: []Row{
					{Kind: KindWorkflow, Name: "~/weave", Trail: "49M"},
					{Kind: KindAgent, Name: "a", Trail: "49M"},
				},
				Pane:    tt.pane,
				Dropped: sizedColumns(),
			})
			require.Len(t, list.Lines, 2)

			assert.Equal(t, tt.want, list.Lines[0].Trail)
			assert.Equal(t, tt.want, list.Lines[1].Trail)
		})
	}
}

// TestPrepareClipsTheNameAroundTheTrail covers the trail and its space coming out of the name's
// room rather than pushing the row past the pane.
func TestPrepareClipsTheNameAroundTheTrail(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:    []Row{{Kind: KindAgent, Name: "issue-607-panel-key-binding", Trail: "49M"}},
		Pane:    30,
		Dropped: sizedColumns(),
	})
	require.Len(t, list.Lines, 1)

	// Eight columns of gutter, two of indent, four for the trail and its space, leaving sixteen.
	assert.Equal(t, "issue-607-panel…", list.Lines[0].Name)
	assert.Equal(t, "49M", list.Lines[0].Trail)
}

// TestRenderTrailSitsAtTheRightEdge covers the trail being padded out to the pane on every
// row, whatever the name before it took.
func TestRenderTrailSitsAtTheRightEdge(t *testing.T) {
	list := Prepare(Snapshot{
		Rows: []Row{
			{Kind: KindWorkflow, Name: "~/weave", Trail: "24G"},
			{Kind: KindAgent, Name: "worktree-view", Trail: "49M"},
			{Kind: KindAgent, Name: "issue-607-panel-key-binding", Trail: "9.2M"},
			{Kind: KindChild, Name: "review-607"},
			{Kind: KindAgent, Name: "gone", Repo: RepoGone, Process: ProcessStopped, Trail: "—"},
		},
		Pane:    30,
		Dropped: sizedColumns(),
	})

	drawn := Render(list, paint.Panel, 30)
	requireGolden(t, "list-trailed", drawn)

	for i, row := range strings.Split(drawn, "\n") {
		if list.Lines[i].Trail == "" {
			continue
		}

		assert.Equalf(t, 30, lipgloss.Width(row), "row %d fills the pane exactly", i)
		assert.Truef(t, strings.HasSuffix(ansiEscapes.ReplaceAllString(row, ""), list.Lines[i].Trail), "row %d ends in its trail", i)
	}
}

// TestRenderRowWithoutATrailIsUnchanged pins a row with nothing at its right edge drawing as it
// always has, with no padding added.
func TestRenderRowWithoutATrailIsUnchanged(t *testing.T) {
	list := Prepare(Snapshot{Rows: []Row{{Kind: KindAgent, Name: "iss-582"}}, Pane: canonicalPane})

	assert.Equal(t, Render(list, paint.Panel, 0), Render(list, paint.Panel, 40))
}
