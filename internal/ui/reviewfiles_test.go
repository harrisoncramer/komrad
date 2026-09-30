package ui_test

import (
	"fmt"
	"strings"
	"testing"

	"komrad/internal/gittest"
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
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

	t.Run("the header counts the files read and the threads waiting", func(t *testing.T) {
		server := newReviewServer(t)
		server.repo.Patches[gittest.NamePair(gittest.BaseSHA, gittest.SnapshotSHA)] = gittest.PatchOfBothFiles
		server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newHumanMessage("m1", "rename the package"))}
		harness := openReview(t, server)

		harness.press("v")

		assert.Equal(t, "FILES 1 of 2 read +4 −1 ○1", readFilesBlock(harness.readFrame())[0])
	})
}

func readFilesBlock(frame string) []string {
	var block []string
	for _, line := range readSidebar(frame) {
		if strings.HasPrefix(line, "FILES") {
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
