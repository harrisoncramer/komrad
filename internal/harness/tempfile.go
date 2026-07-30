package harness

import (
	"fmt"
	"os"
)

// WriteTempText writes text to a temp file matching pattern and returns its path. It
// is how a harness gets multi-line markdown past send-keys: the command it builds
// sources the file in place, so nothing in the text has to be escaped for a shell.
//
// The file is removed by the command that reads it rather than here, since nothing on
// this side of send-keys knows when the shell has run it.
func WriteTempText(pattern, text string) (string, error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("failed to create %q: %w", pattern, err)
	}

	if _, err := file.WriteString(text); err != nil {
		_ = file.Close()

		return "", fmt.Errorf("failed to write %q: %w", file.Name(), err)
	}

	if err := file.Close(); err != nil {
		return "", fmt.Errorf("failed to close %q: %w", file.Name(), err)
	}

	return file.Name(), nil
}
