package git

// NewClientParams names the SSH hosts that stand in for a forge's web host, keyed by the SSH host.
type NewClientParams struct {
	SSHHosts map[string]string
}

// Client is git as a dependency a caller can take.
type Client struct {
	sshHosts map[string]string
}

// NewClient returns a client that runs git in whichever directory a call names.
func NewClient(params NewClientParams) *Client {
	return &Client{sshHosts: params.SSHHosts}
}
