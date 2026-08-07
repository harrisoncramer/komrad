package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrLogged marks a failure that has already been written to the log in full, so main (which
// prints what Execute hands back) does not log it a second time as a bare sentence; the
// command still fails, since the exit status is how whoever spawned it knows.
var ErrLogged = errors.New("logged")

// openServeLog opens the log the spawned server writes to, creating the directory it lives
// in; the server outlives the process that started it, so it cannot write to that process's
// output and this file is where everything it has to say goes.
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

// serveLogLines is how much of the tail is worth carrying: a failed dial spends several
// lines on the addresses it tried, with the line naming what failed sitting above them.
const serveLogLines = 12

// serveLogTail reads what the server logged from offset on, as a single line, since the
// error it builds is rendered wrapped and the log's own newlines would only add breaks the
// screen has to fold again.
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
