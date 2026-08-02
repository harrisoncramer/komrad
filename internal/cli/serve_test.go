package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"weave/internal/logging"
	"weave/internal/logtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// passes drives a loop through a fixed sequence of results and stops it, so a test says
// what each pass did and nothing waits on a clock. The loop runs its first pass before
// its first wait, so the sequence is consumed from the top.
func passes(t *testing.T, level slog.Level, results []error) string {
	t.Helper()

	each := make([]func() error, 0, len(results))
	for _, result := range results {
		each = append(each, func() error { return result })
	}

	return drive(t, level, each)
}

// drive is passes over passes that do something other than return, which is how a test
// supplies one that panics. A result cannot express a panic, so the sequence holds the
// passes themselves.
func drive(t *testing.T, level slog.Level, each []func() error) string {
	t.Helper()

	var out bytes.Buffer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	remaining := each
	done := make(chan struct{})

	go func() {
		defer close(done)
		runLoop(ctx, runLoopParams{
			Name:     "reconcile",
			Interval: time.Millisecond,
			Logger:   logging.New(&out, level),
			Pass: func(context.Context) error {
				if len(remaining) == 0 {
					cancel()

					return nil
				}

				pass := remaining[0]
				remaining = remaining[1:]

				return pass()
			},
		})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not stop")
	}

	return out.String()
}

// TestLoopLogsAFailureOnce covers a pass that keeps failing the same way, which is what a
// database that is down or a schema that is behind looks like from in here. The loop runs
// every couple of seconds, so writing every failure would be the same sentence a thousand
// times over and every other line in the file buried under it.
func TestLoopLogsAFailureOnce(t *testing.T) {
	down := errors.New("failed to list agents: connection refused")

	written := passes(t, slog.LevelInfo, []error{down, down, down, down})

	records := logtest.Records(t, written)
	require.Len(t, records, 1, "one failure however many times it repeats")
	assert.Equal(t, "loop pass failed", records[0]["body"])
	assert.Equal(t, "reconcile", records[0][logging.Loop])
	assert.Contains(t, records[0][logging.ErrorMessage], "connection refused")
}

// TestLoopLogsEachDistinctFailure covers a loop failing two different ways. Only the
// repeats are folded, since a second failure is news whether or not the first is still
// happening.
func TestLoopLogsEachDistinctFailure(t *testing.T) {
	first := errors.New("failed to list agents")
	second := errors.New("failed to capture pane")

	written := passes(t, slog.LevelInfo, []error{first, first, second, first})

	assert.Equal(t, []string{
		"loop pass failed", "loop pass failed", "loop pass failed",
	}, logtest.Bodies(t, written))
}

// TestLoopLogsTheRepeatsAtDebug covers somebody chasing a failure that comes and goes.
// Turning the log up has to show every pass, since which passes failed is the question
// being asked.
func TestLoopLogsTheRepeatsAtDebug(t *testing.T) {
	down := errors.New("failed to list agents")

	written := passes(t, slog.LevelDebug, []error{down, down, down})

	records := logtest.Records(t, written)
	require.Len(t, records, 3)
	assert.Equal(t, []string{
		"loop pass failed", "loop still failing", "loop still failing",
	}, logtest.Bodies(t, written))
	assert.EqualValues(t, 3, records[2][logging.Repeats])
}

// TestLoopLogsRecovery covers the pass that works after a run of failures. Nothing else
// says the thing came back, and a log that only records failures reads as a service still
// broken long after it recovered.
func TestLoopLogsRecovery(t *testing.T) {
	down := errors.New("failed to list agents")

	written := passes(t, slog.LevelInfo, []error{down, down, nil})

	records := logtest.Records(t, written)
	require.Len(t, records, 2)
	assert.Equal(t, "loop recovered", records[1]["body"])
	assert.EqualValues(t, 2, records[1][logging.Repeats])
}

// TestLoopSurvivesAPanickingPass covers a pass that panics instead of returning. Go would
// otherwise end the process over it, which freezes every agent's state at whatever the last
// pass wrote, so the panic is taken as a failed pass and the next one runs. The stack is
// asserted on because a panic value alone never says where it came from.
func TestLoopSurvivesAPanickingPass(t *testing.T) {
	written := drive(t, slog.LevelInfo, []func() error{
		func() error { panic("assignment to entry in nil map") },
		func() error { return nil },
	})

	records := logtest.Records(t, written)
	require.Len(t, records, 2, "the panic and the pass that ran after it")
	assert.Equal(t, "loop pass failed", records[0]["body"])
	assert.Contains(t, records[0][logging.ErrorMessage], "assignment to entry in nil map")
	assert.Contains(t, records[0][logging.StackTrace], "serve_test.go")
	assert.Equal(t, "loop recovered", records[1]["body"])
}

// TestLoopFoldsARepeatingPanic covers a pass panicking the same way every time, which is
// what a frame or a log one agent keeps producing looks like from in here. It folds through
// the same path a repeating error does, so a two-second loop does not write the same stack
// a thousand times over.
func TestLoopFoldsARepeatingPanic(t *testing.T) {
	panics := func() error { panic("index out of range [30] with length 6") }

	written := drive(t, slog.LevelDebug, []func() error{panics, panics, panics})

	assert.Equal(t, []string{
		"loop pass failed", "loop still failing", "loop still failing",
	}, logtest.Bodies(t, written))
}

