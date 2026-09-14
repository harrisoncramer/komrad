// Package bintest decides whether a test that drives an external binary may skip itself when
// that binary is missing.
//
// A laptop without tmux still wants the rest of the suite, so the default is to skip. On CI a
// missing binary is a broken runner rather than a machine that was never going to run these
// tests, and a skip there reports success for a test that ran nothing.
package bintest

import (
	"os/exec"
	"testing"

	"weave/config"
)

// RequireVariable is the environment variable that turns a missing binary into a failure.
const RequireVariable = "WEAVE_REQUIRE_BINARIES"

// Require skips the test when name is not runnable, or fails it when RequireVariable is set.
func Require(t *testing.T, name string) {
	t.Helper()

	missing, required := decide(name)

	switch {
	case !missing:
	case required:
		t.Fatalf("%s is not installed and %s is set", name, RequireVariable)
	default:
		t.Skip(name + " is not installed")
	}
}

// decide reports whether name is unrunnable and whether a missing binary is required to be there.
func decide(name string) (missing, required bool) {
	_, err := exec.LookPath(name)
	_, set := config.LookupEnv(RequireVariable)

	return err != nil, set
}
