package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"komrade/internal/caller"
	"komrade/internal/logging"
	"komrade/internal/logtest"
	"komrade/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func passes(t *testing.T, level slog.Level, results []error) string {
	t.Helper()

	each := xslice.Map(results, func(result error) func(context.Context) error {
		return func(context.Context) error { return result }
	})

	return drive(t, level, each)
}

func drive(t *testing.T, level slog.Level, each []func(context.Context) error) string {
	t.Helper()

	var out bytes.Buffer

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	remaining := each
	done := make(chan struct{})

	go func() {
		defer close(done)
		runLoop(ctx, Loop{
			Name:     "reconcile",
			Interval: time.Millisecond,
			Pass: func(ctx context.Context) error {
				if len(remaining) == 0 {
					cancel()

					return nil
				}

				pass := remaining[0]
				remaining = remaining[1:]

				return pass(ctx)
			},
		}, logging.New(&out, level))
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not stop")
	}

	return out.String()
}

// TestLoopLogsAFailureOnce covers a pass failing the same way over and over, which a
// two-second loop would otherwise write out a thousand times.
func TestLoopLogsAFailureOnce(t *testing.T) {
	down := errors.New("failed to list agents: connection refused")

	written := passes(t, slog.LevelInfo, []error{down, down, down, down})

	records := logtest.Records(t, written)
	require.Len(t, records, 1, "one failure however many times it repeats")
	assert.Equal(t, "loop pass failed", records[0]["body"])
	assert.Equal(t, "reconcile", records[0][logging.Loop])
	assert.Contains(t, records[0][logging.ErrorMessage], "connection refused")
}

// TestLoopLogsEachDistinctFailure covers a loop failing two different ways, a second failure
// being news whether or not the first is still happening.
func TestLoopLogsEachDistinctFailure(t *testing.T) {
	first := errors.New("failed to list agents")
	second := errors.New("failed to capture pane")

	written := passes(t, slog.LevelInfo, []error{first, first, second, first})

	assert.Equal(t, []string{
		"loop pass failed", "loop pass failed", "loop pass failed",
	}, logtest.Bodies(t, written))
}

// TestLoopLogsTheRepeatsAtDebug covers somebody chasing a failure that comes and goes, where
// which passes failed is the whole question.
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

// TestLoopLogsRecovery covers the first pass to work after a run of failures.
func TestLoopLogsRecovery(t *testing.T) {
	down := errors.New("failed to list agents")

	written := passes(t, slog.LevelInfo, []error{down, down, nil})

	records := logtest.Records(t, written)
	require.Len(t, records, 2)
	assert.Equal(t, "loop recovered", records[1]["body"])
	assert.EqualValues(t, 2, records[1][logging.Repeats])
}

// TestLoopSurvivesAPanickingPass covers a pass that panics instead of returning, which would
// otherwise end the process and freeze every agent's state where the last pass left it.
func TestLoopSurvivesAPanickingPass(t *testing.T) {
	written := drive(t, slog.LevelInfo, []func(context.Context) error{
		func(context.Context) error { panic("assignment to entry in nil map") },
		func(context.Context) error { return nil },
	})

	records := logtest.Records(t, written)
	require.Len(t, records, 2, "the panic and the pass that ran after it")
	assert.Equal(t, "loop pass failed", records[0]["body"])
	assert.Contains(t, records[0][logging.ErrorMessage], "assignment to entry in nil map")
	assert.Contains(t, records[0][logging.StackTrace], "background_test.go")
	assert.Equal(t, "loop recovered", records[1]["body"])
}

// TestLoopFoldsARepeatingPanic covers a pass panicking the same way every time, so a
// two-second loop does not write the same stack a thousand times over.
func TestLoopFoldsARepeatingPanic(t *testing.T) {
	panics := func(context.Context) error { panic("index out of range [30] with length 6") }

	written := drive(t, slog.LevelDebug, []func(context.Context) error{panics, panics, panics})

	assert.Equal(t, []string{
		"loop pass failed", "loop still failing", "loop still failing",
	}, logtest.Bodies(t, written))
}

// TestLoopSaysNothingOnceCancelled covers a shutdown reaching a pass partway through, where
// reporting its cancelled store calls would end a clean stop with a page of errors.
func TestLoopSaysNothingOnceCancelled(t *testing.T) {
	var out bytes.Buffer

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)
		runLoop(ctx, Loop{
			Name:     "transcript sync",
			Interval: time.Millisecond,
			Pass: func(ctx context.Context) error {
				cancel()

				return fmt.Errorf("failed to sync transcript: %w", ctx.Err())
			},
		}, logging.New(&out, slog.LevelDebug))
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not stop")
	}

	assert.Empty(t, logtest.Bodies(t, out.String()), "a pass cut short by the shutdown is not a failure")
}

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

