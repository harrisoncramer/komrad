package doctor

import (
	"context"
	"errors"
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
		name          string
		isRemote      bool
		engineVersion string
		engineCommit  string
		want          string
	}{
		{
			name:          "the engine was deployed from a dirty tree, so it names no commit to build from",
			isRemote:      true,
			engineVersion: "v0.0.0-engine.4b1db3cf+dirty.131311",
			want:          "use the dist/weave-panel that deploy built, since it has no commit to build from",
		},
		{
			name:          "the engine was deployed by hand from a commit",
			isRemote:      true,
			engineVersion: "v0.0.0-engine.4b1db3cf",
			engineCommit:  "4b1db3cfa7f3eb6f9f5f4de5b4ba5f5e12f5a0c1",
			want:          "build weave from 4b1db3c, or put this build on the engine with mise run remote:deploy",
		},
		{
			name:          "the engine runs a release, which is installed rather than built",
			isRemote:      true,
			engineVersion: "v0.4.0",
			engineCommit:  "4b1db3cfa7f3eb6f9f5f4de5b4ba5f5e12f5a0c1",
			want:          "install weave v0.4.0, which the engine runs",
		},
		{
			name:          "the engine is this machine, so its server is restarted here",
			isRemote:      false,
			engineVersion: "v0.4.0",
			want:          "tmux -L weave kill-server, then reopen the panel, which also restarts every agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := Env{
				Settings:       Settings{TmuxSocket: "weave", EngineIsRemote: tt.isRemote},
				IsPanelRunning: func(context.Context) bool { return true },
			}

			assert.Equal(t, tt.want, describeStaleServerFix(t.Context(), env, tt.engineVersion, tt.engineCommit))
		})
	}
}

// TestServerCheckOnARemoteEngine covers the arms whose wording and advice only make sense about
// the machine holding the service, which for a remote engine is not this one.
func TestServerCheckOnARemoteEngine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     ServerStatus
		probeErr   error
		wantStatus Status
		wantCode   string
		wantFix    string
	}{
		{
			name:       "the forward never came up, so nothing was learned about the engine",
			probeErr:   errors.New("ssh: connect to host 203.0.113.4 port 22: Connection refused"),
			wantStatus: StatusFail,
			wantCode:   "engine_unreachable",
			wantFix:    "check the engine is up and that this engine's ssh settings reach it",
		},
		{
			name:       "the engine is reachable but serving nothing",
			status:     ServerStatus{},
			wantStatus: StatusFail,
			wantCode:   "server_down",
			wantFix:    "start weave on the engine, since its service runs whether or not a panel is open",
		},
		{
			name:       "something on the engine holds the address",
			status:     ServerStatus{Answering: true},
			wantStatus: StatusFail,
			wantCode:   "address_taken",
			wantFix:    "free the address on the engine, since what holds it is a process there",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := Env{
				Settings: Settings{
					Addr:           "127.0.0.1:8787",
					Version:        "v1.2.3",
					TmuxSocket:     "weave",
					EngineIsRemote: true,
				},
				ProbeServer: func(context.Context) (ServerStatus, error) {
					return tt.status, tt.probeErr
				},
				IsPanelRunning: func(context.Context) bool { return false },
			}

			result := newServerCheck().Run(t.Context(), env)

			assert.Equal(t, tt.wantStatus, result.Status)
			assert.Equal(t, tt.wantCode, codeOf(result.Err))
			assert.Equal(t, tt.wantFix, result.Fix)
		})
	}
}