// TestLoopSaysNothingOnceCancelled covers the shutdown reaching a pass that is partway
// through. Every store call a pass makes takes the loop's context, so the pass fails the
// moment it is cancelled, and reporting that would end a clean stop with an error naming
// every agent the pass had left to do.
func TestLoopSaysNothingOnceCancelled(t *testing.T) {
	var out bytes.Buffer

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		runLoop(ctx, runLoopParams{
			Name:     "transcript sync",
			Interval: time.Millisecond,
			Logger:   logging.New(&out, slog.LevelDebug),
			Pass: func(ctx context.Context) error {
				cancel()

				return fmt.Errorf("failed to sync transcript: %w", ctx.Err())
			},
		})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not stop")
	}

	assert.Empty(t, logtest.Bodies(t, out.String()), "a pass cut short by the shutdown is not a failure")
}

// TestServeLoggerRejectsAnUnknownLevel covers a level the config carries that slog has no
// answer for. It fails rather than falling back, so a misspelled level is reported instead
// of quietly leaving the log at a verbosity nobody chose.
func TestServeLoggerRejectsAnUnknownLevel(t *testing.T) {
	_, err := serveLogger("verbose")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "verbose")
}

// fakeRunner stands in for River and records the two things the teardown has to get
// right: the context it was started on, and the one it was stopped with.
type fakeRunner struct {
	startCtx context.Context //nolint:containedctx // the point of the fake is to hold it for inspection
	stopCtx  context.Context //nolint:containedctx // as above
	startErr error
	stopped  bool
}

func (f *fakeRunner) Start(ctx context.Context) error {
	f.startCtx = ctx

	return f.startErr
}

func (f *fakeRunner) Stop(ctx context.Context) error {
	f.stopCtx = ctx
	f.stopped = true

	return nil
}

// TestBackgroundOutlivesTheSignal is the one that matters. River treats cancelling the
// context it was started on as a hard stop, which would cut a launch off mid-flight
// instead of waiting for it, so the signal must not reach that context.
func TestBackgroundOutlivesTheSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &fakeRunner{}

	work, err := startBackground(ctx, backgroundParams{Runner: runner})
	require.NoError(t, err)

	cancel()

	require.NotNil(t, runner.startCtx)
	assert.NoError(t, runner.startCtx.Err(), "the context River was started on outlives the signal")

	work.stop(context.Background(), logging.New(io.Discard, slog.LevelError))
}

// TestBackgroundStopWaitsForTheLoops covers the loops being waited on rather than left to
// die with the process, which is what makes the teardown ordered rather than a race.
func TestBackgroundStopWaitsForTheLoops(t *testing.T) {
	var passes atomic.Int64
	runner := &fakeRunner{}

	work, err := startBackground(context.Background(), backgroundParams{
		Runner: runner,
		Loops: []runLoopParams{{
			Name:     "reconcile",
			Interval: time.Millisecond,
			Logger:   logging.New(io.Discard, slog.LevelError),
			Pass: func(context.Context) error {
				passes.Add(1)

				return nil
			},
		}},
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool { return passes.Load() > 0 }, 5*time.Second, time.Millisecond)

	work.stop(context.Background(), logging.New(io.Discard, slog.LevelError))

	// stop waits on the loops, so nothing runs after it returns.
	settled := passes.Load()
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, settled, passes.Load(), "the loop is stopped rather than still ticking")
	assert.True(t, runner.stopped, "and the job runner is stopped after it")
}

// TestStartBackgroundReportsAFailedRunner covers River refusing to come up, which is a
// server that can never fire a schedule and has to say so rather than serve anyway.
func TestStartBackgroundReportsAFailedRunner(t *testing.T) {
	_, err := startBackground(context.Background(), backgroundParams{
		Runner: &fakeRunner{startErr: errors.New("river is unreachable")},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "river is unreachable")
}

// TestShutdownDeadlineOutlivesTheSignal covers each stage of the teardown getting a clock
// of its own. The signal that started the shutdown is already cancelled by the time these
// are made, so deriving from it directly would give every stage no time at all.
func TestShutdownDeadlineOutlivesTheSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	deadline, release := shutdownDeadline(ctx, jobStopTimeout)
	defer release()

	assert.NoError(t, deadline.Err(), "the stage is not already over before it starts")

	at, ok := deadline.Deadline()
	require.True(t, ok, "the stage is bounded rather than waiting forever")
	assert.WithinDuration(t, time.Now().Add(jobStopTimeout), at, time.Second)
}

// TestAwaitWithinGivesUp covers the teardown moving on from something that will not
// finish. A loop pass talking to a tmux server that stopped answering, or a pool waiting
// on a query that will not end, would otherwise hold the process open for good.
func TestAwaitWithinGivesUp(t *testing.T) {
	assert.True(t, didFinishWithin(5*time.Second, func() {}), "work that finishes is waited for")

	blocked := make(chan struct{})
	defer close(blocked)

	assert.False(t, didFinishWithin(10*time.Millisecond, func() { <-blocked }), "work that hangs is left behind")
}

// TestShutdownSignalFollowsItsParent covers the context handed to serve still being the
// command's own, so a parent that goes away stops the server as a signal would.
func TestShutdownSignalFollowsItsParent(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())

	ctx, stop := shutdownSignal(parent)
	defer stop()

	assert.NoError(t, ctx.Err())

	cancelParent()

	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the parent did not stop the server")
	}
}

// TestShutdownSignalStopsOnASignal covers the wiring itself, that the signals a reload
// and a Ctrl-C actually send are the ones being listened for.
func TestShutdownSignalStopsOnASignal(t *testing.T) {
	ctx, stop := shutdownSignal(context.Background())
	defer stop()

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))

	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the signal did not stop the server")
	}
}
