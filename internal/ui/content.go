package ui

import (
	"fmt"
	"hash/maphash"
	"os"
	"strings"

	"weave/internal/ui/layout"
)

var transcriptSeed = maphash.MakeSeed()

var stageTextPath = stageFilePath("text")

var stageTranscriptPath = stageFilePath("transcript")

const transcriptPreviewBefore = 12

// homeDir is where content that reads a file rather than a directory is rooted, since a pager
// takes its path from the argv and cares nothing for where it was started.
func homeDir() string {
	dir, _ := os.UserHomeDir()

	return dir
}

func newTextContent(markdown string) layout.Content {
	return layout.Content{
		Key:   "text:" + markdown,
		Dir:   homeDir(),
		Wraps: true,
		Render: func(target layout.Target) ([]string, bool) {
			if !writeStageFile(stageTextPath, render(markdown, target.Width)) {
				return nil, false
			}

			return pagerArgv(stageTextPath, 0), true
		},
	}
}

// newLogContent keys on the command rather than the file it reads, since the panel's own log is one
// file every agent shares and only the viewer's environment says which agent it is filtered to.
func newLogContent(argv []string) layout.Content {
	return layout.Content{
		Key: "log:" + strings.Join(argv, "\x00"),
		Dir: homeDir(),
		Render: func(layout.Target) ([]string, bool) {
			return argv, len(argv) > 0
		},
	}
}

func newTranscriptContent(conversation string, spans []span) layout.Content {
	return layout.Content{
		// The stage is drawn again on every refresh, so the key has to answer for the
		// conversation as well as the matched spans, or a transcript that has grown would stop
		// restaging.
		Key:   fmt.Sprintf("transcript:%v:%d", spans, maphash.String(transcriptSeed, conversation)),
		Dir:   homeDir(),
		Wraps: true,
		Render: func(target layout.Target) ([]string, bool) {
			text, line := renderTranscript(conversation, spans, target.Width)
			if !writeStageFile(stageTranscriptPath, text) {
				return nil, false
			}

			open := 0
			if line > 0 {
				open = max(line-transcriptPreviewBefore, 1)
			}

			return pagerArgv(stageTranscriptPath, open), true
		},
	}
}

func newBlankContent() layout.Content {
	return layout.Content{
		Key:    "blank",
		Dir:    homeDir(),
		Render: func(layout.Target) ([]string, bool) { return nil, true },
	}
}
