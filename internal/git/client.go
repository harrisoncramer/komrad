package git

import (
	"context"
	"log/slog"

	"komrad/internal/logging"
)

// SSHHostsReader reads the SSH hosts that stand in for a forge's web host, keyed by the SSH host.
type SSHHostsReader func() (map[string]string, error)

// Client is git as a dependency a caller can take.
type Client struct {
	sshHosts SSHHostsReader
}

// NewClientParams holds where a client reads its named SSH hosts.
type NewClientParams struct {
	SSHHosts SSHHostsReader
}

// NewClient returns a client that runs git in whichever directory a call names.
func NewClient(params NewClientParams) *Client {
	return &Client{sshHosts: params.SSHHosts}
}

// ParseRemoteURL takes a git remote apart, swapping a named SSH host for the web host it stands in for.
func (c *Client) ParseRemoteURL(ctx context.Context, raw string) (RemoteURL, error) {
	remote, err := ParseRemoteURL(raw)
	if err != nil {
		return RemoteURL{}, err
	}
	if remote.Scheme != "ssh" || c.sshHosts == nil {
		return remote, nil
	}

	hosts, err := c.sshHosts()
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "could not read the SSH hosts, so none were swapped", logging.Err(err)...)

		return remote, nil
	}
	if web, ok := hosts[remote.Host]; ok {
		remote.Host = web
		remote.Port = ""
	}

	return remote, nil
}
