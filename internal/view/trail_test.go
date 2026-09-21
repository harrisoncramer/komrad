package view

import (
	"strings"
	"testing"

	"weave/internal/ui/paint"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trailedColumns is what a list drawing a trail at the right edge keeps: the repo column and
// nothing after it.
func trailedColumns() Columns {
	return Columns{Review: true, Forge: true}
}

// TestTrailLaysItsCellsOutInColumns covers the amount and the age each right-aligned in its own
// cell, either one blank, and nothing at all when both are.
func TestTrailLaysItsCellsOutInColumns(t *testing.T) {
	tests := []struct {
		name   string
		amount string
		age    string
		want   string
	}{
		{
			name: "both blank is no trail",
			want: "",
		},
		{
			name:   "an amount alone sits over the amount cell",
			amount: "24G",
			want:   " 24G    ",
		},
		{
			name: "an age alone sits in the age cell",
			age:  "4d",
			want: "      4d",
		},
		{
			name:   "both fill their cells",
			amount: "9.2M",
			age:    "12h",
			want:   "9.2M 12h",
		},
		{
			name:   "a dash costs one column like any other",
			amount: "—",
			age:    "2w",
			want:   "   —  2w",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Trail(tt.amount, tt.age)

			assert.Equal(t, tt.want, got)
			if got != "" {
				assert.Equal(t, trailBudget-spaceColumns, lipgloss.Width(got), "a trail fills its budget exactly")
			}
		})
	}
}

// TestPrepareKeepsTheTrailOnlyWhereTheListHasRoom covers the whole trail being given up list-wide,
// on the pane alone.
func TestPrepareKeepsTheTrailOnlyWhereTheListHasRoom(t *testing.T) {
	tests := []struct {
		name string
		pane int
		want string
	}{
		{
			name: "the default sidebar keeps its trails",
			pane: 30,
			want: " 49M  4d",
		},
		{
			name: "the narrowest pane that keeps them",
			pane: 29,
			want: " 49M  4d",
		},
		{
			name: "one column narrower gives every trail up whole",
			pane: 28,
			want: "",
		},
		{
			name: "no pane at all leaves the trail alone",
			want: " 49M  4d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{
				Rows: []Row{
					{
						Kind:  KindWorkflow,
						Name:  "~/weave",
						Trail: " 49M  4d",
					},
					{
						Kind:  KindAgent,
						Name:  "a",
						Trail: " 49M  4d",
					},
				},
				Pane:    tt.pane,
				Dropped: trailedColumns(),
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
		Rows: []Row{{
			Kind:  KindAgent,
			Name:  "issue-607-panel-key-binding",
			Trail: " 49M  4d",
		}},
		Pane:    30,
		Dropped: trailedColumns(),
	})
	require.Len(t, list.Lines, 1)

	// Eight columns of gutter, two of indent, nine for the trail and its space, leaving eleven.
	assert.Equal(t, "issue-607-…", list.Lines[0].Name)
	assert.Equal(t, " 49M  4d", list.Lines[0].Trail)
}

// TestRenderTrailSitsAtTheRightEdge covers the trail being padded out to the pane on every
// row, whatever the name before it took.
func TestRenderTrailSitsAtTheRightEdge(t *testing.T) {
	list := Prepare(Snapshot{
		Rows: []Row{
			{
				Kind:  KindWorkflow,
				Name:  "~/weave",
				Trail: " 24G    ",
			},
			{
				Kind:  KindAgent,
				Name:  "worktree-view",
				Trail: " 49M  4d",
			},
			{
				Kind:  KindAgent,
				Name:  "issue-607-panel-key-binding",
				Trail: "9.2M 12h",
			},
			{Kind: KindChild, Name: "review-607"},
			{
				Kind:    KindAgent,
				Name:    "gone",
				Repo:    RepoGone,
				Process: ProcessStopped,
				Trail:   "   —  2w",
			},
		},
		Pane:    30,
		Dropped: trailedColumns(),
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
