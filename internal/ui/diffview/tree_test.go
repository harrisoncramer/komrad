package diffview

import (
	"fmt"
	"strings"
	"testing"

	"komrad/internal/diff"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
)

func fileAt(path string, lines ...diff.Line) diff.File {
	return diff.File{
		OldPath: path,
		NewPath: path,
		Hunks:   []diff.Hunk{hunkOf(lines...)},
	}
}

func addedLine(text string) diff.Line {
	return diff.Line{
		Kind:      diff.Addition,
		Text:      text,
		NewNumber: 1,
	}
}

func describeTree(rows []TreeRow) []string {
	described := make([]string, 0, len(rows))
	for _, row := range rows {
		kind := "file"
		if row.Folder {
			kind = "folder"
		}

		said := fmt.Sprintf("%s%s %s row=%d", strings.Repeat("  ", row.Depth), kind, row.Name, row.Row)
		if row.Viewed {
			said += " viewed"
		}
		if row.Counts != (ThreadCounts{}) {
			said += fmt.Sprintf(" %+v", row.Counts)
		}

		described = append(described, said)
	}

	return described
}

type fileTreeCase struct {
	name    string
	files   []diff.File
	viewed  map[string]bool
	threads []*agentv1.ReviewThread
	want    []string
}

// TestFileTree covers folders opened and merged in diff order, and what each file row carries.
func TestFileTree(t *testing.T) {
	tests := []fileTreeCase{
		{
			name: "a comment on the change as a whole is counted on no file",
			files: []diff.File{
				fileAt("a.go", addedLine("first")),
			},
			threads: []*agentv1.ReviewThread{
				threadAt("whole", "", 0, 0, ""),
			},
			want: []string{
				"file a.go row=1",
			},
		},
		{
			name: "files out of path order open a folder twice rather than being moved",
			files: []diff.File{
				fileAt("a/one.go", addedLine("one")),
				fileAt("b.go", addedLine("b")),
				fileAt("a/two.go", addedLine("two")),
			},
			want: []string{
				"folder a/ row=-1",
				"  file one.go row=1",
				"file b.go row=4",
				"folder a/ row=-1",
				"  file two.go row=7",
			},
		},
		{
			name: "a folder holding one folder and nothing else is merged with it",
			files: []diff.File{
				fileAt("internal/ui/diffview/view.go", addedLine("view")),
				fileAt("internal/ui/review.go", addedLine("review")),
			},
			want: []string{
				"folder internal/ui/ row=-1",
				"  folder diffview/ row=-1",
				"    file view.go row=1",
				"  file review.go row=4",
			},
		},
		{
			name: "a folder holding two folders keeps its own row",
			files: []diff.File{
				fileAt("docs/a/one.md", addedLine("one")),
				fileAt("docs/b/two.md", addedLine("two")),
			},
			want: []string{
				"folder docs/ row=-1",
				"  folder a/ row=-1",
				"    file one.md row=1",
				"  folder b/ row=-1",
				"    file two.md row=4",
			},
		},
		{
			name: "a read file is marked and left where it is",
			files: []diff.File{
				fileAt("a.go", addedLine("a")),
				fileAt("b.go", addedLine("b")),
			},
			viewed: map[string]bool{"a.go": true},
			want: []string{
				"file a.go row=1 viewed",
				"file b.go row=2",
			},
		},
		{
			name: "a file counts its threads, those with nothing left to stand on included",
			files: []diff.File{
				fileAt("a.go", addedLine("first")),
				fileAt("b.go", addedLine("second")),
			},
			threads: []*agentv1.ReviewThread{
				threadAt("on-line", "a.go", 0, 1, "+first"),
				threadAt("gone", "a.go", 0, 9, "+removed"),
			},
			want: []string{
				"file a.go row=1 {Unsent:0 Unread:0 Open:2 Settled:0}",
				"file b.go row=4",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := NewView(patchWith(tt.files...), false)
			view.SetViewed(tt.viewed)
			view.PinThreads(tt.threads)

			assert.Equal(t, tt.want, describeTree(view.GetFileTree()))
		})
	}
}

// TestCountThreads covers each thread counted once, under the first state it stands in.
func TestCountThreads(t *testing.T) {
	unsent := threadAt("unsent", "a.go", 0, 1, "+first")
	unsent.Messages = []*agentv1.ReviewMessage{{Delivery: agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT}}
	unsent.UnreadByHuman = true

	unread := threadAt("unread", "a.go", 0, 1, "+first")
	unread.UnreadByHuman = true

	resolvedUnread := threadAt("resolved-unread", "a.go", 0, 1, "+first")
	resolvedUnread.ResolvedAt = "2026-09-30T00:00:00Z"
	resolvedUnread.UnreadByHuman = true

	resolved := threadAt("resolved", "a.go", 0, 1, "+first")
	resolved.ResolvedAt = "2026-09-30T00:00:00Z"

	agent := threadAt("agent", "a.go", 0, 1, "+first")
	agent.Messages = []*agentv1.ReviewMessage{{AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT}}

	view := NewView(patchWith(fileAt("a.go", addedLine("first"))), false)
	view.PinThreads([]*agentv1.ReviewThread{
		unsent,
		unread,
		resolvedUnread,
		resolved,
		agent,
		threadAt("open", "a.go", 0, 1, "+first"),
	})

	want := ThreadCounts{
		Unsent:  1,
		Unread:  2,
		Open:    2,
		Settled: 1,
	}
	assert.Equal(t, want, view.CountThreads())
	assert.Equal(t, want, view.GetFileTree()[0].Counts)
}

type treeFocusCase struct {
	name string
	row  int
	want string
}

// TestGetTreeFocus covers which file a diff row, a thread row at the foot included, belongs to.
func TestGetTreeFocus(t *testing.T) {
	view := NewView(patchWith(
		fileAt("a.go", addedLine("first")),
		fileAt("b.go", addedLine("second")),
	), false)
	view.PinThreads([]*agentv1.ReviewThread{
		threadAt("on-line", "b.go", 0, 1, "+second"),
		threadAt("adrift", "a.go", 0, 9, "+removed"),
	})

	tests := []treeFocusCase{
		{
			name: "the row for the change as a whole belongs to no file",
			row:  0,
		},
		{
			name: "a file header belongs to its file",
			row:  1,
			want: "a.go",
		},
		{
			name: "a line belongs to its file",
			row:  6,
			want: "b.go",
		},
		{
			name: "a thread at the foot belongs to the file it was written on",
			row:  8,
			want: "a.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, view.GetTreeFocus(tt.row))
		})
	}
}
