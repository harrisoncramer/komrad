package tmux

import (
	"os"
	"slices"
	"strings"

	"komrad/lib/xslice"
)

const tmuxEnvFields = 3

var launcherEnvPrefixes = []string{
	"TMUX=",
	"MISE_CONFIG_ROOT=",
	"MISE_PROJECT_ROOT=",
	"MISE_ORIGINAL_CWD=",
	"MISE_TASK_",
}

// SocketFromEnv returns the socket path of the server a TMUX value names.
func SocketFromEnv(value string) string {
	fields := strings.Split(value, ",")
	if len(fields) != tmuxEnvFields {
		return ""
	}

	return fields[0]
}

func buildClientEnv() []string {
	return dropLauncherEnv(os.Environ())
}

func dropLauncherEnv(environ []string) []string {
	return xslice.Filter(environ, func(entry string) bool {
		return !slices.ContainsFunc(launcherEnvPrefixes, func(prefix string) bool { return strings.HasPrefix(entry, prefix) })
	})
}
