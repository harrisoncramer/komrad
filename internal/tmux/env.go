package tmux

import (
	"strings"
)

const tmuxEnvFields = 3

// SocketFromEnv returns the socket path of the server a TMUX value names, which is how the popup
// finds the user's server.
func SocketFromEnv(value string) string {
	fields := strings.Split(value, ",")
	if len(fields) != tmuxEnvFields {
		return ""
	}

	return fields[0]
}
