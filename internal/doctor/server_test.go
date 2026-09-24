package doctor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func serverEnv(status ServerStatus) Env {
	return withPanel(status, false)
}

func withPanel(status ServerStatus, running bool) Env {
	return Env{
		Settings: Settings{
			Addr:       "127.0.0.1:8787",
			Version:    "v1.2.3",
			TmuxSocket: "weave",
		},
		ProbeServer:    func(context.Context) (ServerStatus, error) { return status, nil },
		IsPanelRunning: func(context.Context) bool { return running },
	}
}

// TestServerCheck covers who can be on the address: three of these four end as the same
// startup error in the panel, and the address being held by a stranger is the one that
// leaves somebody with no idea what happened.
func TestServerCheck(t *testing.T) {
	tests := []struct {
		name     string
		status   ServerStatus
		want     Status
		wantCode string
	}{
		{
			name:     "nothing there yet",
			status:   ServerStatus{},
			want:     StatusWarn,
			wantCode: "server_down",
		},
		{
			name:     "something that is not weave",
			status:   ServerStatus{Answering: true},
			want:     StatusFail,
			wantCode: "address_taken",
		},
		{
			name: "a weave from another build",
			status: ServerStatus{
				Answering: true,
				Ready:     true,
				Version:   "v1.0.0",
			},
			want:     StatusFail,
			wantCode: "server_stale",
		},
		{
			name: "this build, listening but unable to reach its database",
			status: ServerStatus{
				Answering: true,
				Version:   "v1.2.3",
				Current:   true,
			},
			want:     StatusFail,
			wantCode: "server_not_ready",
		},
		{
			name: "this build, serving",
			status: ServerStatus{
				Answering: true,
				Ready:     true,
				Version:   "v1.2.3",
				Current:   true,
			},
			want: StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := newServerCheck().Run(t.Context(), serverEnv(tt.status))

			assert.Equal(t, tt.want, result.Status)
			assert.Equal(t, tt.wantCode, codeOf(result.Err))
		})
	}
}

// TestServerDownIsNotAProblem covers the reading that keeps a fresh install from looking
// broken: the panel spawns the server the first time it opens, so nothing serving before
// then is the expected state.
func TestServerDownIsNotAProblem(t *testing.T) {
	report := Run(t.Context(), serverEnv(ServerStatus{}), []Check{newServerCheck()})

	assert.True(t, report.Summary.Healthy)
	assert.Equal(t, 1, report.Summary.Warn)
}

// TestStaleServerNamesBothBuilds covers that the message carries the two versions.
func TestStaleServerNamesBothBuilds(t *testing.T) {
	result := newServerCheck().Run(t.Context(), serverEnv(ServerStatus{
		Answering: true,
		Ready:     true,
		Version:   "v1.0.0",
	}))

	assert.Contains(t, result.Err.Error(), "v1.0.0")
	assert.Contains(t, result.Err.Error(), "v1.2.3")
}

// TestStaleServerTellsApartTwoDevelBuilds covers the message a worktree build gets, where the
// version is devel on both sides and only the commit says they differ.
func TestStaleServerTellsApartTwoDevelBuilds(t *testing.T) {
	env := serverEnv(ServerStatus{
		Answering: true,
		Ready:     true,
		Version:   "devel",
		Commit:    "4b1db3cfa7f3eb6f9f5f4de5b4ba5f5e12f5a0c1",
	})
	env.Version = "devel"
	env.Commit = "6da018be0403648b090dfa7695d160efea180b49"

	result := newServerCheck().Run(t.Context(), env)

	assert.Contains(t, result.Err.Error(), "4b1db3c")
	assert.Contains(t, result.Err.Error(), "6da018b")
}

// TestStaleServerFixDependsOnThePanel covers advice that would strand a live panel: the
// panel calls ensureServer only once, when its tmux session is created, so telling somebody
// with a live panel to stop the server leaves nothing that will ever start one again.
func TestStaleServerFixDependsOnThePanel(t *testing.T) {
	stale := ServerStatus{
		Answering: true,
		Ready:     true,
		Version:   "v1.0.0",
	}

	t.Run("no panel running", func(t *testing.T) {
		result := newServerCheck().Run(t.Context(), withPanel(stale, false))

		assert.Equal(t, "weave stop, then reopen the panel", result.Fix,
			"the next open creates the session and spawns the current build")
	})

	t.Run("a panel is running", func(t *testing.T) {
		result := newServerCheck().Run(t.Context(), withPanel(stale, true))

		assert.NotContains(t, result.Fix, "weave stop",
			"stopping the server under a live panel strands it, since nothing re-runs ensureServer")
		assert.Contains(t, result.Fix, "kill-server")
		assert.Contains(t, result.Fix, "every agent",
			"agents are windows in the panel's own session, so the cost is named up front")
	})
}

// TestNotReadyWaitsRatherThanRestarts covers that a database coming back needs no
// intervention: the pool reconnects, so the server recovers and the panel clears itself.
func TestNotReadyWaitsRatherThanRestarts(t *testing.T) {
	result := newServerCheck().Run(t.Context(), withPanel(ServerStatus{
		Answering: true,
		Version:   "v1.2.3",
		Current:   true,
	}, true))

	assert.Equal(t, "server_not_ready", codeOf(result.Err))
	assert.NotContains(t, result.Fix, "stop")
	assert.NotContains(t, result.Fix, "kill")
}

// TestListenAddressIsCheckedFirst covers WEAVE_ADDR itself: every other branch of this
// check reads a probe, and a probe against an address nothing can bind answers the same way
// as one nobody has started yet, so an unbindable address would otherwise read as the
// ordinary state of a fresh machine.
func TestListenAddressIsCheckedFirst(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		want     Status
		wantCode string
	}{
		{
			name:     "a host with no port at all",
			addr:     "localhost",
			want:     StatusFail,
			wantCode: "address_unusable",
		},
		{
			name:     "a port above the ones there are",
			addr:     "127.0.0.1:99999",
			want:     StatusFail,
			wantCode: "address_unusable",
		},
		{
			name:     "port zero, which binds whatever is free and tells nobody which",
			addr:     "127.0.0.1:0",
			want:     StatusFail,
			wantCode: "address_unusable",
		},
		{
			name:     "a port that is not a number",
			addr:     "127.0.0.1:http-ish",
			want:     StatusFail,
			wantCode: "address_unusable",
		},
		{
			name: "an ordinary loopback address",
			addr: "127.0.0.1:8787",
			want: StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := serverEnv(ServerStatus{
				Answering: true,
				Ready:     true,
				Version:   "v1.2.3",
				Current:   true,
			})
			env.Addr = tt.addr

			result := newServerCheck().Run(t.Context(), env)

			assert.Equal(t, tt.want, result.Status)
			assert.Equal(t, tt.wantCode, codeOf(result.Err))
		})
	}
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
			want:          "build weave from 4b1db3c, or put this local build on the engine",
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
