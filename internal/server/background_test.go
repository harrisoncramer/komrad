package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"komrad/internal/caller"
	"komrad/internal/logging"
	"komrad/internal/logtest"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runs(t *testing.T, level slog.Level, results []error) string {
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
		runLoop(ctx, NewLoop(NewLoopParams{
			Name:     "reconcile",
			Interval: time.Millisecond,
			Run: func(ctx context.Context) error {
				if len(remaining) == 0 {
					cancel()

					return nil
				}

				run := remaining[0]
				remaining = remaining[1:]

				return run(ctx)
			},
		}), logging.New(&out, level))
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not stop")
	}

	return out.String()
}

// TestLoopLogsAFailureOnce covers a run failing the same way over and over, which a
// two-second loop would otherwise write out a thousand times.
func TestLoopLogsAFailureOnce(t *testing.T) {
	down := errors.New("failed to list agents: connection refused")

	written := runs(t, slog.LevelInfo, []error{down, down, down, down})

	records := logtest.Records(t, written)
	require.Len(t, records, 1, "one failure however many times it repeats")
	assert.Equal(t, "loop run failed", records[0]["body"])
	assert.Equal(t, "reconcile", records[0][logging.Loop])
	assert.Contains(t, records[0][logging.ErrorMessage], "connection refused")
}

// TestLoopLogsEachDistinctFailure covers a loop failing two different ways, a second failure
// being news whether or not the first is still happening.
func TestLoopLogsEachDistinctFailure(t *testing.T) {
	first := errors.New("failed to list agents")
	second := errors.New("failed to capture pane")

	written := runs(t, slog.LevelInfo, []error{first, first, second, first})

	assert.Equal(t, []string{
		"loop run failed", "loop run failed", "loop run failed",
	}, logtest.Bodies(t, written))
}

// TestLoopLogsTheRepeatsAtDebug covers somebody chasing a failure that comes and goes, where
// which runs failed is the whole question.
func TestLoopLogsTheRepeatsAtDebug(t *testing.T) {
	down := errors.New("failed to list agents")

	written := runs(t, slog.LevelDebug, []error{down, down, down})

	records := logtest.Records(t, written)
	require.Len(t, records, 3)
	assert.Equal(t, []string{
		"loop run failed", "loop still failing", "loop still failing",
	}, logtest.Bodies(t, written))
	assert.EqualValues(t, 3, records[2][logging.Repeats])
}

// TestLoopLogsRecovery covers the first run to work after a run of failures.
func TestLoopLogsRecovery(t *testing.T) {
	down := errors.New("failed to list agents")

	written := runs(t, slog.LevelInfo, []error{down, down, nil})

	records := logtest.Records(t, written)
	require.Len(t, records, 2)
	assert.Equal(t, "loop recovered", records[1]["body"])
	assert.EqualValues(t, 2, records[1][logging.Repeats])
}

// TestLoopSurvivesAPanickingRun covers a run that panics instead of returning, which would
// otherwise end the process and freeze every agent's state where the last run left it.
func TestLoopSurvivesAPanickingRun(t *testing.T) {
	written := drive(t, slog.LevelInfo, []func(context.Context) error{
		func(context.Context) error { panic("assignment to entry in nil map") },
		func(context.Context) error { return nil },
	})

	records := logtest.Records(t, written)
	require.Len(t, records, 2, "the panic and the run that ran after it")
	assert.Equal(t, "loop run failed", records[0]["body"])
	assert.Contains(t, records[0][logging.ErrorMessage], "assignment to entry in nil map")
	assert.Contains(t, records[0][logging.StackTrace], "background_test.go")
	assert.Equal(t, "loop recovered", records[1]["body"])
}

// TestLoopFoldsARepeatingPanic covers a run panicking the same way every time, so a
// two-second loop does not write the same stack a thousand times over.
func TestLoopFoldsARepeatingPanic(t *testing.T) {
	panics := func(context.Context) error { panic("index out of range [30] with length 6") }

	written := drive(t, slog.LevelDebug, []func(context.Context) error{panics, panics, panics})

	assert.Equal(t, []string{
		"loop run failed", "loop still failing", "loop still failing",
	}, logtest.Bodies(t, written))
}

