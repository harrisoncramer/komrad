package actions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDiscover covers what the directory offers as an action and what it passes over. A file
// that cannot be run is still listed, because a script that was written and then never appears
// reads as the panel being broken rather than as a file wanting chmod.
func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "open-pr"), 0o755)
	write(t, filepath.Join(dir, "not-executable"), 0o644)
	write(t, filepath.Join(dir, ".open-pr.swp"), 0o755)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "helpers"), 0o755))
	write(t, filepath.Join(dir, "helpers", "nested"), 0o755)
	require.NoError(t, os.Symlink(filepath.Join(dir, "open-pr"), filepath.Join(dir, "linked")))
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "fifo"), 0o755))

	found, err := Discover(dir)
	require.NoError(t, err)

	listed := map[string]Action{}
	names := make([]string, 0, len(found))
	for _, action := range found {
		listed[action.Name] = action
		names = append(names, action.Name)
	}

	tests := []struct {
		name           string
		entry          string
		wantListed     bool
		wantExecutable bool
	}{
		{
			name:       "an editor's leftover is not an action",
			entry:      ".open-pr.swp",
			wantListed: false,
		},
		{
			name:       "a subdirectory is skipped rather than walked",
			entry:      "helpers",
			wantListed: false,
		},
		{
			// Reading one waits for a writer that is never coming, so listing it would
			// hang whoever reads the file rather than fail.
			name:       "a fifo is not an action, however its mode reads",
			entry:      "fifo",
			wantListed: false,
		},
		{
			name:       "a nested file is not reached",
			entry:      "nested",
			wantListed: false,
		},
		{
			name:           "a file without the bit is listed and will not run",
			entry:          "not-executable",
			wantListed:     true,
			wantExecutable: false,
		},
		{
			name:           "a symlink carries the mode of the file it points at",
			entry:          "linked",
			wantListed:     true,
			wantExecutable: true,
		},
		{
			name:           "an executable file is an action",
			entry:          "open-pr",
			wantListed:     true,
			wantExecutable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, ok := listed[tt.entry]
			require.Equal(t, tt.wantListed, ok)
			if !tt.wantListed {
				return
			}

			assert.Equal(t, tt.wantExecutable, action.Executable)
			assert.Equal(t, filepath.Join(dir, tt.entry), action.Path)
		})
	}

	assert.Equal(t, []string{"linked", "not-executable", "open-pr"}, names, "listed by name")
}

// TestDiscoverReadsWhatAFileDeclares covers the header arriving with the listing rather than
// being read again later.
func TestDiscoverReadsWhatAFileDeclares(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "open-pr"), []byte(
		"#!/bin/sh\n#WEAVE description=\"Open the pull request.\"\n#WEAVE timeout=\"45s\"\n",
	), 0o755))
	write(t, filepath.Join(dir, "silent"), 0o755)

	found, err := Discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 2)

	assert.Equal(t, Header{Description: "Open the pull request.", Timeout: 45 * time.Second, Shebang: true}, found[0].Header)
	assert.Equal(t, Header{Shebang: true}, found[1].Header, "a file that declares nothing carries nothing")
}

// TestDiscoverSaysWhichFilesAreText covers the one thing about a file that is not read off
// its header, which is whether there is text in it at all. An action is as readily a
// compiled program as a script, and a caller offering to open one in an editor needs to
// know which it has.
func TestDiscoverSaysWhichFilesAreText(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compiled"), []byte{0x7f, 'E', 'L', 'F', 0x00}, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "script"), []byte("#!/bin/sh\n"), 0o755))

	found, err := Discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 2)

	assert.False(t, found[0].Text, "a compiled program has nothing to read")
	assert.True(t, found[1].Text)
}

