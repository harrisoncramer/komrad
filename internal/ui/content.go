package ui

import (
	"os"
	"strings"

	"weave/internal/ui/layout"
)

var stageTextPath = stageFilePath("text")

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

func newBlankContent() layout.Content {
	return layout.Content{
		Key:    "blank",
		Dir:    homeDir(),
		Render: func(layout.Target) ([]string, bool) { return nil, true },
	}
}