// TestBackgroundOutlivesTheSignal keeps the signal off River's own context, which River
// treats as a hard stop that cuts a launch off mid-flight.
func TestBackgroundOutlivesTheSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	runner := &fakeRunner{}

	work, err := startBackground(ctx, runner, nil, logging.New(io.Discard, slog.LevelError))
	require.NoError(t, err)

	cancel()

	require.NotNil(t, runner.startCtx)
	require.NoError(t, runner.startCtx.Err(), "the context River was started on outlives the signal")

	work.stop(t.Context())
}

// TestBackgroundStopWaitsForTheLoops covers the loops being waited on rather than left to die with
// the process.
func TestBackgroundStopWaitsForTheLoops(t *testing.T) {
	var passes atomic.Int64
	runner := &fakeRunner{}

	work, err := startBackground(t.Context(), runner, []Loop{{
		Name:     "reconcile",
		Interval: time.Millisecond,
		Pass: func(context.Context) error {
			passes.Add(1)

			return nil
		},
	}}, logging.New(io.Discard, slog.LevelError))
	require.NoError(t, err)

	require.Eventually(t, func() bool { return passes.Load() > 0 }, 5*time.Second, time.Millisecond)

	work.stop(t.Context())

	settled := passes.Load()
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, settled, passes.Load(), "the loop is stopped rather than still ticking")
	assert.True(t, runner.stopped, "and the job runner is stopped after it")
}

// TestStartBackgroundReportsAFailedRunner covers River refusing to come up.
func TestStartBackgroundReportsAFailedRunner(t *testing.T) {
	_, err := startBackground(t.Context(), &fakeRunner{startErr: errors.New("river is unreachable")},
		nil, logging.New(io.Discard, slog.LevelError))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "river is unreachable")
}

// TestShutdownDeadlineOutlivesTheSignal covers each teardown stage getting a clock of its own
// rather than the already-cancelled signal's.
func TestShutdownDeadlineOutlivesTheSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	deadline, release := shutdownDeadline(ctx, jobStopTimeout)
	defer release()

	require.NoError(t, deadline.Err(), "the stage is not already over before it starts")

	at, ok := deadline.Deadline()
	require.True(t, ok, "the stage is bounded rather than waiting forever")
	assert.WithinDuration(t, time.Now().Add(jobStopTimeout), at, time.Second)
}

// TestEachPassIsTracedOnItsOwn covers one pass of a loop being one trace, so following a
// reconcile means naming its id rather than reading four loops interleaved.
func TestEachPassIsTracedOnItsOwn(t *testing.T) {
	var out bytes.Buffer
	logger := logging.New(&out, slog.LevelInfo)

	var traces []string
	for range 2 {
		ctx := tracedPass(t.Context(), logger)

		logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "a pass said something")
		traces = append(traces, caller.Trace(ctx))
	}

	records := logtest.Records(t, out.String())
	require.Len(t, records, 2)

	assert.NotEmpty(t, records[0][logging.TraceID], "a pass writes under a trace")
	assert.Equal(t, traces[0], records[0][logging.TraceID], "the id on the line is the one on the context")
	assert.Equal(t, traces[1], records[1][logging.TraceID])
	assert.NotEqual(t, traces[0], traces[1], "two passes are two traces")
}

// TestAFailedPassIsLoggedOnItsOwnTrace pins the failure onto the pass's own id.
func TestAFailedPassIsLoggedOnItsOwnTrace(t *testing.T) {
	written := drive(t, slog.LevelInfo, []func(context.Context) error{
		func(ctx context.Context) error {
			logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "the pass said something on its way")

			return errors.New("failed to list agents: connection refused")
		},
	})

	records := logtest.Records(t, written)
	require.Len(t, records, 2)

	trace, _ := records[0][logging.TraceID].(string)
	require.NotEmpty(t, trace, "the pass writes under a trace")

	assert.Equal(t, "loop pass failed", records[1]["body"])
	assert.Equal(t, trace, records[1][logging.TraceID], "the outcome is on the trace that produced it")
}

// TestEveryOutcomeCarriesTheTraceOfThePassItDescribes covers a repeat and a recovery each
// being written under the trace of the pass it describes.
func TestEveryOutcomeCarriesTheTraceOfThePassItDescribes(t *testing.T) {
	down := errors.New("failed to list agents")

	written := passes(t, slog.LevelDebug, []error{down, down, nil})

	records := logtest.Records(t, written)
	require.Equal(t, []string{
		"loop pass failed", "loop still failing", "loop recovered",
	}, logtest.Bodies(t, written))

	traces := map[string]bool{}
	for _, record := range records {
		trace, _ := record[logging.TraceID].(string)
		require.NotEmpty(t, trace, "every outcome names the pass it describes")
		traces[trace] = true
	}

	assert.Len(t, traces, 3, "three passes are three traces")
}
