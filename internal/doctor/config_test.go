package doctor

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"komrade/config"
	"komrade/lib/xslice"

	"github.com/stretchr/testify/assert"
)

var claudeAndCodex = []HarnessInfo{
	{ID: "claude", Binary: "claude"},
	{ID: "codex", Binary: "codex"},
}

func configEnv(cfg *config.Config, loadErr error, exists bool, installed ...string) Env {
	return Env{
		Settings: Settings{
			configLayers: []config.Layer{{Path: "/home/u/.config/komrade/config.toml"}},
			Harnesses:    claudeAndCodex,
		},
		LookPath: func(name string) (string, error) {
			for _, have := range installed {
				if have == name {
					return "/usr/local/bin/" + name, nil
				}
			}

			return "", exec.ErrNotFound
		},
		Output:         func(context.Context, string, ...string) (string, error) { return "", errors.New("no server") },
		LoadConfig:     func() (*config.Config, error) { return cfg, loadErr },
		DoesLayerExist: func(string) bool { return exists },
	}
}

// TestConfigCheck covers that having no config file is a correct install rather than a
// finding, and that a file which will not parse is the failure it is.
func TestConfigCheck(t *testing.T) {
	t.Run("no file at all", func(t *testing.T) {
		result := newConfigCheck().Run(t.Context(), configEnv(config.Defaults(), nil, false))

		assert.Equal(t, StatusOK, result.Status, "the default install has no config file")
		assert.Contains(t, result.Detail, "(absent)")
	})

	t.Run("a file that parses", func(t *testing.T) {
		result := newConfigCheck().Run(t.Context(), configEnv(config.Defaults(), nil, true))

		assert.Equal(t, StatusOK, result.Status)
		assert.Equal(t, "/home/u/.config/komrade/config.toml", result.Detail,
			"the path is reported, since a config problem is not actionable without knowing which file")
		assert.NotContains(t, result.Detail, "(absent)", "only a file that is missing is marked")
	})

	t.Run("a file that does not parse", func(t *testing.T) {
		broken := errors.New(`failed to parse config: 'keybindings' has invalid keys: list`)
		result := newConfigCheck().Run(t.Context(), configEnv(nil, broken, true))

		assert.Equal(t, StatusFail, result.Status)
		assert.Equal(t, "config_unreadable", codeOf(result.Err))
		assert.Contains(t, result.Err.Error(), "invalid keys",
			"what the parser objected to is carried, since that is the only part naming the typo")
		assert.Contains(t, result.Fix, "fix or delete")
	})

	t.Run("a file that was named but never written", func(t *testing.T) {
		missing := errors.New("failed to read config /home/u/.config/komrade/config.toml: there is no file there")
		result := newConfigCheck().Run(t.Context(), configEnv(nil, missing, false))

		assert.Equal(t, StatusFail, result.Status)
		assert.NotContains(t, result.Fix, "fix or delete",
			"editing a path with nothing at it sends the user looking for a file they never wrote")
		assert.Contains(t, result.Fix, "--config")
	})
}

// TestDefaultHarnessCheck covers the misspelling the panel only finds when somebody tries
// to create an agent.
func TestDefaultHarnessCheck(t *testing.T) {
	tests := []struct {
		name     string
		harness  string
		want     Status
		wantCode string
	}{
		{
			name:     "one this build does not have",
			harness:  "aider",
			want:     StatusFail,
			wantCode: "harness_unknown",
		},
		{
			name:     "a near miss on one it does",
			harness:  "Claude",
			want:     StatusFail,
			wantCode: "harness_unknown",
		},
		{
			name:    "a harness this build has",
			harness: "claude",
			want:    StatusOK,
		},
		{
			name:    "the other one it has",
			harness: "codex",
			want:    StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.DefaultHarness = tt.harness

			result := newDefaultHarnessCheck().Run(t.Context(), configEnv(cfg, nil, true))

			assert.Equal(t, tt.want, result.Status)
			assert.Equal(t, tt.wantCode, codeOf(result.Err))
			if tt.want == StatusFail {
				assert.Contains(t, result.Err.Error(), "claude or codex",
					"the choices are listed, since the fix is picking one of them")
			}
		})
	}
}

// TestHarnessBinaryCheck covers the quietest failure komrade has: a missing CLI leaves the
// pane idle, which reads as an agent that finished.
func TestHarnessBinaryCheck(t *testing.T) {
	tests := []struct {
		name       string
		agent      HarnessInfo
		configured string
		installed  []string
		want       Status
		wantCode   string
	}{
		{
			name:       "the default one is missing",
			agent:      claudeAndCodex[0],
			configured: "claude",
			installed:  []string{},
			want:       StatusFail,
			wantCode:   "harness_missing",
		},
		{
			name:       "a harness nobody is defaulting to is missing",
			agent:      claudeAndCodex[1],
			configured: "claude",
			installed:  []string{"claude"},
			want:       StatusWarn,
			wantCode:   "harness_missing",
		},
		{
			name:       "the same harness once it is the default",
			agent:      claudeAndCodex[1],
			configured: "codex",
			installed:  []string{"claude"},
			want:       StatusFail,
			wantCode:   "harness_missing",
		},
		{
			name:       "installed",
			agent:      claudeAndCodex[0],
			configured: "claude",
			installed:  []string{"claude"},
			want:       StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.DefaultHarness = tt.configured

			result := newHarnessBinaryCheck(tt.agent).Run(t.Context(), configEnv(cfg, nil, true, tt.installed...))

			assert.Equal(t, tt.want, result.Status)
			assert.Equal(t, tt.wantCode, codeOf(result.Err))
		})
	}
}

// TestHarnessChecksFollowTheRegistry covers that the report gains a row when the build gains an
// agent CLI.
func TestHarnessChecksFollowTheRegistry(t *testing.T) {
	checks := Checks(Settings{Harnesses: []HarnessInfo{
		{ID: "claude", Binary: "claude"},
		{ID: "codex", Binary: "codex"},
		{ID: "aider", Binary: "aider"},
	}})

	names := xslice.Map(checks, func(check Check) string {
		return check.Name
	})

	assert.Contains(t, names, "claude")
	assert.Contains(t, names, "codex")
	assert.Contains(t, names, "aider")
}
