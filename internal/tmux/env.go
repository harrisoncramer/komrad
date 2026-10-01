package tmux

import (
	"os"
	"strings"

	"komrad/lib/xslice"
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

func environWithoutTmux() []string {
	return xslice.Filter(os.Environ(), func(entry string) bool { return !strings.HasPrefix(entry, "TMUX=") })
}
