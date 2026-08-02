package doctor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// serverEnv answers for whoever holds the listen address, on a machine with no panel up.
func serverEnv(status ServerStatus) Env {
	return withPanel(status, false)
}

// withPanel answers for the address and for whether the panel's tmux session exists, which
// is what decides whether stopping the server repairs anything.
func withPanel(status ServerStatus, running bool) Env {
	return Env{
		Settings: Settings{
			Addr:         "127.0.0.1:8787",
			Version:      "v1.2.3",
			TmuxSocket:   "weave",
			PanelSession: "weave",
		},
		ProbeServer: func(context.Context, string) ServerStatus { return status },
		Output: func(_ context.Context, _ string, args ...string) (string, error) {
			if running {
				return "", nil
			}

			return "", errors.New("no server running on /tmp/tmux-501/weave")
		},
	}
}

// TestServerCheck covers who can be on the address. Three of these four end as the same
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
			name:     "a weave from another build",
			status:   ServerStatus{Answering: true, Ready: true, Version: "v1.0.0"},
			want:     StatusFail,
			wantCode: "server_stale",
		},
		{
			name:     "this build, listening but unable to reach its database",
			status:   ServerStatus{Answering: true, Version: "v1.2.3", Current: true},
			want:     StatusFail,
			wantCode: "server_not_ready",
		},
		{
			name:   "this build, serving",
			status: ServerStatus{Answering: true, Ready: true, Version: "v1.2.3", Current: true},
			want:   StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Server().Run(t.Context(), serverEnv(tt.status))

			assert.Equal(t, tt.want, result.Status)
			assert.Equal(t, tt.wantCode, CodeOf(result.Err))
		})
	}
}

// TestServerDownIsNotAProblem covers the reading that keeps a fresh install from looking
// broken: the panel spawns the server the first time it opens, so nothing serving before
// then is the expected state.
func TestServerDownIsNotAProblem(t *testing.T) {
	report := Run(t.Context(), serverEnv(ServerStatus{}), []Check{Server()})

	assert.True(t, report.Summary.Healthy)
	assert.Equal(t, 1, report.Summary.Warn)
}

// TestStaleServerNamesBothBuilds covers that the message carries the two versions, since
// which one is stale is the whole question and neither is guessable from the other.
func TestStaleServerNamesBothBuilds(t *testing.T) {
	result := Server().Run(t.Context(), serverEnv(ServerStatus{
		Answering: true, Ready: true, Version: "v1.0.0",
	}))

	assert.Contains(t, result.Err.Error(), "v1.0.0")
	assert.Contains(t, result.Err.Error(), "v1.2.3")
}

// TestStaleServerFixDependsOnThePanel covers advice that was actively harmful.
//
// The panel calls ensureServer once, when its tmux session is created. Re-opening the popup
// re-attaches the process already running it, so telling somebody with a live panel to stop
// the server leaves them on the unreachable screen with nothing that will ever start one.
func TestStaleServerFixDependsOnThePanel(t *testing.T) {
	stale := ServerStatus{Answering: true, Ready: true, Version: "v1.0.0"}

	t.Run("no panel running", func(t *testing.T) {
		result := Server().Run(t.Context(), withPanel(stale, false))

		assert.Equal(t, "weave stop, then reopen the panel", result.Fix,
			"the next open creates the session and spawns the current build")
	})

	t.Run("a panel is running", func(t *testing.T) {
		result := Server().Run(t.Context(), withPanel(stale, true))

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
	result := Server().Run(t.Context(), withPanel(ServerStatus{
		Answering: true, Version: "v1.2.3", Current: true,
	}, true))

	assert.Equal(t, "server_not_ready", CodeOf(result.Err))
	assert.NotContains(t, result.Fix, "stop")
	assert.NotContains(t, result.Fix, "kill")
}

// TestListenAddressIsCheckedFirst covers WEAVE_ADDR itself. Every other branch of this
// check reads a probe, and a probe against an address nothing can bind answers the same way
// an address nobody has started yet does, so an unbindable one would report as the ordinary
// state of a fresh machine while the server it describes can never come up.
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
			env := serverEnv(ServerStatus{Answering: true, Ready: true, Version: "v1.2.3", Current: true})
			env.Addr = tt.addr

			result := Server().Run(t.Context(), env)

			assert.Equal(t, tt.want, result.Status)
			assert.Equal(t, tt.wantCode, CodeOf(result.Err))
		})
	}
}