// TestDiscoverMissingDirectory covers the ordinary case of never having written one.
// Nothing is wrong, so nothing is reported: the panel says where it looked on screen.
func TestDiscoverMissingDirectory(t *testing.T) {
	found, err := Discover(filepath.Join(t.TempDir(), "never-created"))

	require.NoError(t, err)
	assert.Empty(t, found)
}

// TestRunPassesTheEnvironmentAndDirectory covers the contract a script is written
// against: it runs in the directory it was given, with what it was told about the agent in
// the environment, and inherits the rest of the environment around it.
func TestRunPassesTheEnvironmentAndDirectory(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	script := filepath.Join(dir, "record")
	require.NoError(t, os.WriteFile(script, []byte(
		"#!/bin/sh\n{ printf '%s\\n' \"$WEAVE_AGENT\" \"$PWD\" \"$WEAVE_TEST_INHERITED\"; } > "+out+"\n",
	), 0o755))

	// Set on this process rather than passed in, so what the script sees of it is
	// evidence the environment around the run is inherited rather than replaced.
	t.Setenv("WEAVE_TEST_INHERITED", "from the panel")

	work := t.TempDir()
	found, err := Discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	require.NoError(t, NewRunner().Run(RunParams{Action: found[0], Dir: work, Env: []string{`WEAVE_AGENT={"name":"auth"}`}}))

	written := eventually(t, out)
	assert.Contains(t, written, `{"name":"auth"}`, "the payload reaches the script")
	assert.Contains(t, written, work, "the script runs in the directory it was given")
	assert.Contains(t, written, "from the panel", "the environment around it is inherited, not replaced")
}

// TestRunRefusesWhatItCannotStart covers the failures that would otherwise be
// indistinguishable from a keypress that did nothing at all.
func TestRunRefusesWhatItCannotStart(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		contents string
		mode     os.FileMode
		// wantIs is the error the refusal has to carry, nil when any failure will do.
		wantIs error
	}{
		{
			name:     "a file without the executable bit names the chmod that fixes it",
			file:     "unrunnable",
			contents: "#!/bin/sh\n",
			mode:     0o644,
			wantIs:   ErrNotExecutable,
		},
		{
			name:     "a shebang naming an interpreter that is not there fails to start",
			file:     "bad-shebang",
			contents: "#!/nope/sh\n",
			mode:     0o755,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, tt.file), []byte(tt.contents), tt.mode))

			found, err := Discover(dir)
			require.NoError(t, err)
			require.Len(t, found, 1)

			err = NewRunner().Run(RunParams{Action: found[0], Dir: dir})

			require.Error(t, err)
			if tt.wantIs != nil {
				assert.ErrorIs(t, err, tt.wantIs)
			}
		})
	}
}

// TestRunRefusesAMissingDirectory covers an agent outliving the worktree it ran in, which is
// ordinary once a worktree is cleaned up. The refusal has to name the directory: the exec failure
// underneath it reports no such file against the script, which is the one thing here that is
// fine.
func TestRunRefusesAMissingDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "open-pr"), 0o755)

	found, err := Discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	gone := filepath.Join(t.TempDir(), "worktree-that-was-removed")

	err = NewRunner().Run(RunParams{Action: found[0], Dir: gone})

	require.ErrorIs(t, err, ErrDirMissing)
	assert.Contains(t, err.Error(), gone, "the refusal names the directory, not the script")
}

// TestRunRefusesADirectoryThatIsAFile covers the same check meeting a path that exists and is
// not somewhere a process can be started, which fails at chdir the same way nothing there does.
func TestRunRefusesADirectoryThatIsAFile(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "open-pr"), 0o755)

	found, err := Discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	notADir := filepath.Join(dir, "open-pr")

	assert.ErrorIs(t, NewRunner().Run(RunParams{Action: found[0], Dir: notADir}), ErrDirMissing)
}

