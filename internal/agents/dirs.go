package agents

import "path/filepath"

// isSameDir compares two paths as the filesystem sees them.
func isSameDir(a, b string) bool {
	resolvedA, err := filepath.EvalSymlinks(a)
	if err != nil {
		resolvedA = a
	}

	resolvedB, err := filepath.EvalSymlinks(b)
	if err != nil {
		resolvedB = b
	}

	return filepath.Clean(resolvedA) == filepath.Clean(resolvedB)
}
