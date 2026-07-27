package valid

import "os"

// Environ, Setenv, and Unsetenv are process-env management, not configuration
// reads, so spawning a child process with a modified environment is allowed.
func spawnEnv() []string {
	_ = os.Setenv("CHILD_FLAG", "1")
	_ = os.Unsetenv("CHILD_FLAG")
	return os.Environ()
}

// A local shadow named os is not the standard library package.
type shadow struct{}

func (shadow) Getenv(string) string { return "" }

func shadowed() string {
	os := shadow{}
	return os.Getenv("HOME")
}
