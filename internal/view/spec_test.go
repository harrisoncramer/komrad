package view

import (
	"fmt"
	"os"
	"strings"
)

// specPath is the settled design as plain text, kept beside the package since the RFC it was
// drawn in is not tracked.
const specPath = "testdata/canonical-sidebar.txt"

// readSpec is the lines of the settled design, as columns with no styling on them.
func readSpec() ([]string, error) {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read the settled design at %q: %w", specPath, err)
	}

	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n"), nil
}
