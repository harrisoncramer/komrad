package tmux

import (
	"errors"
	"strings"
)

// hostSocketOption records the socket path of the tmux server the user is on. It is stored
// on the manager's own server, where the panel and the API server run, and rewritten on
// every open, since only the open that created the session could set an environment.
const hostSocketOption = "@weave_host_socket"

// InvokingClientOption holds the tmux client that opened the current popup. The open
// command writes it on each open, and it is read live, so anything that needs the invoking
// client uses the one actually viewing the popup rather than one fixed when the persistent
// panel first started.
const InvokingClientOption = "@weave_client"

// InfoPaneOption holds the panel's info pane, the one an editor or a shell
// opens in. The panel writes it before running an action, since how the panel laid itself
// out is known nowhere else and a script filling that pane has to be told which it is.
//
// It lives on the manager's own server, where that pane is, rather than beside the options
// the key binding reads on the user's.
const InfoPaneOption = "@weave_info_pane"

// tmuxEnvFields is how many fields tmux writes into TMUX for a pane it spawns: the
// server's socket path, its pid, and the id of the session the pane belongs to.
const tmuxEnvFields = 3

// SocketFromEnv returns the socket path of the server a TMUX value names, empty when it
// names none. It is how the popup finds the user's server, which cannot be passed in: tmux
// expands no format in a popup's command or environment, and an unexpanded one at the
// start of a shell word is a comment, so a value in the key binding never arrives.
func SocketFromEnv(value string) string {
	fields := strings.Split(value, ",")
	if len(fields) != tmuxEnvFields {
		return ""
	}

	return fields[0]
}

// SessionFromEnv returns the session a TMUX value belongs to, as the id target tmux
// reads it by, which unlike a bare name cannot be confused with a window's.
func SessionFromEnv(value string) string {
	fields := strings.Split(value, ",")
	if len(fields) != tmuxEnvFields || fields[tmuxEnvFields-1] == "" {
		return ""
	}

	return "$" + fields[tmuxEnvFields-1]
}

// Host is the tmux server the user is on. The manager runs on a server of its own, so its
// session cannot be attached to by accident, and that isolation cuts both ways: the
// working directory of the client viewing the popup and the global options the key binding
// reads live only on the user's server, and this is how they are reached.
//
// The socket is looked up per call rather than held, since the panel outlives many opens
// and each may come from a different server.
type Host struct {
	manager *Client
}

// NewHost returns a Host that finds the user's server through the manager's, where its
// socket path is recorded.
func NewHost(manager *Client) *Host {
	return &Host{manager: manager}
}

// Record stores the socket path of the user's server, called by the open command with the
// path tmux interpolated, so every open points the panel at the server that opened it.
func (h *Host) Record(path string) {
	if path == "" {
		return
	}

	h.manager.SetGlobal(hostSocketOption, path)
}

// client returns a client for the user's server, nil when no open has recorded one, which
// leaves every method below doing nothing.
func (h *Host) client() *Client {
	path := h.manager.ShowGlobal(hostSocketOption)
	if path == "" {
		return nil
	}

	return NewClientAtPath(path)
}

// GetClientPath returns a client's active pane working directory.
func (h *Host) GetClientPath(client string) string {
	host := h.client()
	if host == nil {
		return ""
	}

	return host.GetClientPath(client)
}

// SetGlobal sets a global tmux option on the user's server.
func (h *Host) SetGlobal(name, value string) {
	if host := h.client(); host != nil {
		host.SetGlobal(name, value)
	}
}

// OpenWindow creates a window on the user's server for the client viewing the popup,
// running argv in dir. It is the one thing the panel puts on that server rather than reads
// off it, since that is where the sessions a person is looking at live.
func (h *Host) OpenWindow(client, name, dir string, argv ...string) error {
	host := h.client()
	if host == nil {
		return errors.New("no tmux server has opened the panel yet")
	}

	return host.OpenWindow(client, name, dir, argv...)
}

// ShowGlobal returns a global tmux option's value from the user's server, empty if unset.
func (h *Host) ShowGlobal(name string) string {
	host := h.client()
	if host == nil {
		return ""
	}

	return host.ShowGlobal(name)
}

// Servers is the pair of tmux servers this program uses, presented as the one client the
// rest of it holds. Sessions, windows, and panes belong to the manager's server, while
// global options and the invoking client's directory belong to the user's.
type Servers struct {
	*Client
	host *Host
}

// NewServers pairs a client for the manager's server with the user's server.
func NewServers(manager *Client, host *Host) *Servers {
	return &Servers{Client: manager, host: host}
}

// GetClientPath asks the user's server, the only one the invoking client exists on.
func (s *Servers) GetClientPath(client string) string {
	return s.host.GetClientPath(client)
}

// SetGlobal writes on the user's server, where the tmux key binding reads it.
func (s *Servers) SetGlobal(name, value string) {
	s.host.SetGlobal(name, value)
}

// ShowGlobal reads from the user's server, for the same reason.
func (s *Servers) ShowGlobal(name string) string {
	return s.host.ShowGlobal(name)
}

// GetHostSocket is the socket path of the user's server, which an action reaching that
// server needs. It comes off the manager's own server, where the open command records it,
// rather than through ShowGlobal above, which asks the user's.
func (s *Servers) GetHostSocket() string {
	return s.Client.ShowGlobal(hostSocketOption)
}

// OpenWindow goes to the user's server, unlike NewWindow beside it, since a window meant
// for the user to sit in has to land in the session they are attached to.
func (s *Servers) OpenWindow(client, name, dir string, argv ...string) error {
	return s.host.OpenWindow(client, name, dir, argv...)
}
