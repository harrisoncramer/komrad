package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"weave/internal/logging"
	"weave/lib/xsync"
)

const loopStopTimeout = 2 * time.Second

// River leaves an interrupted job running until its rescuer retries it.
const jobStopTimeout = 10 * time.Second

// Loop is one pass the service re-runs on an interval.
type Loop struct {
	Name     string
	Interval time.Duration
	Pass     func(context.Context) error
}

// Start begins the job queue and the background loops. Nothing runs until this is called,
// and the caller tears it down with Stop before Close.
func (s *Server) Start(ctx context.Context) error {
	work, err := startBackground(ctx, s.Jobs, s.loops(), s.logger)
	if err != nil {
		return err
	}

	s.background = work

	return nil
}

// Stop ends the loops before the job queue, so nothing enqueues more work while jobs drain.
// It is a no-op on a server that was never started.
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

// backgroundWork is what startBackground returns and stop tears down, loops first so they
// cannot enqueue more work while jobs drain.
type backgroundWork struct {
	runner    jobRunner
	stopLoops context.CancelFunc
	loops     *sync.WaitGroup
	logger    *slog.Logger
}

// River's startup context must survive shutdown so Stop can drain active jobs.
func startBackground(ctx context.Context, runner jobRunner, loops []Loop, logger *slog.Logger) (*backgroundWork, error) {
	uncancelled := context.WithoutCancel(ctx)

	if err := runner.Start(uncancelled); err != nil {
		return nil, fmt.Errorf("failed to start the job runner: %w", err)
	}

	loopCtx, stopLoops := context.WithCancel(uncancelled)

	var wg sync.WaitGroup
	for _, loop := range loops {
		wg.Add(1)

		go func() {
			defer wg.Done()
			defer logging.LogAndRepanic(loopCtx)

			runLoop(loopCtx, loop, logger)
		}()
	}

	return &backgroundWork{runner: runner, stopLoops: stopLoops, loops: &wg, logger: logger}, nil
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

func runLoop(ctx context.Context, loop Loop, logger *slog.Logger) {
	ticker := time.NewTicker(loop.Interval)
	defer ticker.Stop()

	logger = logger.With(slog.String(logging.Loop, loop.Name))

	var failing string
	var repeats int

	for {
		err := runPass(ctx, loop.Pass)

		if ctx.Err() != nil {
			return
		}

		switch {
		case err == nil && failing != "":
			logger.LogAttrs(ctx, slog.LevelInfo, "loop recovered", slog.Int(logging.Repeats, repeats))
			failing, repeats = "", 0
		case err != nil && err.Error() == failing:
			repeats++
			logger.LogAttrs(ctx, slog.LevelDebug, "loop still failing", slog.Int(logging.Repeats, repeats))
		case err != nil:
			failing, repeats = err.Error(), 1
			logger.LogAttrs(ctx, slog.LevelError, "loop pass failed", failureAttrs(err)...)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Recover inside the loop goroutine so one bad pass does not stop the service.
func runPass(ctx context.Context, pass func(context.Context) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = &panicError{value: recovered, stack: string(debug.Stack())}
		}
	}()

	return pass(ctx)
}

type panicError struct {
	value any
	stack string
}

func (e *panicError) Error() string {
	return fmt.Sprintf("the pass panicked: %v", e.value)
}

func failureAttrs(err error) []slog.Attr {
	attrs := logging.Err(err)

	var panicked *panicError
	if errors.As(err, &panicked) {
		attrs = append(attrs, slog.String(logging.StackTrace, panicked.stack))
	}

	return attrs
}
