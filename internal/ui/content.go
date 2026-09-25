package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"komrad/internal/ui/layout"
	"komrad/internal/ui/paint"
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
			if err := writeStageFile(stageTextPath, paint.Panel.Render(markdown, target.Width)); err != nil {
				return nil, false
			}

			return pagerArgv(stageTextPath, 0), true
		},
	}
}

// newLogContent keys on the command rather than the file it reads.
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
// every komrad on the machine shares.
func stageFilePath(name string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("komrad-%s-%d.ansi", name, os.Getpid()))
}

// writeStageFile writes the frame beside the path and renames it into place.
func writeStageFile(path, content string) error {
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create a temporary file beside %s: %w", path, err)
	}

	if err := fillStageFile(temp, content); err != nil {
		return errors.Join(err, os.Remove(temp.Name()))
	}

	if err := os.Rename(temp.Name(), path); err != nil {
		return errors.Join(fmt.Errorf("failed to move %s into place: %w", temp.Name(), err), os.Remove(temp.Name()))
	}

	return nil
}

func fillStageFile(temp *os.File, content string) error {
	if _, err := temp.WriteString(content); err != nil {
		_ = temp.Close()

		return fmt.Errorf("failed to write %s: %w", temp.Name(), err)
	}

	if err := temp.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", temp.Name(), err)
	}

	return nil
}

func pagerArgv(path string, line int) []string {
	argv := []string{"less", "-R", "-X", "-Ps"}
	if line > 1 {
		argv = append(argv, fmt.Sprintf("+%d", line))
	}

	return append(argv, path)
}
