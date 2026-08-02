package doctor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// serverEnv answers for whoever holds the listen address.
func serverEnv(status ServerStatus) Env {
	return Env{
		Settings:    Settings{Addr: "127.0.0.1:8787", Version: "v1.2.3"},
		ProbeServer: func(context.Context, string) ServerStatus { return status },
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
			name:   "this build, serving",
			status: ServerStatus{Answering: true, Ready: true, Version: "v1.2.3", Current: true},
			want:   StatusOK,
		},
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
