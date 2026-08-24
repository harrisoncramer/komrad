package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrLogged = errors.New("logged")

func openServeLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create the directory for the server log %s: %w", path, err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open the server log %s: %w", path, err)
	}

	return file, nil
}

const serveLogLines = 12

func serveLogTail(path string, offset int64) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return ""
	}

	written, err := io.ReadAll(file)
	if err != nil {
		return ""
	}

	trimmed := strings.TrimSpace(string(written))
	if trimmed == "" {
		return ""
	}

	lines := strings.Split(trimmed, "\n")
	if len(lines) > serveLogLines {
		lines = lines[len(lines)-serveLogLines:]
	}

	return strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
}
