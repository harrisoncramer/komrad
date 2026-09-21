// Package xpath holds small filesystem path helpers.
package xpath

import "path/filepath"

// IsSameDir compares two paths as the filesystem sees them.
func IsSameDir(a, b string) bool {
	if a == b {
		return true
	}

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