// TestRunReportsTheOutcome covers the whole of what a caller with nothing waiting on an
// action can learn about it.
func TestRunReportsTheOutcome(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		wantCode int
		wantErr  bool
		// wantStderr is what the tail of the captured stream has to contain.
		wantStderr string
	}{
		{
			name:     "one that worked reports no failure",
			contents: "#!/bin/sh\nexit 0\n",
		},
		{
			name:       "one that exited non-zero reports the status and what it said",
			contents:   "#!/bin/sh\necho 'the worktree is dirty' >&2\nexit 3\n",
			wantCode:   3,
			wantErr:    true,
			wantStderr: "the worktree is dirty",
		},
		{
			name:     "one that was signalled reports no status of its own",
			contents: "#!/bin/sh\nkill -TERM $$\n",
			wantCode: -1,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "cleanup"), []byte(tt.contents), 0o755))

			found, err := Discover(dir)
			require.NoError(t, err)
			require.Len(t, found, 1)

			reported := make(chan outcomeReport, 1)
			require.NoError(t, NewRunner().Run(RunParams{
				Action: found[0],
				Dir:    dir,
				Done: func(result Result, err error) {
					reported <- outcomeReport{result: result, err: err}
				},
			}))

			got := await(t, reported)

			assert.Equal(t, tt.wantCode, got.result.ExitCode)
			assert.Contains(t, string(got.result.Stderr), tt.wantStderr)
			if !tt.wantErr {
				assert.NoError(t, got.err)

				return
			}

			require.ErrorIs(t, got.err, ErrFailed)
			assert.Contains(t, got.err.Error(), "cleanup", "the failure names the action")
		})
	}
}

// TestRunLeavesStdoutAlone covers the half of the streams that is still the null device.
func TestRunLeavesStdoutAlone(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "chatty"), []byte(
		"#!/bin/sh\necho an answer nobody asked for\n",
	), 0o755))

	found, err := Discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	reported := make(chan outcomeReport, 1)
	require.NoError(t, NewRunner().Run(RunParams{
		Action: found[0],
		Dir:    dir,
		Done: func(result Result, _ error) {
			reported <- outcomeReport{result: result}
		},
	}))

	assert.Empty(t, await(t, reported).result.Stdout)
}

type outcomeReport struct {
	result Result
	err    error
}

// await reads an outcome reported from the goroutine that reaped the process.
func await(t *testing.T, reported chan outcomeReport) outcomeReport {
	t.Helper()

	select {
	case got := <-reported:
		return got
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the action never reported what it did")

		return outcomeReport{}
	}
}

// TestRunAndWaitReportsWhatTheActionSaid covers the streams coming back apart, since a
// caller reads stdout as the action's answer and stderr as what it told a person.
func TestRunAndWaitReportsWhatTheActionSaid(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "provision"), []byte(
		"#!/bin/sh\necho progress >&2\nprintf '%s\\n' \"$WEAVE_TEST_ANSWER\"\n",
	), 0o755))

	found, err := Discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	result, err := NewRunner().RunAndWait(t.Context(), RunParams{Action: found[0], Dir: dir, Env: []string{"WEAVE_TEST_ANSWER=/work/api-auth"}})
	require.NoError(t, err)
	assert.Equal(t, "/work/api-auth", strings.TrimSpace(string(result.Stdout)), "stdout carries what the action answered")
	assert.Equal(t, "progress", strings.TrimSpace(string(result.Stderr)), "stderr stays separate from it")
}

// TestRunAndWaitRefusesANonZeroExit covers the case Run cannot have, an action that
// started fine and then disagreed. The result still comes back, since a failing action's
// stderr is the only thing that says why.
func TestRunAndWaitRefusesANonZeroExit(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "install"), []byte(
		"#!/bin/sh\necho 'no lockfile' >&2\nexit 3\n",
	), 0o755))

	found, err := Discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	result, err := NewRunner().RunAndWait(t.Context(), RunParams{Action: found[0], Dir: dir})

	require.ErrorIs(t, err, ErrFailed)
	assert.Contains(t, err.Error(), "install", "the refusal names the action")
	assert.Equal(t, "no lockfile", strings.TrimSpace(string(result.Stderr)))
}

