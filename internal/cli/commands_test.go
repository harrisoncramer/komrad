//go:build integration

package cli

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationEveryCommandAnswersForItself walks the whole tree rather than the commands somebody wrote
// a test for.
func TestIntegrationEveryCommandAnswersForItself(t *testing.T) {
	walk(t, NewRootCommand(), func(t *testing.T, cmd *cobra.Command, path string) {
		assert.NotEmpty(t, cmd.Short, "%s says nothing about itself", path)
		assert.NotContains(t, cmd.Short, "\n", "%s runs its summary onto a second line", path)

		if !cmd.HasSubCommands() {
			assert.NotNil(t, cmd.Args, "%s takes any arguments it is given", path)
			assert.True(t, cmd.RunE != nil || cmd.Run != nil, "%s does nothing when run", path)
		}
	})
}

// TestIntegrationNoTwoCommandsAnswerToOneName pins the tree against a name bound twice, which cobra resolves
// to whichever was registered first and leaves the other unreachable.
func TestIntegrationNoTwoCommandsAnswerToOneName(t *testing.T) {
	walk(t, NewRootCommand(), func(t *testing.T, cmd *cobra.Command, path string) {
		held := map[string]string{}
		for _, child := range cmd.Commands() {
			for _, name := range append([]string{child.Name()}, child.Aliases...) {
				if was, taken := held[name]; taken && was != child.Name() {
					assert.Failf(t, "one name, two commands", "%s: %q is both %s and %s", path, name, was, child.Name())

					continue
				}
				held[name] = child.Name()
			}
		}
	})
}

func walk(t *testing.T, cmd *cobra.Command, check func(*testing.T, *cobra.Command, string)) {
	t.Helper()

	var descend func(*cobra.Command, string)
	descend = func(current *cobra.Command, path string) {
		check(t, current, path)

		for _, child := range current.Commands() {
			descend(child, strings.TrimSpace(path+" "+child.Name()))
		}
	}

	descend(cmd, cmd.Name())
}

// TestIntegrationQuietErrorsAreTheOnesAlreadyAnswered pins which failures main prints and which it
// swallows.
func TestIntegrationQuietErrorsAreTheOnesAlreadyAnswered(t *testing.T) {
	for _, quiet := range []error{ErrLogged, ErrReported, ErrPanelHidden} {
		assert.True(t, IsQuiet(quiet), "%v has already been answered", quiet)
	}

	assert.False(t, IsQuiet(nil))
	assert.False(t, IsQuiet(assert.AnError), "an ordinary failure is main's to print")
}

// TestIntegrationColorFollowsWhatItWasAskedFor pins the three modes and the refusal.
func TestIntegrationColorFollowsWhatItWasAskedFor(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		out     io.Writer
		want    bool
		wantErr bool
	}{
		{
			name: "always, whatever it is writing to",
			mode: "always",
			out:  &bytes.Buffer{},
			want: true,
		},
		{
			name: "never, even at a terminal",
			mode: "never",
			out:  os.Stdout,
		},
		{
			name: "auto into a buffer is not a terminal",
			mode: "auto",
			out:  &bytes.Buffer{},
		},
		{
			name: "auto into a file that is not one either",
			mode: "auto",
			out:  os.NewFile(0, os.DevNull),
		},
		{
			name:    "anything else is the caller's mistake",
			mode:    "sometimes",
			out:     &bytes.Buffer{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := shouldColorize(tt.mode, tt.out)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "sometimes", "the refusal names what was asked for")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestIntegrationCloseWithinSaysWhichHandleHungRatherThanWaiting pins the timeout around shutdown.
func TestIntegrationCloseWithinSaysWhichHandleHungRatherThanWaiting(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, nil))

	closed := make(chan struct{})
	closeWithin(logger, "the pool", func() { close(closed) })

	select {
	case <-closed:
	default:
		t.Fatal("a handle that closes at once is closed")
	}
	assert.Empty(t, logged.String(), "and nothing is said about it")

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	closeWithin(logger, "the wedged pool", func() { <-release })

	assert.Contains(t, logged.String(), "did not close in time")
	assert.Contains(t, logged.String(), "the wedged pool", "the log names the handle that hung")
}

// TestIntegrationHookBinaryPathIsThisBuild pins that agents are launched with the hooks of the binary
// serving them.
func TestIntegrationHookBinaryPathIsThisBuild(t *testing.T) {
	path := getHookBinaryPath(t.Context())
	require.NotEmpty(t, path)

	self, err := os.Executable()
	require.NoError(t, err)
	assert.Equal(t, self, path)
}
