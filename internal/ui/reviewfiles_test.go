package ui_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"komrad/internal/gittest"
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewFiles covers the file tree drawn above the thread, which follows the diff cursor.
func TestReviewFiles(t *testing.T) {
	t.Run("the row the cursor is in is marked as it moves", func(t *testing.T) {
		adrift := newAdriftThread("gone", newHumanMessage("m1", "this line went away"))
		adrift.FilePath = gittest.File
		whole := &agentv1.ReviewThread{
			Id:       "whole",
			Messages: []*agentv1.ReviewMessage{newHumanMessage("m2", "split this in two")},
		}

		tests := []struct {
			name    string
			threads []*agentv1.ReviewThread
			keys    []string
			want    string
		}{
			{
				name: "the row for the change as a whole marks nothing",
				keys: []string{"k"},
			},
			{
				name: "a thread at the foot marks the file it was written on",
				threads: []*agentv1.ReviewThread{
					whole,
					adrift,
				},
				keys: []string{"G"},
				want: "▸ greet.go +1 −1 ○",
			},
			{
				name: "a comment on the change as a whole marks nothing",
				threads: []*agentv1.ReviewThread{
					whole,
					adrift,
				},
				keys: []string{"G", "k"},
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

	t.Run("the review opens on the tree, and the back key steps from the diff to the tree and then out", func(t *testing.T) {
		server := newReviewServer(t)
		server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = gittest.PatchOfBothFiles
		harness := newPanelHarness(t, panelHarnessParams{width: reviewWidth, height: reviewHeight, git: server.repo})
		server.serve(harness)
		harness.watch(server.buildSnapshot())

		harness.press("space", "r", "j")
		assert.Contains(t, readFilesBlock(harness.readFrame()), "› second.go +3", "the tree holds the keys")

		harness.press("ctrl+space", "ctrl+space", "esc", "k")
		assert.Contains(t, readFilesBlock(harness.readFrame()), "› greet.go +1 −1", "the back key hands the keys to the tree")

		harness.press("esc")
		assert.Contains(t, readHoveredRow(harness.readFrame()), "auth")
	})

	t.Run("the viewed key on the tree marks the file it is on", func(t *testing.T) {
		server := newReviewServer(t)
		server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = gittest.PatchOfBothFiles
		harness := openReview(t, server)

		harness.press("ctrl+space", "ctrl+space", "j", "v")

		require.Len(t, server.viewed, 1)
		assert.Equal(t, "second.go", server.viewed[0].GetFilePath())
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
	var patch strings.Builder
	for i := range count {
		path := fmt.Sprintf("f%02d.go", i)
		fmt.Fprintf(&patch, "diff --git a/%[1]s b/%[1]s\n--- a/%[1]s\n+++ b/%[1]s\n@@ -1 +1 @@\n-old\n+new\n", path)
	}

	return patch.String()
}
