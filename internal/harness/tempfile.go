package harness

import (
	"fmt"
	"os"
)

// WriteTempText writes text to a file matching pattern under dir and returns its path. It
// is how a harness gets multi-line markdown past send-keys, since the command it builds
// sources the file in place and nothing in the text has to be escaped for a shell.
//
// The file is removed by the command that reads it rather than here, since nothing on this
// side of send-keys knows when the shell has run it.
func WriteTempText(dir, pattern, text string) (string, error) {
	// MkdirAll refuses an empty dir where CreateTemp takes it as the system temp directory,
	// so a launch that names none still stages its prompt somewhere.
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("failed to create %q: %w", dir, err)
		}
	}

	file, err := os.CreateTemp(dir, pattern)
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
