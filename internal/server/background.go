package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"

	"komrad/internal/caller"
	"komrad/internal/logging"
	"komrad/lib/xsync"
)

const loopStopTimeout = 2 * time.Second

const jobStopTimeout = 10 * time.Second

// LoopWaker asks one loop to run before its next tick.
type LoopWaker struct {
	wake chan struct{}
}

// NewLoopWaker builds a waker holding no wake.
func NewLoopWaker() *LoopWaker {
	return &LoopWaker{wake: make(chan struct{}, 1)}
}

// Wake asks the loop to run again, dropping the ask when one is already waiting.
func (w *LoopWaker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *LoopWaker) drain() {
	select {
	case <-w.wake:
	default:
	}
}

// NewLoopParams is what a loop is built from.
type NewLoopParams struct {
	Name     string
	Interval time.Duration
	// Floor is the least time after one run starts that a woken run may start.
	Floor time.Duration
	Run   func(context.Context) error
	// Waker is what wakes the loop, one of its own when nil.
	Waker *LoopWaker
}

// Loop is a function the server runs on an interval, and sooner when woken.
type Loop struct {
	name     string
	interval time.Duration
	floor    time.Duration
	run      func(context.Context) error
	waker    *LoopWaker
}

// NewLoop builds a loop that runs on its interval and whenever its waker is woken.
func NewLoop(params NewLoopParams) *Loop {
	waker := params.Waker
	if waker == nil {
		waker = NewLoopWaker()
	}

	return &Loop{
		name:     params.Name,
		interval: params.Interval,
		floor:    params.Floor,
		run:      params.Run,
		waker:    waker,
	}
}

// Name returns the loop's name.
func (l *Loop) Name() string {
	return l.name
}

// Run calls the loop's run function once.
func (l *Loop) Run(ctx context.Context) error {
	return l.run(ctx)
}

// Start begins the job queue and the background loops, which the caller tears down with
// Stop before Close.
func (s *Server) Start(ctx context.Context) error {
	work, err := startBackground(ctx, s.Jobs, s.Loops, s.logger)
	if err != nil {
		return err
	}

	s.background = work

	return nil
}

// Stop ends the loops before the job queue, so nothing enqueues more work while jobs drain.
func (s *Server) Stop(ctx context.Context) {
	if s.background == nil {
		return
	}

	s.background.stop(ctx)
}

// StopRequested answers when something has asked the service to stop over its own stop route.
func (s *Server) StopRequested() <-chan struct{} {
	return s.stopRequested
}

type jobRunner interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

type backgroundWork struct {
	runner    jobRunner
	stopLoops context.CancelFunc
	loops     *sync.WaitGroup
	logger    *slog.Logger
}

func startBackground(ctx context.Context, runner jobRunner, loops []*Loop, logger *slog.Logger) (*backgroundWork, error) {
	uncancelled := context.WithoutCancel(ctx)

	if err := runner.Start(uncancelled); err != nil {
		return nil, fmt.Errorf("failed to start the job runner: %w", err)
	}

	loopCtx, stopLoops := context.WithCancel(uncancelled)

	var wg sync.WaitGroup
	for _, loop := range loops {
		wg.Go(func() {
			defer logging.LogAndRepanic(loopCtx)
			runLoop(loopCtx, loop, logger)
		})
	}

	return &backgroundWork{
		runner:    runner,
		stopLoops: stopLoops,
		loops:     &wg,
		logger:    logger,
	}, nil
}

func (b *backgroundWork) stop(ctx context.Context) {
	b.stopLoops()

	finished := xsync.WithinTimeout(loopStopTimeout, func() {
		defer logging.LogAndRepanic(context.Background())

		b.loops.Wait()
	})
	if !finished {
		b.logger.LogAttrs(ctx, slog.LevelWarn, "a background loop did not stop in time")
	}

	deadline, cancel := shutdownDeadline(ctx, jobStopTimeout)
	defer cancel()

	if err := b.runner.Stop(deadline); err != nil {
		b.logger.LogAttrs(ctx, slog.LevelError, "gave up waiting for a job to finish", logging.Err(err)...)
	}
}

func shutdownDeadline(ctx context.Context, within time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), within)
}

func runLoop(ctx context.Context, loop *Loop, logger *slog.Logger) {
	ticker := time.NewTicker(loop.interval)
	defer ticker.Stop()

	logger = logger.With(slog.String(logging.Loop, loop.name))

	var failing string
	var repeats int
	var woken bool

	loop.waker.drain()

	for {
		started := time.Now()
		traced := tracedRun(ctx, logger, woken)

		err := runOnce(traced, loop.run)

		if ctx.Err() != nil {
			return
		}

		runLogger := logging.From(traced)

		switch {
		case err == nil && failing != "":
			runLogger.LogAttrs(traced, slog.LevelInfo, "loop recovered", slog.Int(logging.Repeats, repeats))
			failing, repeats = "", 0
		case err != nil && err.Error() == failing:
			repeats++
			runLogger.LogAttrs(traced, slog.LevelDebug, "loop still failing", slog.Int(logging.Repeats, repeats))
		case err != nil:
			failing, repeats = err.Error(), 1
			runLogger.LogAttrs(traced, slog.LevelError, "loop run failed", failureAttrs(err)...)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			woken = false
		case <-loop.waker.wake:
			woken = true
			if !waitUntil(ctx, started.Add(loop.floor)) {
				return
			}
		}
	}
}

// waitUntil sleeps until the moment, reporting false when the context ends first.
func waitUntil(ctx context.Context, moment time.Time) bool {
	wait := time.Until(moment)
	if wait <= 0 {
		return true
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func tracedRun(ctx context.Context, logger *slog.Logger, woken bool) context.Context {
	trace := uuid.NewString()

	logger = logger.With(slog.String(logging.TraceID, trace))
	if woken {
		logger = logger.With(slog.Bool(logging.LoopWoken, true))
	}

	return caller.WithTrace(logging.WithLogger(ctx, logger), trace)
}

func runOnce(ctx context.Context, run func(context.Context) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = &panicError{value: recovered, stack: string(debug.Stack())}
		}
	}()

	return run(ctx)
}

type panicError struct {
	value any
	stack string
}

func (e *panicError) Error() string {
	return fmt.Sprintf("the run panicked: %v", e.value)
}

func failureAttrs(err error) []slog.Attr {
	attrs := logging.Err(err)

	var panicked *panicError
	if errors.As(err, &panicked) {
		attrs = append(attrs, slog.String(logging.StackTrace, panicked.stack))
	}

	return attrs
}
