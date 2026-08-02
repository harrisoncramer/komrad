package doctor

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stub is a check that reports what it was told to, for the tests about how the run is
// sequenced rather than about any one assertion.
func stub(name string, result Result, needs ...string) Check {
	return Check{
		Name:        name,
		Description: "a stand-in",
		Needs:       needs,
		Run:         func(context.Context, Env) Result { return result },
	}
}

// TestRunSkipsWhatItCannotCheck covers the reason Needs exists: a machine missing one
// thing should say so once, rather than once for every check that would have used it.
func TestRunSkipsWhatItCannotCheck(t *testing.T) {
	problem := &Problem{Code: "gone", Message: "it is not there"}

	report := Run(t.Context(), Env{}, []Check{
		stub("base", fail(problem, "", "install it")),
		stub("dependent", ok("never reached"), "base"),
		stub("transitive", ok("never reached either"), "dependent"),
		stub("unrelated", ok("ran anyway")),
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
		stub("one", ok("fine")),
		stub("two", fail(&Problem{Code: "bad", Message: "broken"}, "", "fix it")),
		stub("three", warn(&Problem{Code: "odd", Message: "unclear"}, "something")),
	})

	require.Len(t, report.Findings, 3)
	assert.Equal(t, Summary{OK: 1, Warn: 1, Fail: 1, Total: 3}, report.Summary)
}

// TestSummaryHealth covers what makes a machine unhealthy. A warning is a fact rather than
// a problem, and a skip is the consequence of a failure already counted.
func TestSummaryHealth(t *testing.T) {
	tests := []struct {
		name  string
		check Check
		want  bool
	}{
		{name: "everything passing is healthy", check: stub("a", ok("")), want: true},
		{name: "a warning is not a problem", check: stub("a", warn(errors.New("odd"), "")), want: true},
		{name: "a failure is", check: stub("a", fail(errors.New("bad"), "", "")), want: false},
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
	sentinel := &Problem{Code: "tmux_too_old", Message: "too old"}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "the sentinel itself", err: sentinel, want: "tmux_too_old"},
		{name: "wrapped with detail", err: fmt.Errorf("%w: found 3.1", sentinel), want: "tmux_too_old"},
		{name: "wrapped twice", err: fmt.Errorf("%w", fmt.Errorf("%w: found 3.1", sentinel)), want: "tmux_too_old"},
		{name: "an error carrying no problem", err: errors.New("plain"), want: ""},
		{name: "no error at all", err: nil, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CodeOf(tt.err))
		})
	}
}

// TestProblemsAreMatchable covers that a wrapped sentinel is still found by errors.Is,
// which is how a caller inside the binary tells one failure from another.
func TestProblemsAreMatchable(t *testing.T) {
	wrapped := fmt.Errorf("%w: found tmux 3.1c", ErrTmuxTooOld)

	assert.ErrorIs(t, wrapped, ErrTmuxTooOld)
	assert.NotErrorIs(t, wrapped, ErrTmuxMissing)
}
