package ui_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"komrad/internal/gittest"
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewFiles covers the file tree rendered above the thread, which follows the diff cursor.
func TestReviewFiles(t *testing.T) {
	t.Run("the row the cursor is in is marked as it moves", func(t *testing.T) {
		adrift := newAdriftThread("gone", newHumanMessage("m1", "this line went away"))
		adrift.FilePath = gittest.File

		tests := []struct {
			name    string
			threads []*agentv1.ReviewThread
			keys    []string
			want    string
		}{
			{
				name: "a thread at the foot marks the file it was written on",
				threads: []*agentv1.ReviewThread{
					adrift,
				},
				keys: []string{"G"},
				want: "▸ greet.go +1 −1 ○",
			},
			{
				name: "a line in the first file marks that file",
				want: "▸ greet.go +1 −1",
			},
			{
				name: "the next file key moves the mark with it",
				keys: []string{"]"},
				want: "▸ second.go +3",
			},
			{
				name: "a line carrying a thread marks its file with the thread",
				threads: []*agentv1.ReviewThread{
					newThreadOnFirstLine(newHumanMessage("m3", "rename the package")),
				},
				keys: []string{"j"},
				want: "▸ greet.go +1 −1 ○",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				server := newReviewServer(t)
				server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = gittest.PatchOfBothFiles
				server.threads = tt.threads
				harness := openReview(t, server)

				harness.press(tt.keys...)

				assert.Equal(t, tt.want, readCurrentFile(harness.readFrame()))
			})
		}
	})

	t.Run("a file's counts wear the colours of the lines they count", func(t *testing.T) {
		server := newReviewServer(t)
		server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = gittest.PatchOfBothFiles
		harness := openReview(t, server)

		frame := harness.readRawFrame()
		assert.Contains(t, frame, paint.Panel.Style(paint.ToneSuccess).Render("+3"))
		assert.Contains(t, frame, paint.Panel.Style(paint.ToneDanger).Render("−1"))
	})

	t.Run("a long tree is windowed on the marked row and counts what it leaves out", func(t *testing.T) {
		server := newReviewServer(t)
		server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = newPatchOfFiles(30)
		harness := openReview(t, server)

		for range 15 {
			harness.press("]")
		}

		block := strings.Join(readFilesBlock(harness.readFrame()), "\n")
		assert.Contains(t, block, "▸ f15.go")
		assert.Regexp(t, `↑ \d+ more`, block)
		assert.Regexp(t, `↓ \d+ more`, block)
		assert.NotContains(t, block, "f00.go")
		assert.NotContains(t, block, "f29.go")
	})

	t.Run("the search over the tree lands on the files it matches", func(t *testing.T) {
		leader := []string{"ctrl+space", "ctrl+space"}
		filter := []string{"/", "f", "0", "[", "13", "]", "tab", "enter"}

		tests := []struct {
			name   string
			keys   []string
			want   []string
			cursor string
		}{
			{
				name: "a filter matching nothing says so",
				keys: slices.Concat(leader, []string{"/", "z", "tab", "enter"}),
				want: []string{"▽ z", "no file matches"},
			},
			{
				name: "a search keeps every file and lands on the first match",
				keys: slices.Concat(leader, []string{"/", "f", "0", "2", "enter"}),
				want: []string{"/ f02", "f00.go +1 −1", "f01.go +1 −1", "› f02.go +1 −1", "f03.go +1 −1"},
			},
			{
				name:   "a search matching the file the cursor is in leaves the cursor where it is",
				keys:   slices.Concat([]string{"j"}, leader, []string{"/", "f", "0", "0", "enter"}),
				want:   []string{"/ f00", "› f00.go +1 −1", "f01.go +1 −1", "f02.go +1 −1", "f03.go +1 −1"},
				cursor: "f00.go:1",
			},
			{
				name: "the next match key walks to the next file the search matches",
				keys: slices.Concat(leader, []string{"/", "f", "0", "[", "13", "]", "enter", "n"}),
				want: []string{"/ f0[13]", "f00.go +1 −1", "f01.go +1 −1", "f02.go +1 −1", "› f03.go +1 −1"},
			},
			{
				name: "tab turns the search into a filter that keeps only the files it matches",
				keys: slices.Concat(leader, filter),
				want: []string{"▽ f0[13]", "› f01.go +1 −1", "f03.go +1 −1"},
			},
			{
				name: "moving in a filtered tree skips the files it took away",
				keys: slices.Concat(leader, filter, []string{"j"}),
				want: []string{"▽ f0[13]", "f01.go +1 −1", "› f03.go +1 −1"},
			},
			{
				name: "the next match key from a file the filter took away lands on the first match",
				keys: slices.Concat(leader, filter, leader, []string{"]"}, leader, []string{"n"}),
				want: []string{"▽ f0[13]", "› f01.go +1 −1", "f03.go +1 −1"},
			},
			{
				name: "the back key clears a standing filter",
				keys: slices.Concat(leader, []string{"/", "f", "0", "1", "tab", "enter", "esc"}),
				want: []string{"/", "f00.go +1 −1", "› f01.go +1 −1", "f02.go +1 −1", "f03.go +1 −1"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				server := newReviewServer(t)
				server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = newPatchOfFiles(4)
				harness := openReview(t, server)

				harness.press(tt.keys...)

				frame := harness.readFrame()
				assert.Equal(t, tt.want, readFilesBlock(frame))
				if tt.cursor != "" {
					assert.Equal(t, tt.cursor, readReviewCursor(frame))
				}
			})
		}
	})

	t.Run("the leader twice hands the keys to the tree, which walks the diff a file at a time", func(t *testing.T) {
		server := newReviewServer(t)
		server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = gittest.PatchOfBothFiles
		harness := openReview(t, server)

		harness.press("ctrl+space", "ctrl+space", "j")
		assert.Contains(t, readFilesBlock(harness.readFrame()), "› second.go +3")

		harness.press("ctrl+space", "ctrl+space")
		assert.Equal(t, "▸ second.go +3", readCurrentFile(harness.readFrame()), "the diff holds the keys again")

		harness.press("ctrl+space", "ctrl+space", "k")
		assert.Contains(t, readFilesBlock(harness.readFrame()), "› greet.go +1 −1")
	})

	t.Run("the review opens on the diff, and the back key steps from the diff to the tree and then out", func(t *testing.T) {
		server := newReviewServer(t)
		server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = gittest.PatchOfBothFiles
		harness := newPanelHarness(t, panelHarnessParams{
			width:  reviewWidth,
			height: reviewHeight,
			git:    server.repo,
		})
		server.serve(harness)
		harness.watch(server.buildSnapshot())

		harness.press("space", "r")
		assert.Contains(t, readFilesBlock(harness.readFrame()), "▸ greet.go +1 −1", "the diff holds the keys")

		harness.press("esc", "j")
		assert.Contains(t, readFilesBlock(harness.readFrame()), "› second.go +3", "the back key hands the keys to the tree")

		harness.press("esc")
		assert.Contains(t, readHoveredRow(harness.readFrame()), "auth")
	})

	t.Run("the viewed key on the tree marks the file it is on", func(t *testing.T) {
		server := newReviewServer(t)
		server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = gittest.PatchOfBothFiles
		harness := openReview(t, server)

		harness.press("ctrl+space", "ctrl+space", "j", "space", "v")

		require.Len(t, server.viewed, 1)
		assert.Equal(t, "second.go", server.viewed[0].GetFilePath())
	})

	t.Run("the movement keys on the tree walk the diff by files", func(t *testing.T) {
		leader := []string{"ctrl+space", "ctrl+space"}

		tests := []struct {
			name string
			keys []string
			want string
		}{
			{
				name: "the bottom key lands on the last file",
				keys: slices.Concat(leader, []string{"G"}),
				want: "› f29.go +1 −1",
			},
			{
				name: "the top key twice lands on the first file",
				keys: slices.Concat(leader, []string{"G", "g", "g"}),
				want: "› f00.go +1 −1",
			},
			{
				name: "the page key walks a page of files",
				keys: slices.Concat(leader, []string{"ctrl+d"}),
				want: "› f10.go +1 −1",
			},
			{
				name: "the page up key walks back a page of files",
				keys: slices.Concat(leader, []string{"ctrl+d", "ctrl+d", "ctrl+u"}),
				want: "› f10.go +1 −1",
			},
			{
				name: "the next file key walks one file",
				keys: slices.Concat(leader, []string{"]"}),
				want: "› f01.go +1 −1",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				server := newReviewServer(t)
				server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = newPatchOfFiles(30)
				harness := openReview(t, server)

				harness.press(tt.keys...)

				assert.Contains(t, readFilesBlock(harness.readFrame()), tt.want)
			})
		}
	})

	t.Run("the top key twice on the first file brings the diff back to its top", func(t *testing.T) {
		harness := openReview(t, newReviewServer(t))
		harness.press("j", "j")
		require.Equal(t, "greet.go:3", readReviewCursor(harness.readFrame()))

		harness.press("ctrl+space", "ctrl+space", "g", "g")

		assert.Equal(t, "greet.go:1", readReviewCursor(harness.readFrame()))
	})

	t.Run("the folder keys on the tree open and close the folder the cursor rests on", func(t *testing.T) {
		leader := []string{"ctrl+space", "ctrl+space"}
		opened := glyph.Defaults().Spell(glyph.FolderOpen)
		closed := glyph.Defaults().Spell(glyph.FolderClosed)
		thread := newAdriftThread("t1", newHumanMessage("m1", "look at this"))
		thread.FilePath = "pkg/a.go"

		tests := []struct {
			name    string
			paths   []string
			threads []*agentv1.ReviewThread
			keys    []string
			want    []string
			cursor  string
		}{
			{
				name: "every folder opens with the review",
				want: []string{"/", opened + " pkg/", "▸ a.go +1 −1", "b.go +1 −1", "top.go +1 −1"},
			},
			{
				name:   "the move key stops on a folder and leaves the diff where it is",
				keys:   slices.Concat(leader, []string{"k"}),
				want:   []string{"/", "› " + opened + " pkg/", "a.go +1 −1", "b.go +1 −1", "top.go +1 −1"},
				cursor: "pkg/a.go:1 (removed)",
			},
			{
				name: "the folder key closes the folder the cursor rests on",
				keys: slices.Concat(leader, []string{"k", "o"}),
				want: []string{"/", "› " + closed + " pkg/", "top.go +1 −1"},
			},
			{
				name: "the open key opens a closed folder and the cursor stays on it",
				keys: slices.Concat(leader, []string{"k", "o", "enter"}),
				want: []string{"/", "› " + opened + " pkg/", "a.go +1 −1", "b.go +1 −1", "top.go +1 −1"},
			},
			{
				name: "the tree lets go of the folder its cursor rested on once it hands the keys back",
				keys: slices.Concat(leader, []string{"k"}, leader, leader),
				want: []string{"/", opened + " pkg/", "› a.go +1 −1", "b.go +1 −1", "top.go +1 −1"},
			},
			{
				name: "the folder key on a file leaves every folder as it is",
				keys: slices.Concat(leader, []string{"o"}),
				want: []string{"/", opened + " pkg/", "› a.go +1 −1", "b.go +1 −1", "top.go +1 −1"},
			},
			{
				name:   "moving past a closed folder skips its files",
				keys:   slices.Concat(leader, []string{"k", "o", "j"}),
				want:   []string{"/", closed + " pkg/", "› top.go +1 −1"},
				cursor: "top.go:1 (removed)",
			},
			{
				name:  "a folder holding only folders closes",
				paths: []string{"internal/ui/x.go", "internal/glyph/y.go"},
				keys:  slices.Concat(leader, []string{"k", "k", "o"}),
				want:  []string{"/", "› " + closed + " internal/"},
			},
			{
				name:    "a closed folder carries the mark of the threads it hides",
				threads: []*agentv1.ReviewThread{thread},
				keys:    slices.Concat(leader, []string{"k", "o"}),
				want:    []string{"/", "› " + closed + " pkg/ ○", "top.go +1 −1"},
			},
			{
				name:   "a search landing in a closed folder opens it",
				keys:   slices.Concat(leader, []string{"k", "o", "j", "/", "b", "enter"}),
				want:   []string{"/ b", opened + " pkg/", "a.go +1 −1", "› b.go +1 −1", "top.go +1 −1"},
				cursor: "pkg/b.go:1 (removed)",
			},
			{
				name:   "a filter whose matches sit in a closed folder opens it",
				keys:   slices.Concat(leader, []string{"k", "o", "j", "/", "b", "tab", "enter"}),
				want:   []string{"▽ b", opened + " pkg/", "› b.go +1 −1"},
				cursor: "pkg/b.go:1 (removed)",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				paths := tt.paths
				if paths == nil {
					paths = []string{"pkg/a.go", "pkg/b.go", "top.go"}
				}

				server := newReviewServer(t)
				server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = newPatchOfPaths(paths...)
				server.threads = tt.threads
				harness := openReview(t, server)

				harness.press(tt.keys...)

				frame := harness.readFrame()
				assert.Equal(t, tt.want, readFilesBlock(frame))
				if tt.cursor != "" {
					assert.Equal(t, tt.cursor, readReviewCursor(frame))
				}
			})
		}
	})

	t.Run("the wheel over the tree walks the diff a file at a time and leaves the keys with the diff", func(t *testing.T) {
		server := newReviewServer(t)
		server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = gittest.PatchOfBothFiles
		harness := openReview(t, server)

		wheelOverSidebar(harness, "▸ greet.go")

		assert.Equal(t, "▸ second.go +3", readCurrentFile(harness.readFrame()))
	})

	t.Run("the wheel over the tree walks past a folder while the diff holds the keys", func(t *testing.T) {
		server := newReviewServer(t)
		server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = newPatchOfPaths("a.go", "pkg/b.go")
		harness := openReview(t, server)

		wheelOverSidebar(harness, "▸ a.go")

		assert.Equal(t, "▸ b.go +1 −1", readCurrentFile(harness.readFrame()))
	})

	t.Run("the wheel beside the tree walks the diff while the tree holds the keys", func(t *testing.T) {
		server := newReviewServer(t)
		server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = gittest.PatchOfBothFiles
		harness := openReview(t, server)
		harness.press("ctrl+space", "ctrl+space")

		wheelOverSidebar(harness, "greet.go:1")

		frame := harness.readFrame()
		assert.NotEqual(t, "greet.go:1", readReviewCursor(frame))
		assert.Contains(t, readFilesBlock(frame), "› greet.go +1 −1")
	})
}

func readFilesBlock(frame string) []string {
	var block []string
	for _, line := range readSidebar(frame) {
		if block == nil && (strings.HasPrefix(line, "/") || strings.HasPrefix(line, "▽")) {
			block = []string{}
		}
		if block == nil {
			continue
		}
		if strings.TrimSpace(line) == "" {
			break
		}

		block = append(block, strings.Join(strings.Fields(line), " "))
	}

	return block
}

func readCurrentFile(frame string) string {
	for _, line := range readFilesBlock(frame) {
		if strings.HasPrefix(line, "▸") {
			return line
		}
	}

	return ""
}

func newPatchOfFiles(count int) string {
	paths := make([]string, 0, count)
	for i := range count {
		paths = append(paths, fmt.Sprintf("f%02d.go", i))
	}

	return newPatchOfPaths(paths...)
}

func newPatchOfPaths(paths ...string) string {
	var patch strings.Builder
	for _, path := range paths {
		fmt.Fprintf(&patch, "diff --git a/%[1]s b/%[1]s\n--- a/%[1]s\n+++ b/%[1]s\n@@ -1 +1 @@\n-old\n+new\n", path)
	}

	return patch.String()
}
