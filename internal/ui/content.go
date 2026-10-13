package ui

import "os"

// homeDir is the user's home directory, which shortens the paths under it.
func homeDir() string {
	dir, _ := os.UserHomeDir()

	return dir
}
