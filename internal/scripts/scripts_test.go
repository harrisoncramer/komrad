package scripts

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

// TestDiscover covers what the directory offers as a script, including a file that cannot be
// run.
func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "open-pr"), 0o755)
	write(t, filepath.Join(dir, "not-executable"), 0o644)
	write(t, filepath.Join(dir, ".open-pr.swp"), 0o755)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "helpers"), 0o755))
	write(t, filepath.Join(dir, "helpers", "nested"), 0o755)
	require.NoError(t, os.Symlink(filepath.Join(dir, "open-pr"), filepath.Join(dir, "linked")))
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "fifo"), 0o755))

	found, err := discover(dir)
	require.NoError(t, err)

	listed := map[string]Script{}
	names := make([]string, 0, len(found))
	for _, script := range found {
		listed[script.Name] = script
		names = append(names, script.Name)
	}

	tests := []struct {
		name           string
		entry          string
		wantListed     bool
		wantExecutable bool
	}{
		{
			name:       "an editor's leftover is not a script",
			entry:      ".open-pr.swp",
			wantListed: false,
		},
		{
			name:       "a subdirectory is skipped rather than walked",
			entry:      "helpers",
			wantListed: false,
		},
		{
			name:       "a fifo is not a script, however its mode reads",
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
			name:           "an executable file is a script",
			entry:          "open-pr",
			wantListed:     true,
			wantExecutable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script, ok := listed[tt.entry]
			require.Equal(t, tt.wantListed, ok)
			if !tt.wantListed {
				return
			}

			assert.Equal(t, tt.wantExecutable, script.Executable)
			assert.Equal(t, filepath.Join(dir, tt.entry), script.Path)
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

	found, err := discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 2)

	assert.Equal(t, Header{Description: "Open the pull request.", Timeout: 45 * time.Second}, found[0].Header)
	assert.Equal(t, Header{}, found[1].Header, "a file that declares nothing carries nothing")
}

// TestDiscoverMissingDirectory covers never having written one, where nothing is wrong so
// nothing is reported.
func TestDiscoverMissingDirectory(t *testing.T) {
	found, err := discover(filepath.Join(t.TempDir(), "never-created"))

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

	t.Setenv("WEAVE_TEST_INHERITED", "from the panel")

	work := t.TempDir()
	found, err := discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	require.NoError(t, NewRunner().Run(RunParams{
		Script: found[0],
		Dir:    work,
		Env:    []string{`WEAVE_AGENT={"name":"auth"}`},
	}))

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
		wantIs   error
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

			found, err := discover(dir)
			require.NoError(t, err)
			require.Len(t, found, 1)

			err = NewRunner().Run(RunParams{Script: found[0], Dir: dir})

			require.Error(t, err)
			if tt.wantIs != nil {
				require.ErrorIs(t, err, tt.wantIs)
			}
		})
	}
}

// TestRunRefusesAMissingDirectory covers an agent outliving its worktree, where the refusal has
// to name the directory since the exec failure underneath blames the script instead.
func TestRunRefusesAMissingDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "open-pr"), 0o755)

	found, err := discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	gone := filepath.Join(t.TempDir(), "worktree-that-was-removed")

	err = NewRunner().Run(RunParams{Script: found[0], Dir: gone})

	require.ErrorIs(t, err, ErrDirMissing)
	assert.Contains(t, err.Error(), gone, "the refusal names the directory, not the script")
}

// TestRunRefusesADirectoryThatIsAFile covers the same check meeting a path that exists and is
// not somewhere a process can be started, which fails at chdir the same way nothing there does.
func TestRunRefusesADirectoryThatIsAFile(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "open-pr"), 0o755)

	found, err := discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	notADir := filepath.Join(dir, "open-pr")

	require.ErrorIs(t, NewRunner().Run(RunParams{Script: found[0], Dir: notADir}), ErrDirMissing)
}

// TestRunReportsTheOutcome covers the whole of what a caller with nothing waiting on an
// script can learn about it.
func TestRunReportsTheOutcome(t *testing.T) {
	tests := []struct {
		name       string
		contents   string
		wantCode   int
		wantErr    bool
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

			found, err := discover(dir)
			require.NoError(t, err)
			require.Len(t, found, 1)

			reported := make(chan outcomeReport, 1)
			require.NoError(t, NewRunner().Run(RunParams{
				Script: found[0],
				Dir:    dir,
				Done: func(result Result, err error) {
					reported <- outcomeReport{result: result, err: err}
				},
			}))

			got := await(t, reported)

			assert.Equal(t, tt.wantCode, got.result.ExitCode)
			assert.Contains(t, string(got.result.Stderr), tt.wantStderr)
			if !tt.wantErr {
				require.NoError(t, got.err)

				return
			}

			require.ErrorIs(t, got.err, ErrFailed)
			assert.Contains(t, got.err.Error(), "cleanup", "the failure names the script")
		})
	}
}