// TestLoopSaysNothingOnceCancelled covers a shutdown reaching a run partway through, where
// reporting its cancelled store calls would end a clean stop with a page of errors.
func TestLoopSaysNothingOnceCancelled(t *testing.T) {
	var out bytes.Buffer

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)
		runLoop(ctx, NewLoop(NewLoopParams{
			Name:     "transcript sync",
			Interval: time.Millisecond,
			Run: func(ctx context.Context) error {
				cancel()

				return fmt.Errorf("failed to sync transcript: %w", ctx.Err())
			},
		}), logging.New(&out, slog.LevelDebug))
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not stop")
	}

	assert.Empty(t, logtest.Bodies(t, out.String()), "a run cut short by the shutdown is not a failure")
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
	var runs atomic.Int64
	runner := &fakeRunner{}

	work, err := startBackground(t.Context(), runner, []*Loop{NewLoop(NewLoopParams{
		Name:     "reconcile",
		Interval: time.Millisecond,
		Run: func(context.Context) error {
			runs.Add(1)

			return nil
		},
	})}, logging.New(io.Discard, slog.LevelError))
	require.NoError(t, err)

	require.Eventually(t, func() bool { return runs.Load() > 0 }, 5*time.Second, time.Millisecond)

	work.stop(t.Context())

	settled := runs.Load()
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, settled, runs.Load(), "the loop is stopped rather than still ticking")
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

// TestEachRunIsTracedOnItsOwn covers one run of a loop being one trace, so following a
// reconcile means naming its id rather than reading four loops interleaved.
func TestEachRunIsTracedOnItsOwn(t *testing.T) {
	var out bytes.Buffer
	logger := logging.New(&out, slog.LevelInfo)

	var traces []string
	for range 2 {
		ctx := tracedRun(t.Context(), logger, false)

		logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "a run said something")
		traces = append(traces, caller.Trace(ctx))
	}

	records := logtest.Records(t, out.String())
	require.Len(t, records, 2)

	assert.NotEmpty(t, records[0][logging.TraceID], "a run writes under a trace")
	assert.Equal(t, traces[0], records[0][logging.TraceID], "the id on the line is the one on the context")
	assert.Equal(t, traces[1], records[1][logging.TraceID])
	assert.NotEqual(t, traces[0], traces[1], "two runs are two traces")
}

// TestAFailedRunIsLoggedOnItsOwnTrace pins the failure onto the run's own id.
func TestAFailedRunIsLoggedOnItsOwnTrace(t *testing.T) {
	written := drive(t, slog.LevelInfo, []func(context.Context) error{
		func(ctx context.Context) error {
			logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "the run said something on its way")

			return errors.New("failed to list agents: connection refused")
		},
	})

	records := logtest.Records(t, written)
	require.Len(t, records, 2)

	trace, _ := records[0][logging.TraceID].(string)
	require.NotEmpty(t, trace, "the run writes under a trace")

	assert.Equal(t, "loop run failed", records[1]["body"])
	assert.Equal(t, trace, records[1][logging.TraceID], "the outcome is on the trace that produced it")
}

// TestEveryOutcomeCarriesTheTraceOfTheRunItDescribes covers a repeat and a recovery each
// being written under the trace of the run it describes.
func TestEveryOutcomeCarriesTheTraceOfTheRunItDescribes(t *testing.T) {
	down := errors.New("failed to list agents")

	written := runs(t, slog.LevelDebug, []error{down, down, nil})

	records := logtest.Records(t, written)
	require.Equal(t, []string{
		"loop run failed", "loop still failing", "loop recovered",
	}, logtest.Bodies(t, written))

	traces := map[string]bool{}
	for _, record := range records {
		trace, _ := record[logging.TraceID].(string)
		require.NotEmpty(t, trace, "every outcome names the run it describes")
		traces[trace] = true
	}

	assert.Len(t, traces, 3, "three runs are three traces")
}

// runStarts records when each run of a loop began.
type runStarts struct {
	mu     sync.Mutex
	starts []time.Time
}

func (r *runStarts) record() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.starts = append(r.starts, time.Now())
}

func (r *runStarts) read() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]time.Time(nil), r.starts...)
}

func (r *runStarts) count() int {
	return len(r.read())
}

