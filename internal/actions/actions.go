// Package actions finds and runs the executables the panel offers as actions.
//
// It knows nothing about agents. An action is a file, and running one is a process with a
// directory and an environment, so what a script is told stays the caller's to decide.
package actions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Action is one file in the actions directory. Name is the file's own name, which is the
// whole of what identifies it: nothing is read out of the file, so what a script does is
// only ever legible from the script.
//
// Executable is whether the file carries the bit that lets it run. One without it is
// still listed, since the alternative is a script that was written and then never appears,
// which reads as the feature being broken rather than as a file wanting chmod.
type Action struct {
	Name       string
	Path       string
	Executable bool
}

// Discover lists the actions in dir, in the order os.ReadDir gives them, which is by name.
// A directory that does not exist yields no actions and no error, since not having written
// one is the ordinary case rather than a failure anybody caused.
//
// Only regular files are offered. Subdirectories are skipped rather than walked, so an action
// is always one file, and anything else with a name in here is passed over: opening a fifo
// blocks until something writes to it, which would hang the caller reading the file rather than
// fail. Each entry is stated rather than read off its directory entry, so a symlink into a
// dotfiles repo is judged by what it points at.
func Discover(dir string) ([]Action, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to read the actions directory %s: %w", dir, err)
	}

	found := make([]Action, 0, len(entries))
	for _, entry := range entries {
		// A dotfile in here is an editor's swap file or something left behind, not
		// something to offer to run.
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}

		found = append(found, Action{
			Name:       entry.Name(),
			Path:       path,
			Executable: info.Mode().Perm()&0o111 != 0,
		})
	}

	return found, nil
}

// ErrNotExecutable is what running a file without the executable bit is refused with. It
// is named rather than left to the exec failure, which surfaces as permission denied on a
// path and says nothing about the one thing that fixes it.
var ErrNotExecutable = errors.New("the file is not executable: run chmod +x on it")

// ErrNotStartable is what a file that would not exec is refused with, most often a shebang
// naming an interpreter that is not installed. It is named so a caller can tell the file being
// unrunnable from something going wrong on its own side, without reading errno itself.
var ErrNotStartable = errors.New("the file could not be started")

// ErrDirMissing is what running in a directory that is not there is refused with. It is named
// because the exec failure it would otherwise arrive as names the script and not the directory,
// which sends whoever reads it to look at a file that is fine.
var ErrDirMissing = errors.New("the directory to run in does not exist")

// Runner starts actions as processes. It holds nothing and exists so a caller can take
// running one as an interface and stand something else in its place under test.
type Runner struct{}

// Run starts an action in dir, with env over the current environment, and returns once the
// process exists rather than once it has finished. A script may take as long as it likes,
// and what it prints and what it changes are its own business.
//
// What it does report is a failure to start, which covers the failures indistinguishable
// from a keypress that did nothing at all. A file that cannot be executed, a directory that is
// no longer there, and a shebang naming an interpreter that is not there.
//
// The three standard streams are left nil, which os/exec maps to the null device. That is
// the point rather than an omission. An inherited stdin would let a script read the
// keystrokes meant for the panel, and inherited output would paint over the frame it is
// drawing.
//
// The file is executed directly rather than through a shell, so nothing in env is ever
// parsed as a command line and none of it needs quoting. The shebang decides what
// interprets the script.
func (Runner) Run(action Action, dir string, env []string) error {
	if !action.Executable {
		return fmt.Errorf("failed to run action %s: %w", action.Name, ErrNotExecutable)
	}

	// Stated before the exec rather than left to it, since a chdir that fails arrives as a
	// no-such-file naming the script, which is the one thing here that is not the problem.
	// A directory removed between this and the start still gets through and comes back as
	// ErrNotStartable, which is the honest answer for a race nothing could have prevented.
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("failed to run action %s in %s: %w", action.Name, dir, ErrDirMissing)
	}

	cmd := exec.Command(action.Path)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	// Its own process group, so restarting the panel does not take a script that is
	// still running down with it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to run action %s: %w: %v", action.Name, ErrNotStartable, err)
	}

	// Nothing reads the exit status, but something has to reap the child, or it stays a
	// zombie for as long as the process that started it runs.
	go func() { _ = cmd.Wait() }()

	return nil
}

