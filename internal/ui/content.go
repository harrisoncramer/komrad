package ui

import "os"

// homeDir is the user's home directory, which paths under it are shortened against.
func homeDir() string {
	dir, _ := os.UserHomeDir()

	return dir
}
