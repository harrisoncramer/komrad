package doctor

import (
	"context"
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
		ProbeServer:    func(context.Context, string) ServerStatus { return status },
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
		Answering: true, Ready: true, Version: "v1.0.0",
	}))

	assert.Contains(t, result.Err.Error(), "v1.0.0")
	assert.Contains(t, result.Err.Error(), "v1.2.3")
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
		Answering: true, Version: "v1.2.3", Current: true,
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
