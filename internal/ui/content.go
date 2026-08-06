package ui

import (
	"os"
	"path/filepath"

	"weave/internal/ui/layout"
)

// What the panel puts on its frame, each as a piece of layout content with its own identity
// and the command that fills a pane with it. render.go draws the text and the layout package
// is where it lands.

// transcriptPreviewBefore is how many lines of the conversation a preview opens above the
// matched line, so a hit is read in context rather than at the top of the pane.
const transcriptPreviewBefore = 12

// newTextContent is markdown, wrapped to the pane it lands in and paged there.
func newTextContent(markdown string) layout.Content {
	return layout.Content{
		Key:   "text:" + markdown,
		Wraps: true,
		Render: func(target layout.Target) ([]string, bool) {
			path := getTextPath(target.Region)
			if !writeStageFile(path, render(markdown, target.Width)) {
				return nil, false
			}

			return pagerArgv(path, 0), true
		},
	}
}

// newFlashContent is text with field's line (1-based) reversed for a moment, the brief flash
// that confirms which field a digit key just copied. It carries no key, so it is always drawn
// and the plain content that ends the flash is not skipped as a no-op.
func newFlashContent(markdown string, field int) layout.Content {
	return layout.Content{
		Wraps: true,
		Render: func(target layout.Target) ([]string, bool) {
			path := getTextPath(target.Region)
			if !writeStageFile(path, renderFlash(markdown, target.Width, field)) {
				return nil, false
			}

			return pagerArgv(path, 0), true
		},
	}
}

// newLogContent is a log read by whatever viewer the user has configured, which lays itself out
// and so is handed the pane as it is.
func newLogContent(path string, argv []string) layout.Content {
	return layout.Content{
		Key: "log:" + path,
		Render: func(layout.Target) ([]string, bool) {
			return argv, len(argv) > 0
		},
	}
}

// newTranscriptContent is a conversation with the matched line marked, opened near it. It
// carries no key, since a preview is always about a hit that was just hovered.
func newTranscriptContent(conversation string, highlightLine int) layout.Content {
	return layout.Content{
		Render: func(layout.Target) ([]string, bool) {
			path := filepath.Join(os.TempDir(), "weave-transcript.txt")
			if !writeStageFile(path, highlighted(conversation, highlightLine)) {
				return nil, false
			}

			open := 0
			if highlightLine > 0 {
				open = max(highlightLine-transcriptPreviewBefore, 1)
			}

			return pagerArgv(path, open), true
		},
	}
}

// newBlankContent empties a region, which runs the pane's own shell and nothing else.
func newBlankContent() layout.Content {
	return layout.Content{
		Key:    "blank",
		Render: func(layout.Target) ([]string, bool) { return nil, true },
	}
}

// getTextPath is where a region's rendered text is written for its pager to read. Each region
// writes its own file, so a document being read in one is not overwritten by the other.
func getTextPath(region layout.Region) string {
	name := "weave-stage.ansi"
	if region == layout.RegionBottom {
		name = "weave-secondary.ansi"
	}

	return filepath.Join(os.TempDir(), name)
}