// TestRunCapturesStdoutToo covers the fire-and-forget path, where a script that fails through a
// plain echo on stdout would otherwise leave its log empty.
func TestRunCapturesStdoutToo(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "chatty"), []byte(
		"#!/bin/sh\necho an answer nobody asked for\n",
	), 0o755))

	found, err := discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	reported := make(chan outcomeReport, 1)
	require.NoError(t, NewRunner().Run(RunParams{
		Script: found[0],
		Dir:    dir,
		Done: func(result Result, _ error) {
			reported <- outcomeReport{result: result}
		},
	}))

	result := await(t, reported).result
	assert.Empty(t, result.Stdout, "Result.Stdout stays unused for this path")
	assert.Contains(t, string(result.Stderr), "an answer nobody asked for")
}

type outcomeReport struct {
	result Result
	err    error
}

func await(t *testing.T, reported chan outcomeReport) outcomeReport {
	t.Helper()

	select {
	case got := <-reported:
		return got
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the script never reported what it did")

		return outcomeReport{}
	}
}

// TestRunAndWaitReportsWhatTheScriptSaid covers the streams coming back apart, stdout being the
// script's answer and stderr what it told a person.
func TestRunAndWaitReportsWhatTheScriptSaid(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "provision"), []byte(
		"#!/bin/sh\necho progress >&2\nprintf '%s\\n' \"$WEAVE_TEST_ANSWER\"\n",
	), 0o755))

	found, err := discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	result, err := NewRunner().RunAndWait(t.Context(), RunParams{
		Script: found[0],
		Dir:    dir,
		Env:    []string{"WEAVE_TEST_ANSWER=/work/api-auth"},
	})
	require.NoError(t, err)
	assert.Equal(t, "/work/api-auth", strings.TrimSpace(string(result.Stdout)), "stdout carries what the script answered")
	assert.Equal(t, "progress", strings.TrimSpace(string(result.Stderr)), "stderr stays separate from it")
}

// TestRunAndWaitRefusesANonZeroExit covers a script that started fine and then disagreed,
// whose result still comes back since its stderr is the only thing that says why.
func TestRunAndWaitRefusesANonZeroExit(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "install"), []byte(
		"#!/bin/sh\necho 'no lockfile' >&2\nexit 3\n",
	), 0o755))

	found, err := discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	result, err := NewRunner().RunAndWait(t.Context(), RunParams{Script: found[0], Dir: dir})

	require.ErrorIs(t, err, ErrFailed)
	assert.Contains(t, err.Error(), "install", "the refusal names the script")
	assert.Equal(t, "no lockfile", strings.TrimSpace(string(result.Stderr)))
}

// TestRunAndWaitStopsAtTheDeadline covers a script that hangs, which without the deadline
// leaves the launch waiting on it forever.
func TestRunAndWaitStopsAtTheDeadline(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hangs"), []byte(
		"#!/bin/sh\nsleep 60\n",
	), 0o755))

	found, err := discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err = NewRunner().RunAndWait(ctx, RunParams{Script: found[0], Dir: dir})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(started), 30*time.Second, "the script is killed rather than waited out")
}

