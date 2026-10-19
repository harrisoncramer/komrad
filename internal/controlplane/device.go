package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"komrad/internal/forge"
)

// githubScopes are what a person's GitHub login reaches: their repositories, their
// organisations' membership and the workflows a push can change.
const githubScopes = "repo read:org workflow"

const deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

// maxDeviceAnswer bounds what is read from GitHub's device flow endpoints.
const maxDeviceAnswer = 64 << 10

// DeviceStatus is where an engine's device flow stands after a poll.
type DeviceStatus string

const (
	DeviceStatusPending    DeviceStatus = "pending"
	DeviceStatusAuthorized DeviceStatus = "authorized"
	DeviceStatusExpired    DeviceStatus = "expired"
	DeviceStatusDenied     DeviceStatus = "denied"
)

// DeviceCode is what a person enters at GitHub to authorize an engine.
type DeviceCode struct {
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	// Interval is how many seconds to wait between polls.
	Interval int `json:"interval"`
	// ExpiresIn is how many seconds the code lasts.
	ExpiresIn int `json:"expires_in"`
}

// DevicePoll is one poll's answer.
type DevicePoll struct {
	Status   DeviceStatus `json:"status"`
	Interval int          `json:"interval"`
}

// pendingFlow is a device code GitHub has handed out for an engine and nobody has finished.
type pendingFlow struct {
	deviceCode string
	interval   int
	expiresAt  time.Time
}

// newDeviceFlowParams names the OAuth app and where GitHub is.
type newDeviceFlowParams struct {
	Client   *http.Client
	GitHub   string
	ClientID string
	Logins   loginStore
	Now      func() time.Time
}

// deviceFlow runs GitHub's device flow for each engine, one pending flow per engine.
type deviceFlow struct {
	client   *http.Client
	github   string
	clientID string
	logins   loginStore
	now      func() time.Time
	mu       sync.Mutex
	pending  map[string]pendingFlow
}

func newDeviceFlow(params newDeviceFlowParams) *deviceFlow {
	return &deviceFlow{
		client:   params.Client,
		github:   strings.TrimSuffix(params.GitHub, "/"),
		clientID: params.ClientID,
		logins:   params.Logins,
		now:      params.Now,
		pending:  make(map[string]pendingFlow),
	}
}

// start asks GitHub for a code, in place of any the engine was waiting on.
func (d *deviceFlow) start(ctx context.Context, engine string) (*DeviceCode, error) {
	var answer struct {
		DeviceCode
		DeviceCodeSecret string `json:"device_code"`
	}
	if err := d.post(ctx, "/login/device/code", url.Values{
		"client_id": {d.clientID},
		"scope":     {githubScopes},
	}, &answer); err != nil {
		return nil, err
	}
	if answer.DeviceCodeSecret == "" || answer.UserCode == "" {
		return nil, fmt.Errorf("GitHub answered a device code request with no code")
	}

	d.mu.Lock()
	d.pending[engine] = pendingFlow{
		deviceCode: answer.DeviceCodeSecret,
		interval:   answer.Interval,
		expiresAt:  d.now().Add(time.Duration(answer.ExpiresIn) * time.Second),
	}
	d.mu.Unlock()

	return &answer.DeviceCode, nil
}

// poll asks GitHub once whether the person has entered the engine's code, keeping the login
// when they have.
func (d *deviceFlow) poll(ctx context.Context, engine string) (*DevicePoll, error) {
	d.mu.Lock()
	flow, found := d.pending[engine]
	d.mu.Unlock()

	if !found || !d.now().Before(flow.expiresAt) {
		d.forget(engine, flow)

		return &DevicePoll{Status: DeviceStatusExpired}, nil
	}

	var answer struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Interval    int    `json:"interval"`
	}
	if err := d.post(ctx, "/login/oauth/access_token", url.Values{
		"client_id":   {d.clientID},
		"device_code": {flow.deviceCode},
		"grant_type":  {deviceGrantType},
	}, &answer); err != nil {
		return nil, err
	}

	switch {
	case answer.AccessToken != "":
		if err := d.logins.Set(ctx, engine, forge.NewSecret(answer.AccessToken)); err != nil {
			return nil, fmt.Errorf("failed to keep the GitHub login for engine %s: %w", engine, err)
		}
		d.forget(engine, flow)

		return &DevicePoll{Status: DeviceStatusAuthorized}, nil
	case answer.Error == "authorization_pending":
		return &DevicePoll{Status: DeviceStatusPending, Interval: flow.interval}, nil
	case answer.Error == "slow_down":
		flow.interval = answer.Interval
		d.mu.Lock()
		if current, ok := d.pending[engine]; ok && current.deviceCode == flow.deviceCode {
			d.pending[engine] = flow
		}
		d.mu.Unlock()

		return &DevicePoll{Status: DeviceStatusPending, Interval: flow.interval}, nil
	case answer.Error == "expired_token":
		d.forget(engine, flow)

		return &DevicePoll{Status: DeviceStatusExpired}, nil
	case answer.Error == "access_denied":
		d.forget(engine, flow)

		return &DevicePoll{Status: DeviceStatusDenied}, nil
	default:
		return nil, fmt.Errorf("GitHub refused the device code poll with %q", answer.Error)
	}
}

// forget drops the engine's flow, unless a newer one replaced it.
func (d *deviceFlow) forget(engine string, flow pendingFlow) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if current, ok := d.pending[engine]; ok && current.deviceCode == flow.deviceCode {
		delete(d.pending, engine)
	}
}

// post sends a form to GitHub and decodes its JSON answer.
func (d *deviceFlow) post(ctx context.Context, path string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.github+path, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("failed to build the request for GitHub's %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach GitHub's %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDeviceAnswer))
	if err != nil {
		return fmt.Errorf("failed to read GitHub's answer from %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub answered %s with %d", path, resp.StatusCode)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("failed to decode GitHub's answer from %s: %w", path, err)
	}

	return nil
}
