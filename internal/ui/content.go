package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"weave/internal/ui/layout"
	"weave/internal/ui/paint"
)

var stageTextPath = stageFilePath("text")

// homeDir is where content that reads a file rather than a directory is rooted.
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
			if !writeStageFile(stageTextPath, paint.Render(markdown, target.Width)) {
				return nil, false
			}

			return pagerArgv(stageTextPath, 0), true
		},
	}
}

// newLogContent keys on the command rather than the file it reads, since it is one log every
// agent shares.
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

// stageFilePath names a screenful for the panel that wrote it, in the one temporary directory
// every weave on the machine shares.
func stageFilePath(name string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("weave-%s-%d.ansi", name, os.Getpid()))
}

// writeStageFile renames the frame into place, so a drawer reading while the panel writes gets
// the frame before or the frame after rather than half of each.
func writeStageFile(path, content string) bool {
	temp := path + ".tmp"
	if os.WriteFile(temp, []byte(content), 0o644) != nil {
		return false
	}

	return os.Rename(temp, path) == nil
}

func pagerArgv(path string, line int) []string {
	argv := []string{"less", "-R", "-X", "-Ps"}
	if line > 1 {
		argv = append(argv, fmt.Sprintf("+%d", line))
	}

	return append(argv, path)
}
