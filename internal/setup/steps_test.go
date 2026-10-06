package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"komrad/config"
	"komrad/internal/doctor"
	"komrad/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeChecker(findings ...doctor.Finding) checker {
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
		verdict  verdict
	}{
		{
			name:     "default installed",
			findings: []doctor.Finding{ok(doctor.CheckDefaultHarness, "claude"), ok("claude", ""), ok("codex", "")},
			verdict:  verdictDone,
		},
		{
			name: "default missing and one other installed still asks",
			findings: []doctor.Finding{
				ok(doctor.CheckDefaultHarness, "claude"),
				{Name: "claude", Status: doctor.StatusFail},
				ok("codex", ""),
			},
			verdict: verdictAsk,
		},
		{
			name: "an unknown default with both installed asks",
			findings: []doctor.Finding{
				{Name: doctor.CheckDefaultHarness, Status: doctor.StatusFail},
				ok("claude", ""),
				ok("codex", ""),
			},
			verdict: verdictAsk,
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
			verdict: verdictStop,
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

	require.ErrorIs(t, err, errNeedsAnswer)
	assert.ErrorContains(t, err, "--harness")
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
	assert.ErrorContains(t, err, "a later config layer")
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
		assert.Equal(t, verdictAsk, step.observe(t.Context(), check).Verdict)

		require.ErrorIs(t, step.repair(t.Context()), errSkipped)
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

		assert.Equal(t, verdictDone, step.observe(t.Context(), check).Verdict)
	})
}

// TestAutoSetupStep covers that only a yes writes auto_setup.
func TestAutoSetupStep(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte("default_harness = \"claude\"\n"), 0o600))
	stack := config.Stack{Paths: []string{configPath}}
	check := fakeChecker(ok(doctor.CheckConfig, ""))

	t.Run("a no writes nothing", func(t *testing.T) {
		step := &AutoSetupStep{Path: configPath, Stack: stack}
		assert.Equal(t, verdictAsk, step.observe(t.Context(), check).Verdict)

		require.ErrorIs(t, step.repair(t.Context()), errSkipped)

		written, err := os.ReadFile(configPath)
		require.NoError(t, err)
		assert.Equal(t, "default_harness = \"claude\"\n", string(written))
	})

	t.Run("a yes turns it on", func(t *testing.T) {
		step := &AutoSetupStep{
			Path:   configPath,
			Stack:  stack,
			chosen: true,
		}

		require.NoError(t, step.repair(t.Context()))

		written, err := os.ReadFile(configPath)
		require.NoError(t, err)
		assert.Equal(t, "default_harness = \"claude\"\nauto_setup = true\n", string(written))
		assert.Equal(t, verdictDone, step.observe(t.Context(), check).Verdict)
	})
}

// TestForgeStep covers when the forge step is done, asks, or skips.
func TestForgeStep(t *testing.T) {
	engineDown := doctor.Finding{Name: doctor.CheckServer, Status: doctor.StatusWarn}
	unreadable := doctor.Finding{Name: doctor.CheckForges, Status: doctor.StatusWarn, Code: doctor.CodeForgesUnreadable}
	noForges := doctor.Finding{Name: doctor.CheckForges, Status: doctor.StatusWarn, Code: doctor.CodeNoForges}

	tests := []struct {
		name     string
		findings []doctor.Finding
		verdict  verdict
		fix      string
	}{
		{
			name:     "a local engine that is not running skips",
			findings: []doctor.Finding{engineDown, unreadable},
			verdict:  verdictSkip,
		},
		{
			name: "a remote engine that cannot be reached skips with its fix",
			findings: []doctor.Finding{
				{
					Name:   doctor.CheckServer,
					Status: doctor.StatusFail,
					Error:  "the engine cannot be reached",
					Fix:    "start the forward",
				},
				{Name: doctor.CheckForges, Status: doctor.StatusSkip},
			},
			verdict: verdictSkip,
			fix:     "start the forward",
		},
		{
			name:     "an engine that cannot list its forges skips",
			findings: []doctor.Finding{ok(doctor.CheckServer, ""), unreadable},
			verdict:  verdictSkip,
		},
		{
			name:     "a connected forge is done",
			findings: []doctor.Finding{ok(doctor.CheckServer, ""), ok(doctor.CheckForges, "github.com as octocat")},
			verdict:  verdictDone,
		},
		{
			name:     "no forge asks",
			findings: []doctor.Finding{ok(doctor.CheckServer, ""), noForges},
			verdict:  verdictAsk,
		},
		{
			name: "a refused forge skips with its reconnect command",
			findings: []doctor.Finding{
				ok(doctor.CheckServer, ""),
				{
					Name:   doctor.CheckForges,
					Status: doctor.StatusWarn,
					Code:   "forge_refused",
					Fix:    "gh auth token --hostname github.com | komrad forge connect github.com",
				},
			},
			verdict: verdictSkip,
			fix:     "gh auth token --hostname github.com | komrad forge connect github.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &ForgeStep{GHToken: func(context.Context) forge.Secret { return forge.Secret{} }}

			observation := step.observe(t.Context(), fakeChecker(tt.findings...))

			assert.Equal(t, tt.verdict, observation.Verdict)
			if tt.fix != "" {
				require.NotNil(t, observation.Finding)
				assert.Equal(t, tt.fix, observation.Finding.Fix)
			}
		})
	}
}

// TestForgeStepUnattended covers which answer each pair of flags gives and what it connects.
func TestForgeStepUnattended(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("glpat-pasted\n"), 0o600))

	tests := []struct {
		name      string
		flag      string
		tokenFile string
		ghToken   string
		err       string
		host      string
		token     string
	}{
		{
			name: "no flag skips",
		},
		{
			name: "another host points at forge connect",
			flag: "gitlab.example.com",
			err:  "komrad forge connect",
		},
		{
			name: "gitlab.com without a token file names the flag",
			flag: "gitlab.com",
			err:  "--forge-token-file",
		},
		{
			name: "github.com without gh's token names the flag",
			flag: "github.com",
			err:  "--forge-token-file",
		},
		{
			name:    "github.com takes gh's token",
			flag:    "github.com",
			ghToken: "gho_from_gh",
			host:    "github.com",
			token:   "gho_from_gh",
		},
		{
			name:      "a token file wins over gh's token",
			flag:      "github.com",
			tokenFile: tokenFile,
			ghToken:   "gho_from_gh",
			host:      "github.com",
			token:     "glpat-pasted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var host, token string
			step := &ForgeStep{
				Connect: func(_ context.Context, h string, s forge.Secret) error {
					host, token = h, s.Expose()

					return nil
				},
				GHToken:   func(context.Context) forge.Secret { return forge.NewSecret(tt.ghToken) },
				Flag:      tt.flag,
				TokenFile: tt.tokenFile,
			}
			step.observe(t.Context(), fakeChecker(
				ok(doctor.CheckServer, ""),
				doctor.Finding{Name: doctor.CheckForges, Status: doctor.StatusWarn, Code: doctor.CodeNoForges},
			))

			err := step.answerUnattended()
			if tt.err != "" {
				require.ErrorIs(t, err, errNeedsAnswer)
				assert.ErrorContains(t, err, tt.err)

				return
			}
			require.NoError(t, err)

			err = step.repair(t.Context())
			if tt.host == "" {
				require.ErrorIs(t, err, errSkipped)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.host, host)
			assert.Equal(t, tt.token, token)
		})
	}
}
