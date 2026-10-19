package cli

import (
	"context"
	"net/http"

	"komrad/internal/controlplane"
	"komrad/internal/ui"
)

// panelControlPlane is the control plane in front of the panel's engine at a url.
type panelControlPlane struct {
	url    string
	client *http.Client
}

func (p *panelControlPlane) StartEngine(ctx context.Context) error {
	var accepted struct {
		Message string `json:"message"`
	}

	return postControlPlane(ctx, p.client, p.url+controlplane.StartPath, &accepted)
}

func (p *panelControlPlane) IsGitHubAuthorized(ctx context.Context) (bool, error) {
	var status controlplane.GitHubStatus
	if err := getControlPlane(ctx, p.client, p.url+controlplane.GitHubPath, &status); err != nil {
		return false, err
	}

	return status.Authorized, nil
}

func (p *panelControlPlane) StartGitHub(ctx context.Context) (*ui.GitHubCode, error) {
	var code controlplane.DeviceCode
	if err := postControlPlane(ctx, p.client, p.url+controlplane.DevicePath, &code); err != nil {
		return nil, err
	}

	return &ui.GitHubCode{
		UserCode:        code.UserCode,
		VerificationURI: code.VerificationURI,
		Interval:        pollInterval(code.Interval),
	}, nil
}

func (p *panelControlPlane) PollGitHub(ctx context.Context) (*ui.GitHubPoll, error) {
	var poll controlplane.DevicePoll
	if err := postControlPlane(ctx, p.client, p.url+controlplane.DevicePollPath, &poll); err != nil {
		return nil, err
	}

	isAuthorized, err := readDevicePoll(poll)
	if err != nil {
		return nil, err
	}

	return &ui.GitHubPoll{IsAuthorized: isAuthorized, Interval: pollInterval(poll.Interval)}, nil
}