// TestRunStopsAtItsOwnDeadline covers a timeout on a run nothing is waiting for, where what
// happened reaches the outcome rather than a return value.
func TestRunStopsAtItsOwnDeadline(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hangs"), []byte(
		"#!/bin/sh\nsleep 60\n",
	), 0o755))

	found, err := discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	failed := make(chan error, 1)
	started := time.Now()
	require.NoError(t, NewRunner().Run(RunParams{
		Script:  found[0],
		Dir:     dir,
		Timeout: 100 * time.Millisecond,
		Done:    func(_ Result, err error) { failed <- err },
	}))

	select {
	case err := <-failed:
		require.ErrorIs(t, err, ErrFailed)
		assert.Contains(t, err.Error(), context.DeadlineExceeded.Error(), "the outcome says the deadline is what stopped it")
		assert.Less(t, time.Since(started), 30*time.Second, "the script is killed rather than waited out")
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

	found, err := discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	started := time.Now()
	_, err = NewRunner().RunAndWait(t.Context(), RunParams{
		Script:  found[0],
		Dir:     dir,
		Timeout: 100 * time.Millisecond,
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, err.Error(), "gave up after 100ms", "the refusal names the limit it passed")
	assert.Less(t, time.Since(started), 30*time.Second, "the script is killed rather than waited out")
}

// TestRunAndWaitKeepsTheTailOfALoudScript covers the output cap, where the end of a failing
// script is the part that says why.
func TestRunAndWaitKeepsTheTailOfALoudScript(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "loud"), []byte(
		"#!/bin/sh\nawk 'BEGIN { for (i = 0; i < 20000; i++) print \"filler line\" }'\necho the-last-thing\n",
	), 0o755))

	found, err := discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	result, err := NewRunner().RunAndWait(t.Context(), RunParams{Script: found[0], Dir: dir})
	require.NoError(t, err)

	assert.LessOrEqual(t, len(result.Stdout), outputLimit, "the stream is capped")
	assert.Contains(t, string(result.Stdout), "the-last-thing", "what is kept is the end, not the start")
}

// TestRunAndWaitAcceptsABackgroundChild pins that a script returning while a child of its own is
// still running is finished.
func TestRunAndWaitAcceptsABackgroundChild(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "provision"), []byte(
		"#!/bin/sh\nsleep 60 &\necho ready >&2\nexit 0\n",
	), 0o755))

	found, err := discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	result, err := NewRunner().RunAndWait(t.Context(), RunParams{
		Script:  found[0],
		Dir:     dir,
		Timeout: 10 * time.Minute,
	})

	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "ready", strings.TrimSpace(string(result.Stderr)), "what the script said still comes back")
}

// TestRunAndWaitRefusesWhatItCannotStart pins that a script that never started comes back as
// not startable, naming which script it was.
func TestRunAndWaitRefusesWhatItCannotStart(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad-shebang"), []byte("#!/nope/sh\n"), 0o755))

	found, err := discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	result, err := NewRunner().RunAndWait(t.Context(), RunParams{Script: found[0], Dir: dir})

	require.ErrorIs(t, err, ErrNotStartable)
	assert.Contains(t, err.Error(), "bad-shebang", "the refusal names the script")
	assert.Equal(t, -1, result.ExitCode, "a process that never ran has no status of its own")
}

// TestRunAndWaitRefusesBeforeItStarts covers the two guards that answer before there is a process
// at all, a bare Result reading as a clean exit.
func TestRunAndWaitRefusesBeforeItStarts(t *testing.T) {
	tests := []struct {
		name    string
		mode    os.FileMode
		dir     func(t *testing.T, found string) string
		wantErr error
	}{
		{
			name:    "a file nobody made executable",
			mode:    0o644,
			dir:     func(t *testing.T, found string) string { t.Helper(); return found },
			wantErr: ErrNotExecutable,
		},
		{
			name:    "a directory that is not there",
			mode:    0o755,
			dir:     func(*testing.T, string) string { return "/definitely/not/here" },
			wantErr: ErrDirMissing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "provision"), []byte("#!/bin/sh\nexit 0\n"), tt.mode))

			found, err := discover(dir)
			require.NoError(t, err)
			require.Len(t, found, 1)

			result, err := NewRunner().RunAndWait(t.Context(), RunParams{Script: found[0], Dir: tt.dir(t, dir)})

			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, -1, result.ExitCode, "a process that never ran has no status of its own")
		})
	}
}

// TestRunAndWaitRefusesAContextAlreadyDone pins that a context cancelled before the call runs
// nothing at all.
func TestRunAndWaitRefusesAContextAlreadyDone(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "provision"), []byte("#!/bin/sh\nexit 0\n"), 0o755))

	found, err := discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	result, err := NewRunner().RunAndWait(ctx, RunParams{
		Script:  found[0],
		Dir:     dir,
		Timeout: time.Minute,
	})

	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, ErrNotStartable, "a cancelled run says nothing about whether the file would exec")
	assert.Contains(t, err.Error(), "gave up after 1m0s", "the refusal names the limit it was given")
	assert.Equal(t, -1, result.ExitCode, "a process that never ran has no status of its own")
}

func write(t *testing.T, path string, mode os.FileMode) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), mode))
}

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
