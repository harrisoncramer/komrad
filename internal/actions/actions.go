// Package actions finds and runs the executables the panel offers as actions. It knows
// nothing about agents: an action is a file and running one is a process with a directory
// and an environment, so what a script is told stays the caller's to decide.
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

	"weave/internal/logging"
)

// ErrNotExecutable is what running a file without the executable bit is refused with. The
// exec failure alone reads as permission denied on a path and says nothing about the chmod
// that fixes it.
var ErrNotExecutable = errors.New("the file is not executable: run chmod +x on it")

// ErrNotAScript is what Validate refuses a text file that does not open with a shebang with:
// it is not a compiled program either, so nothing on the machine knows how to run it.
var ErrNotAScript = errors.New("is text but does not start with a shebang, so nothing knows how to run it")

// ErrNotStartable is what a file that would not exec is refused with, most often a shebang
// naming an interpreter that is not installed. It is named so a caller can tell an
// unrunnable file from something going wrong on its own side.
var ErrNotStartable = errors.New("the file could not be started")

// ErrDirMissing is what running in a directory that is not there is refused with. The exec
// failure it would otherwise arrive as names the script rather than the directory, sending
// whoever reads it to look at a file that is fine.
var ErrDirMissing = errors.New("the directory to run in does not exist")

// ErrFailed is what an action that ran and exited non-zero is refused with, as opposed to
// ErrNotStartable, which means the file itself is wrong.
var ErrFailed = errors.New("the action exited non-zero")

// killDelay is how long a cancelled action's process group gets to die on its own before
// Wait returns anyway. Without it a child still holding the output pipe keeps Wait
// blocked, which is the hang the deadline was there to prevent.
const killDelay = 2 * time.Second

// outputLimit is how much of each stream is kept, since only the tail is ever read and a
// script that prints without stopping would otherwise cost the server its memory.
const outputLimit = 64 << 10

// Action is one file in the actions directory, identified by its own name, which is what
// anything else calls it. A file without the executable bit is still listed, so a script
// missing a chmod appears rather than reading as the feature being broken.
type Action struct {
	Name       string
	Path       string
	Executable bool
	// Text says the file is text rather than a compiled program, which is what a caller
	// offering to open one in an editor needs.
	Text bool
	// Header is what the comments at the top of the file declare about it.
	Header Header
}

// GetDir is the directory an action runs in.
func (a Action) GetDir() string {
	return filepath.Dir(a.Path)
}

// Validate reports every reason this action would not run as written, joined into one
// error: the executable bit is unset, it is text with no shebang and so is neither a script
// nor a compiled program, or a header directive weave could not read. It is stricter than
// Discover on purpose: listing an action is lenient, so a comment typo does not hide one
// that still runs, but a caller validating a file before it goes anywhere, weave doctor or a
// cookbook install, wants to know about all of it up front.
func (a Action) Validate() error {
	var problems []error

	if !a.Executable {
		problems = append(problems, ErrNotExecutable)
	}
	if a.Text && !a.Header.Shebang {
		problems = append(problems, ErrNotAScript)
	}
	for _, problem := range a.Header.Problems {
		problems = append(problems, errors.New(problem))
	}

	return errors.Join(problems...)
}

// Describe reads path the way one entry in the actions directory would be, regardless of
// which directory it actually sits in: whether it is a regular file, whether it is
// executable, and what its header declares. Discover calls this once per entry, and a
// cookbook validates a script it has not installed yet the same way, through
// ValidateAsInstalled, since a built-in action, a user's own, and one a cookbook is about to
// write are the same resource read the same way.
func Describe(path string) (Action, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Action{}, err
	}
	if !info.Mode().IsRegular() {
		return Action{}, fmt.Errorf("%s is not a regular file", path)
	}

	header, text := readHeader(path)

	return Action{
		Name:       filepath.Base(path),
		Path:       path,
		Executable: info.Mode().Perm()&0o111 != 0,
		Text:       text,
		Header:     header,
	}, nil
}

// ValidateAsInstalled reports every reason installing path as an action would not work, as
// if it already carried the executable bit an install always sets rather than whatever
// permission the source file happens to have. It is what a cookbook validates its own
// scripts with before InstallActions ever runs, since those files are not executable yet.
func ValidateAsInstalled(path string) error {
	action, err := Describe(path)
	if err != nil {
		return err
	}

	action.Executable = true

	return action.Validate()
}

// Runner starts actions as processes. It holds nothing, and exists so a caller can take
// running one as an interface and stand something else in its place under test.
type Runner struct{}

// NewRunner builds the runner.
func NewRunner() Runner {
	return Runner{}
}

// Discover lists the actions in dir by name, each with what its header declares. A
// directory that does not exist yields no actions and no error, since not having written
// one is the ordinary case.
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

		action, err := Describe(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}

		found = append(found, action)
	}

	return found, nil
}

// RunParams is what starting an action takes.
type RunParams struct {
	Action Action
	// Dir is where the process starts, and Env goes over the current environment.
	Dir string
	Env []string
	// Timeout is how long the process gets before its group is killed, and zero is no limit.
	// It is the caller's rather than the header's, so what an undeclared one falls back to is
	// decided above this.
	Timeout time.Duration
	// Done is handed what the process did, once, and may be nil. RunAndWait returns the
	// result instead and never calls it.
	Done func(Result, error)
}

