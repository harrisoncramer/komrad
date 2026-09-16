package doctor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChecksAboutTheEngineAreSkippedWhenItIsElsewhere covers the checks that describe the engine,
// since answering them from this machine reports the wrong box as though it were the right one.
func TestChecksAboutTheEngineAreSkippedWhenItIsElsewhere(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		engineSide bool
		isRemote   bool
		wantStatus Status
		wantDetail string
	}{
		{
			name:       "a check about the engine is not answered from here",
			engineSide: true,
			isRemote:   true,
			wantStatus: StatusSkip,
			wantDetail: "not checked, the engine is on another machine",
		},
		{
			name:       "a check about the engine is answered when the engine is this machine",
			engineSide: true,
			isRemote:   false,
			wantStatus: StatusOK,
			wantDetail: "ran",
		},
		{
			name:       "a check about this machine is answered wherever the engine is",
			engineSide: false,
			isRemote:   true,
			wantStatus: StatusOK,
			wantDetail: "ran",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			check := Check{
				Name:        "subject",
				Description: "the check under test",
				Engine:      tt.engineSide,
				Run: func(context.Context, Env) Result {
					return pass("ran")
				},
			}

			env := Env{Settings: Settings{EngineIsRemote: tt.isRemote}}

			report := Run(t.Context(), env, []Check{check})

			require.Len(t, report.Findings, 1)
			assert.Equal(t, tt.wantStatus, report.Findings[0].Status)
			assert.Equal(t, tt.wantDetail, report.Findings[0].Detail)
		})
	}
}

// TestStaleServerFixDependsOnWhereTheEngineIs covers the advice given when the service is a
// different build, since restarting tmux here does nothing to a service on another machine.
func TestStaleServerFixDependsOnWhereTheEngineIs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		isRemote bool
		want     string
	}{
		{
			name:     "the engine is elsewhere, so the build has to be sent to it",
			isRemote: true,
			want:     "deploy this build to the engine, since the service it is running is its own",
		},
		{
			name:     "the engine is this machine, so its server is restarted here",
			isRemote: false,
			want:     "tmux -L weave kill-server, then reopen the panel, which also restarts every agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := Env{
				Settings:       Settings{TmuxSocket: "weave", EngineIsRemote: tt.isRemote},
				IsPanelRunning: func(context.Context) bool { return true },
			}

			assert.Equal(t, tt.want, describeStaleServerFix(t.Context(), env))
		})
	}
}

// TestEngineSideChecksAreTheOnesThatReadTheEngine pins which checks stop being answered, since a
// check marked wrongly either lies about a remote engine or goes silent on a local one.
func TestEngineSideChecksAreTheOnesThatReadTheEngine(t *testing.T) {
	t.Parallel()

	settings := Settings{Harnesses: []HarnessInfo{{ID: "claude", Binary: "claude"}}}

	engineSide := make([]string, 0)

	for _, check := range Checks(settings) {
		if check.Engine {
			engineSide = append(engineSide, check.Name)
		}
	}

	want := []string{
		"default harness",
		"hooks",
		"worktrees",
		"context",
		"database",
		"schema",
		"readonly login",
		"orphaned worktrees",
		"claude",
	}

	assert.Equal(t, want, engineSide)
}
