package tmux

import (
	"strings"
)

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