func startLoop(t *testing.T, loop *Loop) (context.CancelFunc, <-chan struct{}) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)
		runLoop(ctx, loop, logging.New(io.Discard, slog.LevelError))
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	return cancel, done
}

func buildRecordedLoop(starts *runStarts, interval, floor time.Duration, waker *LoopWaker) *Loop {
	return NewLoop(NewLoopParams{
		Name:     "pull request sync",
		Interval: interval,
		Floor:    floor,
		Waker:    waker,
		Run: func(context.Context) error {
			starts.record()

			return nil
		},
	})
}

func buildHeldLoop(starts *runStarts, interval time.Duration, waker *LoopWaker, release <-chan struct{}) *Loop {
	return NewLoop(NewLoopParams{
		Name:     "pull request sync",
		Interval: interval,
		Waker:    waker,
		Run: func(ctx context.Context) error {
			starts.record()
			if starts.count() == 1 {
				select {
				case <-release:
				case <-ctx.Done():
				}
			}

			return nil
		},
	})
}

// TestLoopRunsWhenWoken covers a wake bringing the next run forward rather than waiting out the
// interval.
func TestLoopRunsWhenWoken(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		starts := &runStarts{}
		waker := NewLoopWaker()
		startLoop(t, buildRecordedLoop(starts, time.Hour, 0, waker))
		synctest.Wait()
		require.Equal(t, 1, starts.count())

		waker.Wake()
		synctest.Wait()

		assert.Equal(t, 2, starts.count())
	})
}

// TestLoopFoldsWakesDuringARun covers wakes that arrive while a run is under way costing one
// more run between them.
func TestLoopFoldsWakesDuringARun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		starts := &runStarts{}
		waker := NewLoopWaker()
		release := make(chan struct{})
		startLoop(t, buildHeldLoop(starts, time.Hour, waker, release))
		synctest.Wait()

		for range 5 {
			waker.Wake()
		}
		close(release)
		synctest.Wait()

		assert.Equal(t, 2, starts.count(), "five wakes during one run are one more run")
	})
}

// TestLoopRunsOnceForATickAndAWakeTogether covers a tick and a wake both waiting when a run ends,
// which the next run answers together.
func TestLoopRunsOnceForATickAndAWakeTogether(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const interval = 100 * time.Millisecond

		starts := &runStarts{}
		waker := NewLoopWaker()
		release := make(chan struct{})
		startLoop(t, buildHeldLoop(starts, interval, waker, release))
		synctest.Wait()

		time.Sleep(interval + interval/2)
		waker.Wake()
		close(release)
		synctest.Wait()
		require.Equal(t, 2, starts.count())

		time.Sleep(interval / 4)
		synctest.Wait()

		assert.Equal(t, 2, starts.count(), "the tick and the wake were answered by one run")
	})
}

// TestLoopIgnoresAWakeFromBeforeItStarted covers a wake sent before the loops start, which the
// first run already answers.
func TestLoopIgnoresAWakeFromBeforeItStarted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		starts := &runStarts{}
		waker := NewLoopWaker()
		waker.Wake()

		startLoop(t, buildRecordedLoop(starts, time.Hour, 0, waker))
		synctest.Wait()

		assert.Equal(t, 1, starts.count())
	})
}

// TestLoopHoldsAWokenRunToItsFloor covers a woken run waiting out the floor after the run before
// it started.
func TestLoopHoldsAWokenRunToItsFloor(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const floor = 100 * time.Millisecond

		starts := &runStarts{}
		waker := NewLoopWaker()
		startLoop(t, buildRecordedLoop(starts, time.Hour, floor, waker))
		synctest.Wait()

		waker.Wake()
		synctest.Wait()
		require.Equal(t, 1, starts.count(), "the woken run started inside the floor")

		time.Sleep(floor)
		synctest.Wait()

		recorded := starts.read()
		require.Len(t, recorded, 2)
		assert.Equal(t, floor, recorded[1].Sub(recorded[0]))
	})
}