// Result is what running an action to completion produced.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Run starts an action and returns once the process exists rather than once it has
// finished. What it does report is a failure to start, which is otherwise
// indistinguishable from a keypress that did nothing.
//
// Stdin and stdout are left nil, which os/exec maps to the null device: an inherited stdin
// would let a script read keystrokes meant for the panel, and inherited output would paint
// over the frame it is drawing. The file is executed directly rather than through a shell,
// so nothing in the environment is ever parsed as a command line.
func (r Runner) Run(p RunParams) error {
	if !p.Action.Executable {
		return fmt.Errorf("failed to run action %s: %w", p.Action.Name, ErrNotExecutable)
	}

	if info, err := os.Stat(p.Dir); err != nil || !info.IsDir() {
		return fmt.Errorf("failed to run action %s in %s: %w", p.Action.Name, p.Dir, ErrDirMissing)
	}

	var stderr tail

	// A run nothing waits for is still held to the timeout it was given, so an action that
	// declared one is bounded however it was fired. One with no timeout is left unbounded,
	// since nothing downstream is blocked and cutting off a script somebody fired by hand is
	// worse than letting it run long.
	ctx := context.Background()
	cancel := func() {}
	if p.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, p.Timeout)
	}

	cmd := exec.CommandContext(ctx, p.Action.Path)
	cmd.Dir = p.Dir
	cmd.Env = append(os.Environ(), p.Env...)
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = killGroup(cmd)
	cmd.WaitDelay = killDelay

	if err := cmd.Start(); err != nil {
		cancel()

		return fmt.Errorf("failed to run action %s: %w: %v", p.Action.Name, ErrNotStartable, err)
	}

	go func() {
		defer logging.LogAndRepanic(context.Background())
		defer cancel()

		waitErr := cmd.Wait()
		if expired := ctx.Err(); expired != nil {
			waitErr = fmt.Errorf("%w after %s: %v", expired, p.Timeout, waitErr)
		}

		if p.Done == nil {
			return
		}

		p.Done(r.outcome(outcomeParams{
			action:  p.Action,
			state:   cmd.ProcessState,
			stderr:  stderr.kept(),
			waitErr: waitErr,
		}))
	}()

	return nil
}

// RunAndWait runs an action and does not return until it has finished or ctx is done. It is for a caller with something downstream of the action, where Run is for one
// with nothing waiting. The result comes back whether or not the action succeeded, since a
// non-zero exit is exactly when its stderr is worth showing.
//
// It makes Run's refusals for Run's reasons, plus ErrFailed for a non-zero exit and ctx's
// error for one that outlived its deadline. Cancelling kills the whole process group, so a
// script that spawned a build does not leave it running.
func (r Runner) RunAndWait(ctx context.Context, p RunParams) (Result, error) {
	if !p.Action.Executable {
		return Result{}, fmt.Errorf("failed to run action %s: %w", p.Action.Name, ErrNotExecutable)
	}

	if info, err := os.Stat(p.Dir); err != nil || !info.IsDir() {
		return Result{}, fmt.Errorf("failed to run action %s in %s: %w", p.Action.Name, p.Dir, ErrDirMissing)
	}

	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.Timeout)
		defer cancel()
	}

	var stdout, stderr tail

	cmd := exec.CommandContext(ctx, p.Action.Path)
	cmd.Dir = p.Dir
	cmd.Env = append(os.Environ(), p.Env...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = killGroup(cmd)
	cmd.WaitDelay = killDelay

	err := cmd.Run()
	result := Result{Stdout: stdout.kept(), Stderr: stderr.kept(), ExitCode: r.exitCode(cmd.ProcessState)}

	switch {
	case ctx.Err() != nil:
		return result, fmt.Errorf("failed to run action %s: %w%s", p.Action.Name, ctx.Err(), gaveUpAfter(p.Timeout))
	case errors.As(err, new(*exec.ExitError)):
		return result, fmt.Errorf("failed to run action %s: %w: %v", p.Action.Name, ErrFailed, err)
	case err != nil:
		return result, fmt.Errorf("failed to run action %s: %w: %v", p.Action.Name, ErrNotStartable, err)
	}

	return result, nil
}

// killGroup kills the process group rather than the process. CommandContext's own kill
// reaches the leader only, leaving whatever it spawned running and holding the pipes open.
func killGroup(cmd *exec.Cmd) func() error {
	return func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// gaveUpAfter names the deadline a run passed, since the context error alone does not say
// what the limit was or that weave set it.
func gaveUpAfter(timeout time.Duration) string {
	if timeout <= 0 {
		return ""
	}

	return fmt.Sprintf(": gave up after %s", timeout)
}

type outcomeParams struct {
	action  Action
	state   *os.ProcessState
	stderr  []byte
	waitErr error
}

// outcome is what a finished action did. The exit status decides whether it worked rather
// than the wait's error, since a script that exited cleanly but left something holding
// stderr open comes back as exec.ErrWaitDelay.
func (r Runner) outcome(p outcomeParams) (Result, error) {
	result := Result{Stderr: p.stderr, ExitCode: r.exitCode(p.state)}
	if result.ExitCode == 0 {
		return result, nil
	}

	return result, fmt.Errorf("failed to run action %s: %w: %v", p.action.Name, ErrFailed, p.waitErr)
}

// tail collects the last outputLimit bytes written to it and discards what came before,
// since the end of a failing script's output is the part that says why.
type tail struct {
	buf bytes.Buffer
}

// Write accepts everything and keeps only what fits, and never reports a short write,
// since the cap is not the writing process's problem.
func (t *tail) Write(p []byte) (int, error) {
	t.buf.Write(p)
	if excess := t.buf.Len() - outputLimit; excess > 0 {
		t.buf.Next(excess)
	}

	return len(p), nil
}

func (t *tail) kept() []byte {
	return t.buf.Bytes()
}

// exitCode is what a process exited with, and -1 for one that was signalled or never ran.
func (Runner) exitCode(state *os.ProcessState) int {
	if state == nil {
		return -1
	}

	return state.ExitCode()
}
