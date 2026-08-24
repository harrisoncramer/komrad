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

var ErrNotExecutable = errors.New("the file is not executable: run chmod +x on it")

var ErrNotStartable = errors.New("the file could not be started")

var ErrDirMissing = errors.New("the directory to run in does not exist")

var ErrFailed = errors.New("the action exited non-zero")

const killDelay = 2 * time.Second

const outputLimit = 64 << 10

type Action struct {
	Name       string
	Path       string
	Executable bool

	Text bool

	Header Header
}

// GetDir is the directory an action runs in.
func (a Action) GetDir() string {
	return filepath.Dir(a.Path)
}

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
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}

		header, text := readHeader(path)
		found = append(found, Action{
			Name:       entry.Name(),
			Path:       path,
			Executable: info.Mode().Perm()&0o111 != 0,
			Text:       text,
			Header:     header,
		})
	}

	return found, nil
}

type RunParams struct {
	Action Action

	Dir string
	Env []string

	Timeout time.Duration

	Done func(Result, error)
}

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

func killGroup(cmd *exec.Cmd) func() error {
	return func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

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

func (r Runner) outcome(p outcomeParams) (Result, error) {
	result := Result{Stderr: p.stderr, ExitCode: r.exitCode(p.state)}
	if result.ExitCode == 0 {
		return result, nil
	}

	return result, fmt.Errorf("failed to run action %s: %w: %v", p.action.Name, ErrFailed, p.waitErr)
}

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

func (Runner) exitCode(state *os.ProcessState) int {
	if state == nil {
		return -1
	}

	return state.ExitCode()
}