// TestLoopKeepsItsTickAfterAWokenRun covers a woken run leaving the next ticked run where it was,
// so a wake can only add a run.
func TestLoopKeepsItsTickAfterAWokenRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const interval = 300 * time.Millisecond

		starts := &runStarts{}
		waker := NewLoopWaker()
		startLoop(t, buildRecordedLoop(starts, interval, 0, waker))
		synctest.Wait()

		time.Sleep(interval / 3)
		waker.Wake()
		synctest.Wait()
		require.Equal(t, 2, starts.count())

		time.Sleep(interval - interval/3)
		synctest.Wait()

		recorded := starts.read()
		require.Len(t, recorded, 3)
		assert.Equal(t, interval, recorded[2].Sub(recorded[0]), "the tick came on its schedule rather than an interval after the woken run")
	})
}

// TestLoopDropsATickThatLandsInsideTheFloor covers a tick that fires while a woken run waits out
// its floor, which the woken run answers.
func TestLoopDropsATickThatLandsInsideTheFloor(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const interval = 100 * time.Millisecond
		const floor = 60 * time.Millisecond

		starts := &runStarts{}
		waker := NewLoopWaker()
		startLoop(t, buildRecordedLoop(starts, interval, floor, waker))
		synctest.Wait()

		time.Sleep(70 * time.Millisecond)
		waker.Wake()
		synctest.Wait()
		time.Sleep(10 * time.Millisecond)
		waker.Wake()
		time.Sleep(110 * time.Millisecond)
		synctest.Wait()

		recorded := starts.read()
		require.Len(t, recorded, 3)
		assert.Equal(t, 130*time.Millisecond, recorded[2].Sub(recorded[0]))

		time.Sleep(10 * time.Millisecond)
		synctest.Wait()

		assert.Len(t, starts.read(), 4, "the next run came on the tick after the floor")
	})
}

// TestLoopStopsWhileWaitingOutItsFloor covers a shutdown reaching a loop that is holding a woken
// run back.
func TestLoopStopsWhileWaitingOutItsFloor(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		starts := &runStarts{}
		waker := NewLoopWaker()
		cancel, done := startLoop(t, buildRecordedLoop(starts, time.Hour, time.Hour, waker))
		synctest.Wait()

		waker.Wake()
		synctest.Wait()
		cancel()
		synctest.Wait()

		select {
		case <-done:
		default:
			t.Fatal("the loop waited out its floor rather than stopping")
		}
		assert.Equal(t, 1, starts.count())
	})
}

// TestLoopWakerNeverBlocks covers waking a loop that is not running.
func TestLoopWakerNeverBlocks(t *testing.T) {
	waker := NewLoopWaker()

	woke := make(chan struct{})
	go func() {
		defer close(woke)
		for range 3 {
			waker.Wake()
		}
	}()

	select {
	case <-woke:
	case <-time.After(time.Second):
		t.Fatal("a wake blocked with no loop to take it")
	}
}

// TestLoopLogsAWokenRun covers a run brought forward by a wake saying so on its log lines.
func TestLoopLogsAWokenRun(t *testing.T) {
	var out syncBuffer
	waker := NewLoopWaker()
	starts := &runStarts{}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runLoop(ctx, NewLoop(NewLoopParams{
			Name:     "pull request sync",
			Interval: time.Hour,
			Waker:    waker,
			Run: func(context.Context) error {
				starts.record()

				return fmt.Errorf("failed to read the forge, run %d", starts.count())
			},
		}), logging.New(&out, slog.LevelInfo))
	}()
	require.Eventually(t, func() bool { return starts.count() == 1 }, 5*time.Second, time.Millisecond)

	waker.Wake()
	require.Eventually(t, func() bool { return len(logtest.Records(t, out.String())) == 2 }, 5*time.Second, time.Millisecond)
	cancel()
	<-done

	records := logtest.Records(t, out.String())
	assert.Nil(t, records[0][logging.LoopWoken], "the first run was not woken")
	assert.Equal(t, true, records[1][logging.LoopWoken])
}

// TestBuildLoopsGivesEachLoopItsOwnWaker covers no two loops sharing a waker, where one wake
// would run whichever read it first.
func TestBuildLoopsGivesEachLoopItsOwnWaker(t *testing.T) {
	loops := buildLoops(buildLoopsParams{
		Stores:   &stores{},
		Services: &services{},
	})

	wakers := map[*LoopWaker]string{}
	for _, loop := range loops {
		other, ok := wakers[loop.waker]
		require.False(t, ok, "%s shares a waker with %s", loop.Name(), other)
		wakers[loop.waker] = loop.Name()
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.buf.String()
}
