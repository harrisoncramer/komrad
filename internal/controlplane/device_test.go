package controlplane_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"komrad/internal/controlplane"
	"komrad/internal/controlplanetest"
	"komrad/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// call posts to one of the control plane's endpoints with an ID token, answering the status
// and the decoded body.
func (p *plane) call(t *testing.T, path, token string, answer any) int {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, p.endpoints.URL+path, nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := p.endpoints.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()

	require.NoError(t, json.NewDecoder(res.Body).Decode(answer))

	return res.StatusCode
}

// TestTheDeviceFlowAdmitsOnlyThoseHoldingTheEnginesGrant covers who may start an engine's
// device flow.
func TestTheDeviceFlowAdmitsOnlyThoseHoldingTheEnginesGrant(t *testing.T) {
	p := startPlane(t)

	tests := []struct {
		name        string
		path        string
		token       string
		wantStatus  int
		wantMessage string
	}{
		{
			name:        "an engine the control plane does not serve",
			path:        "/" + p.engine("mallory").name + controlplane.DevicePath,
			token:       harryToken,
			wantStatus:  http.StatusNotFound,
			wantMessage: "the control plane has no engine by that name",
		},
		{
			name:        "no ID token",
			path:        "/" + p.engine("harry").name + controlplane.DevicePath,
			wantStatus:  http.StatusUnauthorized,
			wantMessage: "the request carries no bearer token, so run komrad login",
		},
		{
			name:       "an ID token the provider did not sign",
			path:       "/" + p.engine("harry").name + controlplane.DevicePollPath,
			token:      "id-token-forged",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "a provider the control plane cannot reach",
			path:       "/" + p.engine("harry").name + controlplane.DevicePath,
			token:      downToken,
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:        "someone without the engine's grant",
			path:        "/" + p.engine("harry").name + controlplane.DevicePath,
			token:       adaToken,
			wantStatus:  http.StatusForbidden,
			wantMessage: "nobody has granted this account access to the engine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var answer struct {
				Message string `json:"message"`
			}
			assert.Equal(t, tt.wantStatus, p.call(t, tt.path, tt.token, &answer))
			if tt.wantMessage != "" {
				assert.Equal(t, tt.wantMessage, answer.Message)
			}
			assert.Empty(t, p.github.Requests(), "nothing reaches GitHub")
		})
	}
}

// TestTheDeviceFlowKeepsTheLoginThePersonAuthorized covers each answer a poll can give, from
// the code GitHub hands out to the login the forge proxy then sends.
func TestTheDeviceFlowKeepsTheLoginThePersonAuthorized(t *testing.T) {
	tests := []struct {
		name string
		// github sets what GitHub answers the poll with.
		github     func(github *controlplanetest.GitHub)
		elapsed    time.Duration
		wantPoll   controlplane.DevicePoll
		wantLogin  string
		skipsStart bool
	}{
		{
			name:     "the person has not entered the code yet",
			github:   func(*controlplanetest.GitHub) {},
			wantPoll: controlplane.DevicePoll{Status: controlplane.DeviceStatusPending, Interval: 5},
		},
		{
			name:     "GitHub asks the poller to slow down",
			github:   func(github *controlplanetest.GitHub) { github.Refuse("slow_down", 10) },
			wantPoll: controlplane.DevicePoll{Status: controlplane.DeviceStatusPending, Interval: 10},
		},
		{
			name:      "the person entered the code",
			github:    func(github *controlplanetest.GitHub) { github.Authorize("gho_grace") },
			wantPoll:  controlplane.DevicePoll{Status: controlplane.DeviceStatusAuthorized},
			wantLogin: "gho_grace",
		},
		{
			name:     "the person turned it down",
			github:   func(github *controlplanetest.GitHub) { github.Refuse("access_denied", 0) },
			wantPoll: controlplane.DevicePoll{Status: controlplane.DeviceStatusDenied},
		},
		{
			name:     "GitHub says the code ran out",
			github:   func(github *controlplanetest.GitHub) { github.Refuse("expired_token", 0) },
			wantPoll: controlplane.DevicePoll{Status: controlplane.DeviceStatusExpired},
		},
		{
			name:     "the code ran out before anyone polled",
			github:   func(github *controlplanetest.GitHub) { github.Authorize("gho_grace") },
			elapsed:  15 * time.Minute,
			wantPoll: controlplane.DevicePoll{Status: controlplane.DeviceStatusExpired},
		},
		{
			name:       "a poll with no code handed out",
			github:     func(github *controlplanetest.GitHub) { github.Authorize("gho_grace") },
			skipsStart: true,
			wantPoll:   controlplane.DevicePoll{Status: controlplane.DeviceStatusExpired},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := startPlane(t)

			if !tt.skipsStart {
				var code controlplane.DeviceCode
				require.Equal(t, http.StatusOK, p.call(t, "/"+p.engine("grace").name+controlplane.DevicePath, harryToken, &code))
				assert.Equal(t, controlplane.DeviceCode{
					UserCode:        controlplanetest.UserCode,
					VerificationURI: controlplanetest.VerificationURI,
					Interval:        5,
					ExpiresIn:       900,
				}, code)
			}

			tt.github(p.github)
			p.clock.advance(tt.elapsed)

			var poll controlplane.DevicePoll
			require.Equal(t, http.StatusOK, p.call(t, "/"+p.engine("grace").name+controlplane.DevicePollPath, harryToken, &poll))
			assert.Equal(t, tt.wantPoll, poll)

			login, err := p.logins.Get(t.Context(), p.engine("grace").name)
			if tt.wantLogin == "" {
				require.ErrorIs(t, err, forge.ErrNoCredential)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantLogin, login.Expose())
		})
	}
}