// ErrFailed is what an action that ran and exited non-zero is refused with, as opposed
// to ErrNotStartable, which means the file itself is wrong.
var ErrFailed = errors.New("the action exited non-zero")

// outputLimit is how much of each stream is kept. Only the tail of an action's output is
// ever read, so buffering all of a script that prints without stopping would cost the
// server its memory for nothing.
const outputLimit = 64 << 10

// killDelay is how long a cancelled action's process group gets to die on its own after
// the kill before Wait returns anyway. Without it a child still holding the output pipe
// keeps Wait blocked, which is the hang the deadline was there to prevent.
const killDelay = 2 * time.Second

// Result is what running an action to completion produced. The streams are kept apart
// because they mean different things to the caller. Stdout is the action's answer, and
// stderr is what it had to say about it. Both are truncated to the last outputLimit
// bytes.
type Result struct {
	Stdout []byte
	Stderr []byte
}

// RunAndWait runs an action in dir and does not return until it has finished, or until
// ctx is done. It is for a caller with something downstream of the action, where Run is
// for one with nothing waiting on it.
//
// The result comes back whether or not the action succeeded, since a non-zero exit is
// exactly when its stderr is worth showing.
//
// The refusals Run makes are made here too and for the same reasons, plus ErrFailed for
// an action that exited non-zero and ctx's error for one that outlived its deadline.
// Cancelling kills the whole process group, so a script that spawned a build does not
// leave it running.
//
// Stdin is left nil for the same reason it is in Run. Both output streams are captured
// rather than inherited, so nothing an action prints paints over the frame the panel is
// drawing.
func (Runner) RunAndWait(ctx context.Context, action Action, dir string, env []string) (Result, error) {
	if !action.Executable {
		return Result{}, fmt.Errorf("failed to run action %s: %w", action.Name, ErrNotExecutable)
	}

	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return Result{}, fmt.Errorf("failed to run action %s in %s: %w", action.Name, dir, ErrDirMissing)
	}

	var stdout, stderr tail

	cmd := exec.CommandContext(ctx, action.Path)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// The group, not the process. CommandContext's own kill reaches the leader only,
	// which leaves whatever it spawned running and holding the pipes open.
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = killDelay

	err := cmd.Run()
	result := Result{Stdout: stdout.kept(), Stderr: stderr.kept()}

	switch {
	case ctx.Err() != nil:
		return result, fmt.Errorf("failed to run action %s: %w", action.Name, ctx.Err())
	case errors.As(err, new(*exec.ExitError)):
		return result, fmt.Errorf("failed to run action %s: %w: %v", action.Name, ErrFailed, err)
	case err != nil:
		return result, fmt.Errorf("failed to run action %s: %w: %v", action.Name, ErrNotStartable, err)
	}

	return result, nil
}

// tail collects the last outputLimit bytes written to it and discards what came before.
// The end of a failing script's output is the part that says why, so a truncated stream
// is still worth reading.
type tail struct {
	buf bytes.Buffer
}

// Write accepts everything and keeps only what fits, so a script printing endlessly
// costs a bounded amount of memory. It never reports a short write, since the cap is not
// the writing process's problem.
func (t *tail) Write(p []byte) (int, error) {
	t.buf.Write(p)
	if excess := t.buf.Len() - outputLimit; excess > 0 {
		t.buf.Next(excess)
	}

	return len(p), nil
}

// kept returns the bytes that survived the cap.
func (t *tail) kept() []byte {
	return t.buf.Bytes()
}