// TestRunAndWaitStopsAtTheDeadline covers an action that hangs. Without the deadline the
// launch waiting on it waits forever, which is worse than one that is refused.
func TestRunAndWaitStopsAtTheDeadline(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hangs"), []byte(
		"#!/bin/sh\nsleep 60\n",
	), 0o755))

	found, err := Discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err = NewRunner().RunAndWait(ctx, RunParams{Action: found[0], Dir: dir})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(started), 30*time.Second, "the action is killed rather than waited out")
}

// TestRunStopsAtItsOwnDeadline covers a timeout on a run nothing is waiting for. Run answers as
// soon as the process exists, so the deadline is the run's own to enforce and what it did
// reaches the outcome rather than a return value.
func TestRunStopsAtItsOwnDeadline(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hangs"), []byte(
		"#!/bin/sh\nsleep 60\n",
	), 0o755))

	found, err := Discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	failed := make(chan error, 1)
	started := time.Now()
	require.NoError(t, NewRunner().Run(RunParams{
		Action:  found[0],
		Dir:     dir,
		Timeout: 100 * time.Millisecond,
		Done:    func(_ Result, err error) { failed <- err },
	}))

	select {
	case err := <-failed:
		require.ErrorIs(t, err, ErrFailed)
		assert.Contains(t, err.Error(), context.DeadlineExceeded.Error(), "the outcome says the deadline is what stopped it")
		assert.Less(t, time.Since(started), 30*time.Second, "the action is killed rather than waited out")
	case <-time.After(30 * time.Second):
		t.Fatal("the run was never reported as finished")
	}
}

// TestRunAndWaitStopsAtItsOwnDeadline covers an awaited run bounded by the timeout it was given
// rather than by the context it was handed.
func TestRunAndWaitStopsAtItsOwnDeadline(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hangs"), []byte(
		"#!/bin/sh\nsleep 60\n",
	), 0o755))

	found, err := Discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	started := time.Now()
	_, err = NewRunner().RunAndWait(t.Context(), RunParams{Action: found[0], Dir: dir, Timeout: 100 * time.Millisecond})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, err.Error(), "gave up after 100ms", "the refusal names the limit it passed")
	assert.Less(t, time.Since(started), 30*time.Second, "the action is killed rather than waited out")
}

// TestRunAndWaitKeepsTheTailOfALoudAction covers the cap. The end of a failing script's
// output is the part that says why, so what survives truncation has to be the tail.
func TestRunAndWaitKeepsTheTailOfALoudAction(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "loud"), []byte(
		"#!/bin/sh\nawk 'BEGIN { for (i = 0; i < 20000; i++) print \"filler line\" }'\necho the-last-thing\n",
	), 0o755))

	found, err := Discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	result, err := NewRunner().RunAndWait(t.Context(), RunParams{Action: found[0], Dir: dir})
	require.NoError(t, err)

	assert.LessOrEqual(t, len(result.Stdout), outputLimit, "the stream is capped")
	assert.Contains(t, string(result.Stdout), "the-last-thing", "what is kept is the end, not the start")
}

// write puts an empty file at path with the given mode, standing in for a script whose
// contents no test reads.
func write(t *testing.T, path string, mode os.FileMode) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), mode))
}

// eventually reads a file a started script is on its way to writing. Run returns as soon
// as the process exists rather than once it has finished, so the only thing a test can do
// is wait for what it left behind.
func eventually(t *testing.T, path string) string {
	t.Helper()

	var content []byte
	require.Eventually(t, func() bool {
		read, err := os.ReadFile(path)
		content = read

		return err == nil && len(read) > 0
	}, 5*time.Second, 10*time.Millisecond, "the script never wrote %s", path)

	return string(content)
}
