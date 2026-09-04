package tmux

import (
	"strings"
)

const hostSocketOption = "@weave_host_socket"

const hostPathOption = "@weave_host_path"

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

type Servers struct {
	*Client
}

// NewServers wraps a client for the manager's server with what the panel has written down about
// the user's, which is all it knows of a server it never dials.
func NewServers(manager *Client) *Servers {
	return &Servers{Client: manager}
}

// RecordHostSocket stores the socket path of the user's server, called by the open command with
// the path tmux interpolated, so every open points the panel at the server that opened it.
func (s *Servers) RecordHostSocket(path string) {
	if path == "" {
		return
	}

	s.SetGlobal(hostSocketOption, path)
}

// RecordHostPath stores where the client that opened the panel is sitting, read on the user's own
// server by the commands that run there, since the panel cannot ask a server it does not run.
func (s *Servers) RecordHostPath(path string) {
	if path == "" {
		return
	}

	s.SetGlobal(hostPathOption, path)
}

// HostPath is the working directory of the pane the panel was opened over, which is where an
// agent given no directory of its own goes.
func (s *Servers) HostPath() string {
	return s.ShowGlobal(hostPathOption)
}

// GetHostSocket is the socket path of the user's server, which the open command records here
// so the panel and everything reading it agree on one answer.
func (s *Servers) GetHostSocket() string {
	return s.ShowGlobal(hostSocketOption)
}
