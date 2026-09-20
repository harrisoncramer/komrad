package label

import (
	"path/filepath"
	"strings"
)

// Path spells a path under the home directory with a tilde.
func Path(path, home string) string {
	if home == "" || path == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rel, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rel
	}

	return path
}
