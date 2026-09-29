package setup

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"komrad/internal/doctor"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStep struct {
	observations []Observation
	repairErr    error
	repairs      int
}

func (s *fakeStep) name() string {
	return "fake"
}

func (s *fakeStep) observe(context.Context, Checker) Observation {
	next := s.observations[0]
	if len(s.observations) > 1 {
		s.observations = s.observations[1:]
	}

	return next
}

func (s *fakeStep) question() *huh.Form {
	return nil
}

func (s *fakeStep) answerUnattended() error {
	return nil
}

func (s *fakeStep) repair(context.Context) error {
	s.repairs++

	return s.repairErr
}

func runModel(t *testing.T, step Step) model {
	t.Helper()

	m := newModel(t.Context(), nil, []row{{step: step}})
	cmd := m.observe()
	for cmd != nil {
		msg := cmd()
		switch msg.(type) {
		case observedMsg, repairedMsg:
		default:
			return m
		}

		next, nextCmd := m.Update(msg)
		m, cmd = next.(model), nextCmd
	}

	return m
}

// TestModelObservesRepairsAndObservesAgain covers each way a step ends in the program.
func TestModelObservesRepairsAndObservesAgain(t *testing.T) {
	tests := []struct {
		name    string
		step    *fakeStep
		state   state
		repairs int
	}{
		{
			name:  "done without a repair",
			step:  &fakeStep{observations: []Observation{{Verdict: VerdictDone}}},
			state: stateDone,
		},
		{
			name:    "repaired",
			step:    &fakeStep{observations: []Observation{{Verdict: VerdictRepair}, {Verdict: VerdictDone}}},
			state:   stateDone,
			repairs: 1,
		},
		{
			name:    "a repair that leaves it undone fails rather than looping",
			step:    &fakeStep{observations: []Observation{{Verdict: VerdictRepair}}},
			state:   stateFailed,
			repairs: 1,
		},
		{
			name:    "a repair that errors fails",
			step:    &fakeStep{observations: []Observation{{Verdict: VerdictRepair}}, repairErr: errors.New("boom")},
			state:   stateFailed,
			repairs: 1,
		},
		{
			name:    "a declined step is skipped",
			step:    &fakeStep{observations: []Observation{{Verdict: VerdictRepair}}, repairErr: ErrSkipped},
			state:   stateSkipped,
			repairs: 1,
		},
		{
			name:  "a stop is a failure",
			step:  &fakeStep{observations: []Observation{{Verdict: VerdictStop, Finding: &doctor.Finding{Fix: "do it"}}}},
			state: stateFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := runModel(t, tt.step)

			assert.Equal(t, tt.state, m.rows[0].state)
			assert.Equal(t, tt.repairs, tt.step.repairs)
		})
	}
}

// TestRunUnattended covers the plain run stopping at the first step it cannot finish.
func TestRunUnattended(t *testing.T) {
	t.Run("stops at the first step that cannot be made true", func(t *testing.T) {
		var out bytes.Buffer
		later := &fakeStep{observations: []Observation{{Verdict: VerdictDone}}}

		err := RunUnattended(t.Context(), &out, nil, []Step{
			&fakeStep{observations: []Observation{{Verdict: VerdictStop, Detail: "no tmux", Finding: &doctor.Finding{Fix: "install tmux"}}}},
			later,
		})

		require.ErrorIs(t, err, ErrFailed)
		assert.Contains(t, out.String(), "fix: install tmux")
		assert.NotContains(t, out.String(), "✓")
	})

	t.Run("a step that needs an answer names the flag", func(t *testing.T) {
		var out bytes.Buffer

		check := fakeChecker(
			doctor.Finding{Name: doctor.CheckDefaultHarness, Status: doctor.StatusFail},
			ok("claude", ""),
			ok("codex", ""),
		)

		err := RunUnattended(t.Context(), &out, check, []Step{&HarnessStep{Harnesses: []string{"claude", "codex"}}})

		require.ErrorIs(t, err, ErrFailed)
		assert.Contains(t, out.String(), "--harness")
	})
}
