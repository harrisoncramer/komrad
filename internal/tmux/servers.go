package tmux

// hostSocketOption records the socket path of the tmux server the user is on. It
// is stored on the manager's own server because the panel and the API server both
// run there, and it is rewritten on every open rather than passed in through their
// environment, which only the open that created the session could set.
const hostSocketOption = "@weave_host_socket"

// Host is the tmux server the user is on. The manager runs on a server of its own,
// so its session never shows up in the user's session list and cannot be attached
// to by accident. That isolation cuts both ways: two things live only on the
// user's server, the working directory of the client viewing the popup and the
// global options the key binding reads, and this is how they are reached.
//
// The socket is looked up per call rather than held, since the panel outlives many
// opens and each one may come from a different server.
type Host struct {
	manager *Client
}

// NewHost returns a Host that finds the user's server through the manager's,
// where its socket path is recorded.
func NewHost(manager *Client) *Host {
	return &Host{manager: manager}
}

// Record stores the socket path of the user's server. The open command calls it
// with the path tmux interpolated into the popup command, so every open points the
// panel at the server that actually opened it.
func (h *Host) Record(path string) {
	if path == "" {
		return
	}

	h.manager.SetGlobal(hostSocketOption, path)
}

// client returns a client for the user's server, nil when no open has recorded
// one. Every method below then does nothing, which is the honest answer for a
// panel running with no popup ever having been attached to it.
func (h *Host) client() *Client {
	path := h.manager.ShowGlobal(hostSocketOption)
	if path == "" {
		return nil
	}

	return NewClientAtPath(path)
}

// ClientPath returns a client's active pane working directory.
func (h *Host) ClientPath(client string) string {
	host := h.client()
	if host == nil {
		return ""
	}

	return host.ClientPath(client)
}

// SetGlobal sets a global tmux option on the user's server.
func (h *Host) SetGlobal(name, value string) {
	if host := h.client(); host != nil {
		host.SetGlobal(name, value)
	}
}

// ShowGlobal returns a global tmux option's value from the user's server, empty
// if it is unset.
func (h *Host) ShowGlobal(name string) string {
	host := h.client()
	if host == nil {
		return ""
	}

	return host.ShowGlobal(name)
}

// Servers is the pair of tmux servers this program uses, presented as the one
// client the rest of the program holds. Sessions, windows, and panes belong to the
// manager's server. Global options and the invoking client's directory belong to
// the user's, since the key binding reads those options and that client is
// attached only there.
type Servers struct {
	*Client
	host *Host
}

// NewServers pairs a client for the manager's server with the user's server.
func NewServers(manager *Client, host *Host) *Servers {
	return &Servers{Client: manager, host: host}
}

// ClientPath asks the user's server, the only one the invoking client exists on.
func (s *Servers) ClientPath(client string) string {
	return s.host.ClientPath(client)
}

// SetGlobal writes on the user's server, where the tmux key binding reads it.
func (s *Servers) SetGlobal(name, value string) {
	s.host.SetGlobal(name, value)
}

// ShowGlobal reads from the user's server, for the same reason.
func (s *Servers) ShowGlobal(name string) string {
	return s.host.ShowGlobal(name)
}
