package tmux

import (
	"strings"
)

const tmuxEnvFields = 3

// SocketFromEnv returns the socket path of the server a TMUX value names.
func SocketFromEnv(value string) string {
	fields := strings.Split(value, ",")
	if len(fields) != tmuxEnvFields {
		return ""
	}

	return fields[0]
}
