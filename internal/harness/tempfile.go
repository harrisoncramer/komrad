package harness

import (
	"fmt"
	"os"
)

// WriteTempText writes text to a file under dir so a harness can get multi-line markdown past
// send-keys, left for the command that reads it to remove.
func WriteTempText(dir, pattern, text string) (string, error) {
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
