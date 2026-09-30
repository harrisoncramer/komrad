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

func addedLine(number int, text string) diff.Line {
	return diff.Line{
		Kind:      diff.Addition,
		Text:      text,
		NewNumber: number,
	}
}

func describeTree(rows []TreeRow) []string {
	kinds := map[TreeRowKind]string{
		TreeFolder: "folder",
		TreeFile:   "file",
		TreeThread: "thread",
	}

	described := make([]string, 0, len(rows))
	for _, row := range rows {
		said := fmt.Sprintf("%s%s %s row=%d", strings.Repeat("  ", row.Depth), kinds[row.Kind], row.Name, row.Row)
		if row.Kind == TreeThread {
			said += fmt.Sprintf(" line=%d", row.Line)
		}
		if row.Viewed {
			said += " viewed"
		}

		described = append(described, said)
	}

	return described
}

// TestFileTree covers the files laid out as folders in the order the diff draws them.
func TestFileTree(t *testing.T) {
	tests := []struct {
		name    string
		files   []diff.File
		viewed  map[string]bool
		threads []*agentv1.ReviewThread
		want    []string
	}{
		{
			name: "files out of path order open a folder twice rather than being moved",
			files: []diff.File{
				fileAt("a/one.go", addedLine(1, "one")),
				fileAt("b.go", addedLine(1, "b")),
				fileAt("a/two.go", addedLine(1, "two")),
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
				fileAt("internal/ui/diffview/view.go", addedLine(1, "view")),
				fileAt("internal/ui/review.go", addedLine(1, "review")),
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
				fileAt("docs/a/one.md", addedLine(1, "one")),
				fileAt("docs/b/two.md", addedLine(1, "two")),
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
				fileAt("a.go", addedLine(1, "a")),
				fileAt("b.go", addedLine(1, "b")),
			},
			viewed: map[string]bool{"a.go": true},
			want: []string{
				"file a.go row=1 viewed",
				"file b.go row=2",
			},
		},
		{
			name: "threads sit under their file in the order the diff draws them",
			files: []diff.File{
				fileAt("a.go", addedLine(1, "first"), addedLine(2, "second")),
			},
			threads: []*agentv1.ReviewThread{
				threadAt("later", "a.go", 0, 2, "+second"),
				threadAt("earlier", "a.go", 0, 1, "+first"),
			},
			want: []string{
				"file a.go row=1",
				"  thread  row=3 line=1",
				"  thread  row=4 line=2",
			},
		},
		{
			name: "a thread with nothing left to stand on is listed with no line",
			files: []diff.File{
				fileAt("a.go", addedLine(1, "kept")),
			},
			threads: []*agentv1.ReviewThread{
				threadAt("gone", "a.go", 0, 9, "+removed"),
			},
			want: []string{
				"file a.go row=1",
				"  thread  row=5 line=0",
			},
		},
		{
			name: "a thread in a folded file has no row and keeps its line",
			files: []diff.File{
				fileAt("a.go", addedLine(1, "first")),
			},
			viewed: map[string]bool{"a.go": true},
			threads: []*agentv1.ReviewThread{
				threadAt("folded", "a.go", 0, 1, "+first"),
			},
			want: []string{
				"file a.go row=1 viewed",
				"  thread  row=-1 line=1",
			},
		},
		{
			name: "a comment on the change as a whole is under no file",
			files: []diff.File{
				fileAt("a.go", addedLine(1, "first")),
			},
			threads: []*agentv1.ReviewThread{
				threadAt("whole", "", 0, 0, ""),
			},
			want: []string{
				"file a.go row=1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := NewView(patchWith(tt.files...), false)
			view.SetViewed(tt.viewed)
			view.PinThreads(tt.threads)

			assert.Equal(t, tt.want, describeTree(view.FileTree()))
		})
	}
}

// TestCountThreads covers each thread counted once, under the first state that asks something of the reader.
func TestCountThreads(t *testing.T) {
	agentThread := func(id string) *agentv1.ReviewThread {
		thread := threadAt(id, "a.go", 0, 1, "+first")
		thread.Messages = []*agentv1.ReviewMessage{{AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT}}

		return thread
	}

	unsent := threadAt("unsent", "a.go", 0, 1, "+first")
	unsent.Messages = []*agentv1.ReviewMessage{{Delivery: agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT}}
	unsent.UnreadByHuman = true

	unreadAgent := agentThread("unread-agent")
	unreadAgent.UnreadByHuman = true

	resolvedUnread := threadAt("resolved-unread", "a.go", 0, 1, "+first")
	resolvedUnread.ResolvedAt = "2026-09-30T00:00:00Z"
	resolvedUnread.UnreadByHuman = true

	resolved := threadAt("resolved", "a.go", 0, 1, "+first")
	resolved.ResolvedAt = "2026-09-30T00:00:00Z"

	view := NewView(patchWith(fileAt("a.go", addedLine(1, "first"))), false)
	view.PinThreads([]*agentv1.ReviewThread{
		unsent,
		unreadAgent,
		resolvedUnread,
		resolved,
		agentThread("agent"),
		threadAt("open", "a.go", 0, 1, "+first"),
	})

	want := ThreadCounts{
		Unsent: 1,
		Unread: 2,
		Open:   1,
	}
	assert.Equal(t, want, view.CountThreads())
	assert.Equal(t, want, view.FileTree()[0].Counts)
}

// TestGetTreeFocus covers the file and thread a diff row belongs to.
func TestGetTreeFocus(t *testing.T) {
	view := NewView(patchWith(
		fileAt("a.go", addedLine(1, "first")),
		fileAt("b.go", addedLine(1, "second")),
	), false)
	view.PinThreads([]*agentv1.ReviewThread{
		threadAt("on-line", "b.go", 0, 1, "+second"),
		threadAt("adrift", "a.go", 0, 9, "+removed"),
	})

	tests := []struct {
		name       string
		row        int
		wantPath   string
		wantThread string
	}{
		{
			name: "the row for the change as a whole belongs to no file",
			row:  0,
		},
		{
			name:     "a file header belongs to its file",
			row:      1,
			wantPath: "a.go",
		},
		{
			name:       "a line carrying a thread names the thread",
			row:        6,
			wantPath:   "b.go",
			wantThread: "on-line",
		},
		{
			name:       "a thread at the foot belongs to the file it was written on",
			row:        8,
			wantPath:   "a.go",
			wantThread: "adrift",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, thread := view.GetTreeFocus(tt.row)
			assert.Equal(t, tt.wantPath, path)
			assert.Equal(t, tt.wantThread, thread)
		})
	}
}
