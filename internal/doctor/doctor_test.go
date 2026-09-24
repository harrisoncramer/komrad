package doctor

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stub(name string, result Result, needs ...string) Check {
	return Check{
		Name:        name,
		Description: "a stand-in",
		Needs:       needs,
		Run:         func(context.Context, Env) Result { return result },
	}
}

// TestRunSkipsWhatItCannotCheck covers the reason Needs exists.
func TestRunSkipsWhatItCannotCheck(t *testing.T) {
	problem := &ProblemError{Code: "gone", Message: "it is not there"}

	report := Run(t.Context(), Env{}, []Check{
		stub("base", fail(problem, "", "install it")),
		stub("dependent", pass("never reached"), "base"),
		stub("transitive", pass("never reached either"), "dependent"),
		stub("unrelated", pass("ran anyway")),
	})

	statuses := map[string]Status{}
	for _, finding := range report.Findings {
		statuses[finding.Name] = finding.Status
	}

	assert.Equal(t, StatusFail, statuses["base"])
	assert.Equal(t, StatusSkip, statuses["dependent"])
	assert.Equal(t, StatusSkip, statuses["transitive"],
		"a check needing a skipped one is skipped too, so a chain collapses to the failure at its head")
	assert.Equal(t, StatusOK, statuses["unrelated"],
		"a failure stops only what depended on it, since the point is to report everything in one run")
}

// TestRunReportsEveryCheck covers that the list is the same length whatever happened, so a
// report can be diffed against another machine's.
func TestRunReportsEveryCheck(t *testing.T) {
	report := Run(t.Context(), Env{}, []Check{
		stub("one", pass("fine")),
		stub("two", fail(&ProblemError{Code: "bad", Message: "broken"}, "", "fix it")),
		stub("three", warn(&ProblemError{Code: "odd", Message: "unclear"}, "something")),
	})

	require.Len(t, report.Findings, 3)
	assert.Equal(t, Summary{
		OK:    1,
		Warn:  1,
		Fail:  1,
		Total: 3,
	}, report.Summary)
}

// TestSummaryHealth covers what makes a machine unhealthy: a warning is a fact rather than
// a problem, and a skip is the consequence of a failure already counted.
func TestSummaryHealth(t *testing.T) {
	tests := []struct {
		name  string
		check Check
		want  bool
	}{
		{
			name:  "a failure is not healthy",
			check: stub("a", fail(errors.New("bad"), "", "")),
			want:  false,
		},
		{
			name:  "a warning is a fact rather than a problem",
			check: stub("a", warn(errors.New("odd"), "")),
			want:  true,
		},
		{
			name:  "everything passing is healthy",
			check: stub("a", pass("")),
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := Run(t.Context(), Env{}, []Check{tt.check})
			assert.Equal(t, tt.want, report.Summary.Healthy)
		})
	}
}

// TestCodeOfReadsThroughTheChain covers that a check may wrap its sentinel to add what it
// saw without the code a script keys on being lost.
func TestCodeOfReadsThroughTheChain(t *testing.T) {
	sentinel := &ProblemError{Code: "tmux_too_old", Message: "too old"}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "no error at all",
			err:  nil,
			want: "",
		},
		{
			name: "an error carrying no problem",
			err:  errors.New("plain"),
			want: "",
		},
		{
			name: "the sentinel itself",
			err:  sentinel,
			want: "tmux_too_old",
		},
		{
			name: "wrapped with detail",
			err:  fmt.Errorf("%w: found 3.1", sentinel),
			want: "tmux_too_old",
		},
		{
			name: "wrapped twice",
			err:  fmt.Errorf("%w", fmt.Errorf("%w: found 3.1", sentinel)),
			want: "tmux_too_old",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, codeOf(tt.err))
		})
	}
}

// TestProblemsAreMatchable covers that a wrapped sentinel is still found by errors.Is,
// which is how a caller inside the binary tells one failure from another.
func TestProblemsAreMatchable(t *testing.T) {
	wrapped := fmt.Errorf("%w: found tmux 3.1c", errTmuxTooOld)

	require.ErrorIs(t, wrapped, errTmuxTooOld)
	assert.NotErrorIs(t, wrapped, errTmuxMissing)
}

// TestRunSurvivesACheckThatPanics covers the promise that matters most: the panel runs
// these to explain why its server would not start, so a bug in one check must cost that
// check's row and not the whole screen.
func TestRunSurvivesACheckThatPanics(t *testing.T) {
	report := Run(t.Context(), Env{}, []Check{
		stub("before", pass("fine")),
		{
			Name:        "broken",
			Description: "a check whose seam was never filled in",
			Run:         func(context.Context, Env) Result { panic("nil seam") },
		},
		stub("after", pass("also fine")),
	})

	require.Len(t, report.Findings, 3, "the rows either side of it are still reported")
	assert.Equal(t, StatusOK, report.Findings[0].Status)
	assert.Equal(t, StatusFail, report.Findings[1].Status)
	assert.Equal(t, "check_panicked", report.Findings[1].Code)
	assert.Equal(t, StatusOK, report.Findings[2].Status)
}

// TestWithEngineMarksAndCountsTheEngineFindings covers the report the doctor prints for a
// remote engine: what the service answered comes after what was made here, marked as the
// engine's, and the summary counts both.
func TestWithEngineMarksAndCountsTheEngineFindings(t *testing.T) {
	t.Parallel()

	here := Run(t.Context(), Env{}, []Check{stub("tmux", pass("3.5a"))})

	report := here.WithEngine([]Finding{
		{
			Name:   "tmux",
			Status: StatusOK,
			Detail: "3.4",
		},
		{
			Name:   "database",
			Status: StatusFail,
			Error:  "refused",
		},
	})

	require.Len(t, report.Findings, 3)
	assert.Empty(t, report.Findings[0].Where)
	assert.Equal(t, WhereEngine, report.Findings[1].Where)
	assert.Equal(t, WhereEngine, report.Findings[2].Where)
	assert.Equal(t, 3, report.Summary.Total)
	assert.Equal(t, 1, report.Summary.Fail)
	assert.False(t, report.Summary.Healthy)
	assert.True(t, here.Summary.Healthy, "the report made here is left as it was")
}

// TestEngineStandIns covers the one finding that takes the engine's place when its checks
// could not be had, whose reason is the server check or the call itself.
func TestEngineStandIns(t *testing.T) {
	t.Parallel()

	notChecked := EngineNotChecked()
	assert.Equal(t, StatusSkip, notChecked.Status)
	assert.Equal(t, WhereEngine, notChecked.Where)
	assert.Contains(t, notChecked.Detail, "server has to pass first")

	unanswered := EngineUnanswered(errors.New("connection reset"))
	assert.Equal(t, StatusFail, unanswered.Status)
	assert.Equal(t, WhereEngine, unanswered.Where)
	assert.Equal(t, "engine_unanswered", unanswered.Code)
	assert.Contains(t, unanswered.Error, "connection reset")
}
