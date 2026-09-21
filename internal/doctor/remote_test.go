package doctor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// TestStaleServerFixDependsOnWhereTheEngineIs covers the advice given when the service is a
// different build.
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
