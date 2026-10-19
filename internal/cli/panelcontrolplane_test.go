package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"komrad/internal/controlplane"
	"komrad/internal/ui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestThePanelReadsGitHubsDeviceFlowFromTheControlPlane covers how the panel hears each poll of
// GitHub's device flow, by the status the control plane answers.
func TestThePanelReadsGitHubsDeviceFlowFromTheControlPlane(t *testing.T) {
	tests := []struct {
		name     string
		poll     controlplane.DevicePoll
		wantPoll *ui.GitHubPoll
		wantErr  string
	}{
		{
			name:     "a code nobody has entered yet",
			poll:     controlplane.DevicePoll{Status: controlplane.DeviceStatusPending, Interval: 10},
			wantPoll: &ui.GitHubPoll{Interval: 10 * time.Second},
		},
		{
			name:     "a code GitHub names no interval for",
			poll:     controlplane.DevicePoll{Status: controlplane.DeviceStatusPending},
			wantPoll: &ui.GitHubPoll{Interval: 5 * time.Second},
		},
		{
			name:     "an authorized code",
			poll:     controlplane.DevicePoll{Status: controlplane.DeviceStatusAuthorized},
			wantPoll: &ui.GitHubPoll{IsAuthorized: true, Interval: 5 * time.Second},
		},
		{
			name:    "a code that ran out",
			poll:    controlplane.DevicePoll{Status: controlplane.DeviceStatusExpired},
			wantErr: "the code ran out before it was entered",
		},
		{
			name:    "an authorization turned down",
			poll:    controlplane.DevicePoll{Status: controlplane.DeviceStatusDenied},
			wantErr: "the authorization was turned down at GitHub",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			served := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/harry"+controlplane.DevicePollPath, r.URL.Path)
				_ = json.NewEncoder(w).Encode(tt.poll)
			}))
			t.Cleanup(served.Close)

			plane := &panelControlPlane{url: served.URL + "/harry", client: served.Client()}
			poll, err := plane.PollGitHub(t.Context())
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantPoll, poll)
		})
	}
}
