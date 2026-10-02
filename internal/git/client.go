package git

// NewClientParams says where a client reads the SSH hosts that stand in for a forge's web host, keyed by the SSH host.
type NewClientParams struct {
	SSHHosts func() (map[string]string, error)
}

// Client is git as a dependency a caller can take.
type Client struct {
	sshHosts func() (map[string]string, error)
}

// NewClient returns a client that runs git in whichever directory a call names.
func NewClient(params NewClientParams) *Client {
	return &Client{sshHosts: params.SSHHosts}
}
