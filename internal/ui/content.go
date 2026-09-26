package ui

import (
	"os"

	"komrad/internal/ui/layout"
)

// homeDir is where content that reads a file rather than a directory is rooted.
func homeDir() string {
	dir, _ := os.UserHomeDir()

	return dir
}

func newBlankContent() layout.Content {
	return layout.Content{
		Key:    "blank",
		Dir:    homeDir(),
		Render: func(layout.Target) ([]string, bool) { return nil, true },
	}
}
