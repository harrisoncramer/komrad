package ui

import (
	"fmt"
	"hash/maphash"
	"os"
	"path/filepath"

	"weave/internal/ui/layout"
)

var transcriptSeed = maphash.MakeSeed()

var stageTextPath = filepath.Join(os.TempDir(), "weave-stage.ansi")

const transcriptPreviewBefore = 12

func newTextContent(markdown string) layout.Content {
	return layout.Content{
		Key:   "text:" + markdown,
		Wraps: true,
		Render: func(target layout.Target) ([]string, bool) {
			if !writeStageFile(stageTextPath, render(markdown, target.Width)) {
				return nil, false
			}

			return pagerArgv(stageTextPath, 0), true
		},
	}
}

func newLogContent(path string, argv []string) layout.Content {
	return layout.Content{
		Key: "log:" + path,
		Render: func(layout.Target) ([]string, bool) {
			return argv, len(argv) > 0
		},
	}
}

func newTranscriptContent(conversation string, highlightLine int) layout.Content {
	return layout.Content{
		// The stage is drawn again on every refresh, so the key has to answer for the
		// conversation as well as the line, or a transcript that has grown would stop restaging.
		Key: fmt.Sprintf("transcript:%d:%d", highlightLine, maphash.String(transcriptSeed, conversation)),
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

func newBlankContent() layout.Content {
	return layout.Content{
		Key:    "blank",
		Render: func(layout.Target) ([]string, bool) { return nil, true },
	}
}
