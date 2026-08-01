package cli

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
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
	require.Len(t, records, 2, "one failure and the recovery that stopped the loop")
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

	// The pass that stops the loop succeeds, so a recovery closes every one of these.
	assert.Equal(t, []string{
		"loop pass failed", "loop pass failed", "loop pass failed", "loop recovered",
	}, logtest.Bodies(t, written))
}

// TestLoopLogsTheRepeatsAtDebug covers somebody chasing a failure that comes and goes.
// Turning the log up has to show every pass, since which passes failed is the question
// being asked.
func TestLoopLogsTheRepeatsAtDebug(t *testing.T) {
	down := errors.New("failed to list agents")

	written := passes(t, slog.LevelDebug, []error{down, down, down})

	records := logtest.Records(t, written)
	require.Len(t, records, 4)
	assert.Equal(t, []string{
		"loop pass failed", "loop still failing", "loop still failing", "loop recovered",
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
		"loop pass failed", "loop still failing", "loop still failing", "loop recovered",
	}, logtest.Bodies(t, written))
}

// TestServeLoggerRejectsAnUnknownLevel covers a level the config carries that slog has no
// answer for. It fails rather than falling back, so a misspelled level is reported instead
// of quietly leaving the log at a verbosity nobody chose.
func TestServeLoggerRejectsAnUnknownLevel(t *testing.T) {
	_, err := serveLogger("verbose")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "verbose")
}
