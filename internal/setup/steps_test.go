package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"komrad/config"
	"komrad/internal/doctor"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeChecker(findings ...doctor.Finding) Checker {
	return func(_ context.Context, names ...string) doctor.Report {
		report := doctor.Report{}
		for _, finding := range findings {
			for _, name := range names {
				if finding.Name == name {
					report.Findings = append(report.Findings, finding)
				}
			}
		}

		return report
	}
}

func ok(name, detail string) doctor.Finding {
	return doctor.Finding{
		Name:   name,
		Status: doctor.StatusOK,
		Detail: detail,
	}
}

// TestHarnessStep covers when the harness step is done, repairs alone, asks, or stops.
func TestHarnessStep(t *testing.T) {
	tests := []struct {
		name     string
		findings []doctor.Finding
		verdict  Verdict
	}{
		{
			name:     "default installed",
			findings: []doctor.Finding{ok(doctor.CheckDefaultHarness, "claude"), ok("claude", ""), ok("codex", "")},
			verdict:  VerdictDone,
		},
		{
			name: "default missing and one other installed still asks",
			findings: []doctor.Finding{
				ok(doctor.CheckDefaultHarness, "claude"),
				{Name: "claude", Status: doctor.StatusFail},
				ok("codex", ""),
			},
			verdict: VerdictAsk,
		},
		{
			name: "an unknown default with both installed asks",
			findings: []doctor.Finding{
				{Name: doctor.CheckDefaultHarness, Status: doctor.StatusFail},
				ok("claude", ""),
				ok("codex", ""),
			},
			verdict: VerdictAsk,
		},
		{
			name: "none installed stops",
			findings: []doctor.Finding{
				ok(doctor.CheckDefaultHarness, "claude"),
				{
					Name:   "claude",
					Status: doctor.StatusFail,
					Fix:    "install claude",
				},
				{Name: "codex", Status: doctor.StatusWarn},
			},
			verdict: VerdictStop,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &HarnessStep{Harnesses: []string{"claude", "codex"}}

			observation := step.observe(t.Context(), fakeChecker(tt.findings...))

			assert.Equal(t, tt.verdict, observation.Verdict)
		})
	}
}

// TestHarnessStepUnattendedNamesTheFlag covers that a run with nobody to ask names --harness.
func TestHarnessStepUnattendedNamesTheFlag(t *testing.T) {
	step := &HarnessStep{installed: []string{"claude", "codex"}}

	err := step.answerUnattended()

	require.ErrorIs(t, err, ErrNeedsAnswer)
	assert.Contains(t, err.Error(), "--harness")
}

// TestHarnessStepRepairRefusesAnOverridingLayer covers that a write a later layer overrides is reported rather than passed.
func TestHarnessStepRepairRefusesAnOverridingLayer(t *testing.T) {
	dir := t.TempDir()
	lower, upper := filepath.Join(dir, "lower.toml"), filepath.Join(dir, "upper.toml")
	require.NoError(t, os.WriteFile(upper, []byte("default_harness = \"claude\"\n"), 0o600))

	step := &HarnessStep{
		Path:   lower,
		Stack:  config.Stack{Paths: []string{lower, upper}},
		chosen: "codex",
	}

	err := step.repair(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "a later config layer")
}

// TestContextStep covers that the context step writes an answer and never touches a file already there.
func TestContextStep(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context.md")
	configPath := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte("[context]\nfile = \""+path+"\"\n"), 0o600))
	stack := config.Stack{Paths: []string{configPath}}
	check := fakeChecker(ok(doctor.CheckConfig, ""), ok(doctor.CheckContext, "no standing context"))

	t.Run("an empty answer writes nothing", func(t *testing.T) {
		step := &ContextStep{Stack: stack}
		assert.Equal(t, VerdictAsk, step.observe(t.Context(), check).Verdict)

		require.ErrorIs(t, step.repair(t.Context()), ErrSkipped)
		assert.NoFileExists(t, path)
	})

	t.Run("an answer is written", func(t *testing.T) {
		step := &ContextStep{Stack: stack}
		step.observe(t.Context(), check)
		step.text = "Be terse."

		require.NoError(t, step.repair(t.Context()))

		written, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "Be terse.\n", string(written))
	})

	t.Run("a file already there is left alone", func(t *testing.T) {
		step := &ContextStep{Stack: stack}

		assert.Equal(t, VerdictDone, step.observe(t.Context(), check).Verdict)
	})
}
