package tmux

import (
	"strings"
)

const hostSocketOption = "@weave_host_socket"

const InvokingClientOption = "@weave_client"

const InfoPaneOption = "@weave_info_pane"

const tmuxEnvFields = 3

// SocketFromEnv returns the socket path of the server a TMUX value names, which is how the
// popup finds the user's server, since tmux expands no format in a popup's command and an
// unexpanded one reads as a shell comment rather than failing.
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

type Host struct {
	manager *Client
}

// NewHost returns a Host that finds the user's server through the manager's, which holds the
// socket of whichever server opened the panel last.
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

func (h *Host) client() *Client {
	if path := h.manager.ShowGlobal(hostSocketOption); path != "" {
		return NewClientAtPath(path)
	}

	return nil
}

// GetClientPath returns a client's active pane working directory.
func (h *Host) GetClientPath(client string) string {
	host := h.client()
	if host == nil {
		return ""
	}

	return host.GetClientPath(client)
}

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

// GetHostSocket is the socket path of the user's server, which the open command records here
// so the panel and everything reading it agree on one answer.
func (s *Servers) GetHostSocket() string {
	return s.ShowGlobal(hostSocketOption)
}
